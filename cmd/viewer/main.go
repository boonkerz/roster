// Command pcinv-viewer ist der native Fernsteuerungs-Viewer. Er spricht denselben
// RFB-Tunnel wie die Browser-Ansicht, rendert mit SDL3 (über das cgo-freie
// purego-Binding) und – entscheidend auf Wayland/niri – fordert per Keyboard-Grab
// das Protokoll keyboard-shortcuts-inhibit an, sodass der Compositor ALLE Tasten
// (Win+T, Win+1…, Alt+Tab, Esc …) an das entfernte Gerät durchreicht statt sie
// lokal abzufangen.
//
// Cgo-frei (CGO_ENABLED=0): baut für Linux/Windows/macOS ohne Cross-Toolchain; die
// SDL3-Laufzeitbibliothek wird zur Laufzeit geladen (Linux: libSDL3.so, Windows:
// SDL3.dll, macOS: libSDL3.dylib).
//
// Start: der Browser-Button „Im Viewer öffnen" ruft pcinv://<code>; alternativ
// „Kopieren" + pcinv-viewer <code>, oder pcinv-viewer ohne Argumente → Dialog.
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
	"github.com/jupiterrider/purego-sdl3/sdl"
)

type launchConfig struct {
	URL      string `json:"url"` // Server-Basis (https:// oder wss://)
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
	if len(os.Args) >= 3 && os.Args[1] == "--previewbar" {
		if err := previewBar(os.Args[2]); err != nil {
			log.Fatalf("pcinv-viewer: previewbar: %v", err)
		}
		return
	}
	for _, a := range os.Args[1:] {
		switch a {
		case "--register", "-register":
			if err := registerScheme(); err != nil {
				log.Fatalf("pcinv-viewer: register: %v", err)
			}
			log.Println("pcinv://-Handler registriert – der Browser-Button „Im Viewer öffnen\" funktioniert jetzt.")
			return
		case "--selftest", "-selftest":
			// Lädt die SDL3-Laufzeit und initialisiert das Video-Subsystem (kein Fenster).
			if !sdl.Init(sdl.InitVideo) {
				log.Fatalf("pcinv-viewer: selftest: sdl init: %s", sdl.GetError())
			}
			sdl.Quit()
			log.Println("selftest ok")
			return
		}
	}
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("pcinv-viewer: %v", err)
	}
	if err := runApp(cfg); err != nil {
		log.Fatalf("pcinv-viewer: %v", err)
	}
}

// runApp initialisiert SDL, zeigt bei fehlendem Startcode den Connect-Dialog und
// startet dann die Fernsteuerungs-Sitzung. Fehler werden zusätzlich als Meldebox
// angezeigt (wichtig unter Windows, wo der Viewer ohne Konsole läuft).
func runApp(cfg *launchConfig) error {
	if !sdl.Init(sdl.InitVideo) {
		return fmt.Errorf("sdl init: %s", sdl.GetError())
	}
	defer sdl.Quit()

	err := runAppInner(cfg)
	if err != nil {
		_ = sdl.ShowSimpleMessageBox(sdl.MessageBoxError, "PC-Inventory Fernsteuerung", err.Error(), nil)
	}
	return err
}

func runAppInner(cfg *launchConfig) error {
	if cfg == nil {
		c, err := connectDialog()
		if err != nil {
			return err
		}
		if c == nil {
			return nil // vom Nutzer abgebrochen
		}
		cfg = c
	}
	return runSession(cfg)
}

// decodeLaunchCode entschlüsselt einen base64-Startcode (url-safe oder Standard) in
// eine launchConfig. Akzeptiert auch die pcinv://-URL-Form (inkl. prozent-kodiert).
func decodeLaunchCode(code string) (*launchConfig, error) {
	code = strings.TrimSpace(code)
	code = strings.TrimPrefix(code, "pcinv://")
	code = strings.Trim(code, "/")
	if dec, err := url.QueryUnescape(code); err == nil {
		code = dec
	}
	data, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		data, err = base64.StdEncoding.DecodeString(code)
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

func loadConfig() (*launchConfig, error) {
	args := os.Args[1:]
	// Einzelner Nicht-Flag-Parameter = base64-Startcode (JSON) oder pcinv://-Link.
	if len(args) == 1 && !strings.HasPrefix(args[0], "-") {
		return decodeLaunchCode(args[0])
	}
	if len(args) == 0 {
		return nil, nil // ohne Argumente -> Connect-Dialog
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

// runSession baut die Verbindung auf und fährt Fenster + Event-/Render-Loop
// (SDL ist von runApp bereits initialisiert).
func runSession(cfg *launchConfig) error {
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

	winW, winH := clampWindow(rc.W, rc.H)
	title := cfg.Title
	if title == "" {
		title = "PC-Inventory Fernsteuerung"
	}
	window := sdl.CreateWindow(title, int32(winW), int32(winH), sdl.WindowResizable)
	if window == nil {
		return fmt.Errorf("fenster: %s", sdl.GetError())
	}
	defer sdl.DestroyWindow(window)
	// Keyboard-Grab → shortcuts-inhibit auf Wayland → alle Tasten erreichen das Gerät.
	sdl.SetWindowKeyboardGrab(window, true)

	renderer := sdl.CreateRenderer(window, "")
	if renderer == nil {
		return fmt.Errorf("renderer: %s", sdl.GetError())
	}
	defer sdl.DestroyRenderer(renderer)
	sdl.SetRenderDrawBlendMode(renderer, sdl.BlendModeBlend) // für die halbtransparente Leiste

	txt, err := newTextRenderer(renderer, 15)
	if err != nil {
		return fmt.Errorf("font: %w", err)
	}

	texW, texH := int32(rc.W), int32(rc.H)
	texture := sdl.CreateTexture(renderer, sdl.PixelFormatARGB8888, sdl.TextureAccessStreaming, texW, texH)
	if texture == nil {
		return fmt.Errorf("texture: %s", sdl.GetError())
	}
	defer func() { sdl.DestroyTexture(texture) }() // texture wird bei Auflösungswechsel neu angelegt
	sdl.SetTextureBlendMode(texture, sdl.BlendModeNone)

	sdl.StartTextInput(window)

	updates := make(chan rectUpdate, 256)
	cut := make(chan string, 4)
	done := make(chan error, 1)
	go func() { done <- rc.readLoop(updates, cut) }()

	if err := rc.setEncodings(7, 0, -223); err != nil { // Tight, Raw, DesktopSize
		return err
	}
	if err := rc.requestUpdate(false); err != nil { // erstes Vollbild
		return err
	}
	log.Printf("verbunden – Bedienleiste oben, Tastatur-Grab aktiv")

	tb := newToolbar(txt)
	v := &viewer{rc: rc}
	fullscreen, locked, uiDirty := false, false, false
	quality := byte(1)
	qName := []string{"N", "M", "H"}
	hoverID, lastHover := "", "?"

	doAction := func(id string) {
		uiDirty = true
		switch id {
		case "sas":
			_ = rc.controlSAS()
		case "win":
			_ = rc.keyEvent(true, 0xffeb)
			_ = rc.keyEvent(false, 0xffeb)
		case "alttab":
			_ = rc.keyEvent(true, 0xffe9)
			_ = rc.keyEvent(true, 0xff09)
			_ = rc.keyEvent(false, 0xff09)
			_ = rc.keyEvent(false, 0xffe9)
		case "esc":
			_ = rc.keyEvent(true, 0xff1b)
			_ = rc.keyEvent(false, 0xff1b)
		case "lock":
			locked = !locked
			_ = rc.controlBlock(locked)
			if locked {
				tb.setLabel("lock", "Entsperren")
			} else {
				tb.setLabel("lock", "Sperren")
			}
		case "msg":
			_ = rc.controlMessage("Fernwartung aktiv - bitte nicht ausschalten.")
		case "qual":
			quality = (quality + 1) % 3
			_ = rc.controlQuality(quality)
			tb.setLabel("qual", "Qualität: "+qName[quality])
		case "full":
			fullscreen = !fullscreen
			sdl.SetWindowFullscreen(window, fullscreen)
		}
	}

	running := true
	var ev sdl.Event
	for running {
		var winW, winH int32
		sdl.GetWindowSize(window, &winW, &winH)
		dst := remoteDst(float32(winW), float32(winH), float32(texW), float32(texH))
		tb.layout(float32(winW))

		for sdl.PollEvent(&ev) {
			switch ev.Type() {
			case sdl.EventQuit, sdl.EventWindowCloseRequested:
				running = false
			case sdl.EventKeyDown, sdl.EventKeyUp:
				v.onKey(&ev)
			case sdl.EventTextInput:
				ti := ev.Text()
				v.onText(ti.Text())
			case sdl.EventMouseMotion:
				m := ev.Motion()
				hoverID = tb.hit(m.X, m.Y)
				if rx, ry, ok := winToRemote(m.X, m.Y, dst, float32(texW), float32(texH)); ok {
					v.curMask = int(m.State) & 0x7
					v.lastX, v.lastY = rx, ry
					_ = rc.pointerEvent(v.curMask, rx, ry)
				}
			case sdl.EventMouseButtonDown, sdl.EventMouseButtonUp:
				b := ev.Button()
				down := ev.Type() == sdl.EventMouseButtonDown
				if b.Y <= barHeight { // Klick auf die Bedienleiste
					if down && b.Button == uint8(sdl.ButtonLeft) {
						if id := tb.hit(b.X, b.Y); id == "disc" {
							running = false
						} else if id != "" {
							doAction(id)
						}
					}
					break
				}
				rx, ry, ok := winToRemote(b.X, b.Y, dst, float32(texW), float32(texH))
				if !ok {
					break
				}
				bit := -1
				switch b.Button {
				case uint8(sdl.ButtonLeft):
					bit = 0
				case uint8(sdl.ButtonMiddle):
					bit = 1
				case uint8(sdl.ButtonRight):
					bit = 2
				}
				if bit >= 0 {
					if down {
						v.curMask |= 1 << bit
					} else {
						v.curMask &^= 1 << bit
					}
				}
				v.lastX, v.lastY = rx, ry
				_ = rc.pointerEvent(v.curMask, rx, ry)
			case sdl.EventMouseWheel:
				wh := ev.Wheel()
				if wh.Y != 0 {
					bit := 3
					if wh.Y < 0 {
						bit = 4
					}
					_ = rc.pointerEvent(v.curMask|1<<bit, v.lastX, v.lastY)
					_ = rc.pointerEvent(v.curMask, v.lastX, v.lastY)
				}
			}
		}

		painted := false
	drain:
		for {
			select {
			case up := <-updates:
				if up.resize {
					sdl.DestroyTexture(texture)
					texture = sdl.CreateTexture(renderer, sdl.PixelFormatARGB8888, sdl.TextureAccessStreaming, int32(up.w), int32(up.h))
					if texture == nil {
						log.Printf("resize texture: %s", sdl.GetError())
						running = false
						break drain
					}
					sdl.SetTextureBlendMode(texture, sdl.BlendModeNone)
					texW, texH = int32(up.w), int32(up.h)
					log.Printf("auflösung geändert: %dx%d", up.w, up.h)
				} else {
					applyRect(texture, up)
				}
				painted = true
			case <-cut:
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

		if painted || uiDirty || hoverID != lastHover {
			lastHover, uiDirty = hoverID, false
			sdl.SetRenderDrawColor(renderer, 0x0b, 0x0e, 0x14, 0xff)
			sdl.RenderClear(renderer)
			sdl.RenderTexture(renderer, texture, nil, &dst)
			tb.draw(hoverID, locked)
			sdl.RenderPresent(renderer)
		}
		time.Sleep(6 * time.Millisecond)
	}
	return nil
}

// remoteDst berechnet das Zielrechteck für das Remote-Bild unterhalb der Leiste
// (Seitenverhältnis bleibt, zentriert/letterboxed).
func remoteDst(winW, winH, texW, texH float32) sdl.FRect {
	availH := winH - barHeight
	if availH < 1 {
		availH = 1
	}
	scale := winW / texW
	if s := availH / texH; s < scale {
		scale = s
	}
	dw, dh := texW*scale, texH*scale
	return sdl.FRect{X: (winW - dw) / 2, Y: barHeight + (availH-dh)/2, W: dw, H: dh}
}

// winToRemote rechnet eine Fensterposition in Framebuffer-Koordinaten um (false,
// wenn außerhalb des Remote-Bilds bzw. in der Leiste).
func winToRemote(mx, my float32, dst sdl.FRect, texW, texH float32) (int, int, bool) {
	if my <= barHeight || mx < dst.X || mx >= dst.X+dst.W || my < dst.Y || my >= dst.Y+dst.H {
		return 0, 0, false
	}
	return int((mx - dst.X) / dst.W * texW), int((my - dst.Y) / dst.H * texH), true
}

// viewer hält den Eingabe-Zustand (Maustasten-Maske, letzte Position).
type viewer struct {
	rc           *rfbClient
	curMask      int
	lastX, lastY int
}

func (v *viewer) onKey(ev *sdl.Event) {
	down := ev.Type() == sdl.EventKeyDown
	ke := ev.Key()
	sym := ke.Key
	if ks, ok := modifierKeysym[sym]; ok {
		_ = v.rc.keyEvent(down, ks)
		return
	}
	if sym == sdl.KeycodeRAlt || sym == sdl.KeycodeCapsLock {
		return // AltGr/CapsLock bewusst nicht weiterreichen (siehe keymap.go)
	}
	if ks, ok := specialKeysym[sym]; ok {
		_ = v.rc.keyEvent(down, ks)
		return
	}
	if sym >= 0x20 && sym <= 0x7e {
		// Druckbares Zeichen: nur als Kürzel (Strg/Alt/Win) direkt senden, sonst TextInput.
		if ke.Mod&shortcutMods != 0 {
			_ = v.rc.keyEvent(down, uint32(sym))
		}
	}
}

func (v *viewer) onText(text string) {
	if sdl.GetModState()&shortcutMods != 0 {
		return // Kürzel laufen über onKey; kein doppeltes Senden
	}
	for _, r := range text {
		ks := runeToKeysym(r)
		_ = v.rc.keyEvent(true, ks)
		_ = v.rc.keyEvent(false, ks)
	}
}

// applyRect lädt ein dekodiertes Rechteck in die Textur (BGRX == ARGB8888 im Speicher).
func applyRect(t *sdl.Texture, up rectUpdate) {
	if len(up.pix) == 0 {
		return
	}
	r := sdl.Rect{X: int32(up.x), Y: int32(up.y), W: int32(up.w), H: int32(up.h)}
	sdl.UpdateTexture(t, &r, unsafe.Pointer(&up.pix[0]), int32(up.w*4))
}

// clampWindow verkleinert das Startfenster auf die nutzbare Bildschirmfläche
// (Seitenverhältnis bleibt; die Renderer-Logikgröße bleibt volle Auflösung).
func clampWindow(w, h int) (int, int) {
	var b sdl.Rect
	if !sdl.GetDisplayUsableBounds(sdl.GetPrimaryDisplay(), &b) || b.W == 0 || b.H == 0 {
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
