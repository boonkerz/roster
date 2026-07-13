//go:build linux && cgo

// Command pcinv-viewer ist der native Fernsteuerungs-Viewer für Linux-Operator.
// Er spricht denselben RFB-Tunnel wie die Browser-Ansicht, rendert mit SDL2 und –
// entscheidend auf Wayland/niri – fordert per SDL-Keyboard-Grab das Protokoll
// zwptastenkuerzel-inhibit an, sodass der Compositor ALLE Tasten (Win+T, Win+1…,
// Alt+Tab, Esc …) an das entfernte Gerät durchreicht statt sie lokal abzufangen.
//
// Start: der Browser („Nativer Viewer") ruft /remote/start und übergibt einen
// base64-Startcode: pcinv-viewer <code>. Alternativ per Flags (--url/--device/
// --session/--token).
package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"github.com/coder/websocket"
	"github.com/veandco/go-sdl2/sdl"
)

type launchConfig struct {
	URL      string `json:"url"`   // Server-Basis (https:// oder wss://)
	Device   string `json:"device"`
	Session  string `json:"session"`
	Token    string `json:"token"`
	Insecure bool   `json:"insecure,omitempty"`
	Title    string `json:"title,omitempty"`
}

func (c *launchConfig) validate() error {
	if c.URL == "" || c.Device == "" || c.Session == "" || c.Token == "" {
		return fmt.Errorf("url, device, session und token sind erforderlich")
	}
	return nil
}

func main() {
	runtime.LockOSThread() // SDL: Video/Events müssen auf dem Main-Thread laufen.
	log.SetFlags(0)
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("pcinv-viewer: %v", err)
	}
	if err := run(cfg); err != nil {
		log.Fatalf("pcinv-viewer: %v", err)
	}
}

func loadConfig() (*launchConfig, error) {
	args := os.Args[1:]
	// Einzelner Nicht-Flag-Parameter = base64-Startcode (JSON).
	if len(args) == 1 && !strings.HasPrefix(args[0], "-") {
		data, err := base64.RawURLEncoding.DecodeString(args[0])
		if err != nil {
			data, err = base64.StdEncoding.DecodeString(args[0])
		}
		if err != nil {
			return nil, fmt.Errorf("ungültiger Startcode: %v", err)
		}
		var c launchConfig
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("ungültiger Startcode: %v", err)
		}
		return &c, c.validate()
	}
	fs := flag.NewFlagSet("pcinv-viewer", flag.ContinueOnError)
	c := &launchConfig{}
	fs.StringVar(&c.URL, "url", "", "Server-Basis-URL (https:// oder wss://)")
	fs.StringVar(&c.Device, "device", "", "Geräte-ID")
	fs.StringVar(&c.Session, "session", "", "Session-ID aus /remote/start")
	fs.StringVar(&c.Token, "token", "", "Viewer-Token aus /remote/start")
	fs.BoolVar(&c.Insecure, "insecure", false, "TLS-Zertifikat nicht prüfen (nur Test)")
	fs.StringVar(&c.Title, "title", "", "Fenstertitel")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c, c.validate()
}

func run(cfg *launchConfig) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	endpoint := wsBase(cfg.URL) + "/api/v1/devices/" + cfg.Device +
		"/remote/viewer-ws?session=" + url.QueryEscape(cfg.Session)
	log.Printf("verbinde mit %s …", endpoint)

	httpClient := &http.Client{}
	if cfg.Insecure {
		httpClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	conn, _, err := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{
		HTTPClient:      httpClient,
		HTTPHeader:      http.Header{"Authorization": {"Bearer " + cfg.Token}},
		CompressionMode: websocket.CompressionContextTakeover,
	})
	dialCancel()
	if err != nil {
		return fmt.Errorf("verbindung fehlgeschlagen (Sitzung abgelaufen? im Browser neu starten): %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(16 << 20)
	nc := websocket.NetConn(ctx, conn, websocket.MessageBinary)

	rc, err := rfbHandshake(nc)
	if err != nil {
		return fmt.Errorf("rfb-handshake: %w", err)
	}
	log.Printf("verbunden – bildschirm %dx%d", rc.W, rc.H)

	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		return fmt.Errorf("sdl init: %w", err)
	}
	defer sdl.Quit()
	// Keyboard-Grab: fordert auf Wayland shortcuts-inhibit an → alle Tasten kommen an.
	sdl.SetHint(sdl.HINT_GRAB_KEYBOARD, "1")

	winW, winH := clampWindow(rc.W, rc.H)
	title := cfg.Title
	if title == "" {
		title = "PC-Inventory Fernsteuerung"
	}
	window, err := sdl.CreateWindow(title, sdl.WINDOWPOS_CENTERED, sdl.WINDOWPOS_CENTERED,
		int32(winW), int32(winH), sdl.WINDOW_RESIZABLE)
	if err != nil {
		return fmt.Errorf("fenster: %w", err)
	}
	defer window.Destroy()
	window.SetKeyboardGrab(true)

	renderer, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_ACCELERATED)
	if err != nil {
		return fmt.Errorf("renderer: %w", err)
	}
	defer renderer.Destroy()
	_ = renderer.SetLogicalSize(int32(rc.W), int32(rc.H))

	texture, err := renderer.CreateTexture(sdl.PIXELFORMAT_ARGB8888, sdl.TEXTUREACCESS_STREAMING,
		int32(rc.W), int32(rc.H))
	if err != nil {
		return fmt.Errorf("texture: %w", err)
	}
	defer texture.Destroy()
	_ = texture.SetBlendMode(sdl.BLENDMODE_NONE)

	sdl.StartTextInput()

	updates := make(chan rectUpdate, 256)
	cut := make(chan string, 4)
	done := make(chan error, 1)
	go func() { done <- rc.readLoop(updates, cut) }()

	if err := rc.setEncodings(7, 0); err != nil { // Tight bevorzugt, Raw als Fallback
		return err
	}
	if err := rc.requestUpdate(false); err != nil { // erstes Vollbild
		return err
	}
	log.Printf("tastatur-grab aktiv – zum Beenden Fenster schließen")

	v := &viewer{rc: rc}
	running := true
	for running {
		for ev := sdl.PollEvent(); ev != nil; ev = sdl.PollEvent() {
			switch e := ev.(type) {
			case *sdl.QuitEvent:
				running = false
			case *sdl.WindowEvent:
				if e.Event == sdl.WINDOWEVENT_CLOSE {
					running = false
				}
			case *sdl.KeyboardEvent:
				v.onKey(e)
			case *sdl.TextInputEvent:
				v.onText(e)
			case *sdl.MouseMotionEvent:
				v.curMask = int(e.State) & 0x7
				v.lastX, v.lastY = int(e.X), int(e.Y)
				_ = rc.pointerEvent(v.curMask, v.lastX, v.lastY)
			case *sdl.MouseButtonEvent:
				v.onMouseButton(e)
			case *sdl.MouseWheelEvent:
				v.onWheel(e)
			}
		}

		painted := false
	drain:
		for {
			select {
			case up := <-updates:
				applyRect(texture, up)
				painted = true
			case txt := <-cut:
				_ = sdl.SetClipboardText(txt)
			case err := <-done:
				if err != nil {
					log.Printf("verbindung beendet: %v", err)
				}
				running = false
				break drain
			default:
				break drain
			}
		}
		if painted {
			_ = renderer.Clear()
			_ = renderer.Copy(texture, nil, nil)
			renderer.Present()
		}
		sdl.Delay(3)
	}
	return nil
}

// viewer hält den Eingabe-Zustand (Maustasten-Maske, letzte Position).
type viewer struct {
	rc           *rfbClient
	curMask      int
	lastX, lastY int
}

func (v *viewer) onKey(e *sdl.KeyboardEvent) {
	down := e.Type == sdl.KEYDOWN
	sym := e.Keysym.Sym
	if ks, ok := modifierKeysym[sym]; ok {
		_ = v.rc.keyEvent(down, ks)
		return
	}
	if sym == sdl.K_RALT || sym == sdl.K_CAPSLOCK {
		return // AltGr/CapsLock bewusst nicht weiterreichen (siehe keymap.go)
	}
	if ks, ok := specialKeysym[sym]; ok {
		_ = v.rc.keyEvent(down, ks)
		return
	}
	if sym >= 0x20 && sym <= 0x7e {
		// Druckbares Zeichen: nur als Kürzel (Strg/Alt/Win) direkt senden, sonst TextInput.
		if e.Keysym.Mod&shortcutMods != 0 {
			_ = v.rc.keyEvent(down, uint32(sym))
		}
	}
}

func (v *viewer) onText(e *sdl.TextInputEvent) {
	if uint16(sdl.GetModState())&shortcutMods != 0 {
		return // Kürzel laufen über onKey; kein doppeltes Senden
	}
	for _, r := range e.GetText() {
		ks := runeToKeysym(r)
		_ = v.rc.keyEvent(true, ks)
		_ = v.rc.keyEvent(false, ks)
	}
}

func (v *viewer) onMouseButton(e *sdl.MouseButtonEvent) {
	bit := -1
	switch e.Button {
	case sdl.BUTTON_LEFT:
		bit = 0
	case sdl.BUTTON_MIDDLE:
		bit = 1
	case sdl.BUTTON_RIGHT:
		bit = 2
	}
	if bit >= 0 {
		if e.State == sdl.PRESSED {
			v.curMask |= 1 << bit
		} else {
			v.curMask &^= 1 << bit
		}
	}
	v.lastX, v.lastY = int(e.X), int(e.Y)
	_ = v.rc.pointerEvent(v.curMask, v.lastX, v.lastY)
}

func (v *viewer) onWheel(e *sdl.MouseWheelEvent) {
	if e.Y == 0 {
		return
	}
	bit := 3 // hoch
	if e.Y < 0 {
		bit = 4 // runter
	}
	_ = v.rc.pointerEvent(v.curMask|1<<bit, v.lastX, v.lastY)
	_ = v.rc.pointerEvent(v.curMask, v.lastX, v.lastY)
}

// applyRect lädt ein dekodiertes Rechteck in die Textur (BGRX == ARGB8888 im Speicher).
func applyRect(t *sdl.Texture, up rectUpdate) {
	if len(up.pix) == 0 {
		return
	}
	r := sdl.Rect{X: int32(up.x), Y: int32(up.y), W: int32(up.w), H: int32(up.h)}
	_ = t.Update(&r, unsafe.Pointer(&up.pix[0]), up.w*4)
}

// clampWindow verkleinert das Startfenster auf die nutzbare Bildschirmfläche
// (Seitenverhältnis bleibt; die Renderer-Logikgröße bleibt volle Auflösung).
func clampWindow(w, h int) (int, int) {
	b, err := sdl.GetDisplayUsableBounds(0)
	if err != nil {
		return w, h
	}
	maxW, maxH := int(float64(b.W)*0.95), int(float64(b.H)*0.92)
	if w <= maxW && h <= maxH {
		return w, h
	}
	s := float64(maxW) / float64(w)
	if sy := float64(maxH) / float64(h); sy < s {
		s = sy
	}
	return int(float64(w) * s), int(float64(h) * s)
}

func wsBase(u string) string {
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	case strings.HasPrefix(u, "wss://"), strings.HasPrefix(u, "ws://"):
		return u
	default:
		return "wss://" + u
	}
}
