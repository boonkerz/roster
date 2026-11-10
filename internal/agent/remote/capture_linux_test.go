//go:build linux

package remote

import (
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConvertShm(t *testing.T) {
	// 2x2, stride 12 (Padding), XBGR -> BGRX getauscht, y-invertiert.
	src := make([]byte, 24)
	px := func(x, y int, r, g, b byte) { i := y*12 + x*4; src[i], src[i+1], src[i+2] = r, g, b } // XBGR8888 = RGBX im Speicher
	px(0, 0, 1, 2, 3)
	px(1, 1, 7, 8, 9)
	dst := convertShm(src, make([]byte, 16), 2, 2, 12, wlShmXBGR8888, true)
	if dst[0] != 0 || dst[4+0] != 9 || dst[4+1] != 8 || dst[4+2] != 7 { // Zeile 0 = Quellzeile 1
		t.Fatalf("Zeile 0: %v", dst[:8])
	}
	if dst[8] != 3 || dst[9] != 2 || dst[10] != 1 {
		t.Fatalf("Zeile 1: %v", dst[8:])
	}
	// XRGB: unverändert kopiert
	dst = convertShm(src, make([]byte, 16), 2, 2, 12, wlShmXRGB8888, false)
	if dst[0] != 1 || dst[12] != 7 {
		t.Fatalf("xrgb: %v", dst)
	}
}

func TestBlendCursor(t *testing.T) {
	dst := make([]byte, 3*3*4)
	for i := range dst {
		dst[i] = 100
	}
	// 2x1-Cursor bei (-1,1): erstes Pixel außerhalb, zweites deckend rot, dazu transparentes.
	blendCursor(dst, 3, 3, -1, 1, 2, 1, []uint32{0xffffffff, 0xffff0000})
	i := (1*3 + 0) * 4
	if dst[i+2] != 255 || dst[i+1] != 0 || dst[i] != 0 {
		t.Fatalf("deckend: %v", dst[i:i+4])
	}
	blendCursor(dst, 3, 3, 2, 2, 1, 1, []uint32{0})
	if dst[(2*3+2)*4] != 100 {
		t.Fatal("transparent überschrieben")
	}
}

func TestParseEnviron(t *testing.T) {
	m := parseEnviron([]byte("HOME=/home/pi\x00WAYLAND_DISPLAY=wayland-0\x00A=b=c\x00kaputt\x00"))
	if m["HOME"] != "/home/pi" || m["WAYLAND_DISPLAY"] != "wayland-0" || m["A"] != "b=c" || len(m) != 3 {
		t.Fatalf("%v", m)
	}
}

func TestInfoSource(t *testing.T) {
	s := newInfoSource("Titel", "Zeile")
	w, h := s.Bounds()
	b, err := s.Capture()
	if err != nil || len(b) != w*h*4 {
		t.Fatal(err)
	}
	lit := 0
	for i := 0; i < len(b); i += 4 {
		if b[i+2] > 0xa0 {
			lit++
		}
	}
	if lit == 0 {
		t.Fatal("kein Text gezeichnet")
	}
}

func savePNG(t *testing.T, name string, buf []byte, w, h int) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = buf[i*4+2], buf[i*4+1], buf[i*4], 255
	}
	f, _ := os.Create(name)
	defer f.Close()
	_ = png.Encode(f, img)
	t.Logf("Bild: %s (%dx%d)", name, w, h)
}

// Live-Test gegen die laufende Wayland-Sitzung (nur Aufnahme): ROSTER_LIVE_WAYLAND=1.
func TestLiveWayland(t *testing.T) {
	if os.Getenv("ROSTER_LIVE_WAYLAND") == "" {
		t.Skip("ROSTER_LIVE_WAYLAND nicht gesetzt")
	}
	sock := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), os.Getenv("WAYLAND_DISPLAY"))
	s, err := newWaylandSource(sock, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	t.Logf("eingaben: zeiger=%v tastatur=%v", s.vptr != 0, s.vkbd != 0)
	start := time.Now()
	var b []byte
	for i := 0; i < 5; i++ {
		if b, err = s.Capture(); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("5 Bilder in %v", time.Since(start))
	auto, err := newScreenSource(slog.New(slog.NewTextHandler(io.Discard, nil)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := auto.(*waylandSource); !ok {
		t.Fatalf("automatisch gewählt: %T", auto)
	}
	auto.Close()
	w, h := s.Bounds()
	savePNG(t, os.Getenv("ROSTER_LIVE_OUT")+"wayland.png", b, w, h)
}

// Live-Test X11 mit Xvfb: startet einen Server, prüft Bild und Eingaben (xdotool).
func TestLiveX11(t *testing.T) {
	if _, err := exec.LookPath("Xvfb"); err != nil || os.Getenv("ROSTER_LIVE_X11") == "" {
		t.Skip("Xvfb/ROSTER_LIVE_X11 fehlt")
	}
	x := exec.Command("Xvfb", ":77", "-screen", "0", "800x600x24", "-nolisten", "tcp")
	if err := x.Start(); err != nil {
		t.Fatal(err)
	}
	defer x.Process.Kill()
	time.Sleep(time.Second)
	s, err := newX11Source(":77", "", os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if w, h := s.Bounds(); w != 800 || h != 600 {
		t.Fatalf("%dx%d", w, h)
	}
	s.Pointer(0, 123, 45)
	c := exec.Command("xdotool", "getmouselocation")
	c.Env = append(os.Environ(), "DISPLAY=:77")
	out, _ := c.CombinedOutput()
	if !strings.Contains(string(out), "x:123 y:45") {
		t.Fatalf("mauszeiger: %s", out)
	}
	if kc, ok := s.keycodeFor('a'); !ok || kc == 0 {
		t.Fatal("keycode a")
	}
	if kc, ok := s.keycodeFor(0x20ac); !ok || kc != s.spare { // € nicht im Xvfb-Layout -> Ersatztaste
		t.Logf("€ -> %d (spare %d, ok %v)", kc, s.spare, ok)
	}
	s.Key(true, 'a')
	s.Key(false, 'a')
	b, err := s.Capture()
	if err != nil || len(b) != 800*600*4 {
		t.Fatal(err)
	}
}

func TestPickMime(t *testing.T) {
	if m := pickMime([]string{"text/html", "STRING", "text/plain;charset=utf-8"}); m != "text/plain;charset=utf-8" {
		t.Fatal(m)
	}
	if m := pickMime([]string{"image/png"}); m != "" {
		t.Fatal(m)
	}
}

func waitClip(get func() (string, bool), pump func()) (string, bool) {
	for i := 0; i < 40; i++ {
		if pump != nil {
			pump()
		}
		if t, ok := get(); ok {
			return t, true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return "", false
}

// Zwischenablage X11: zwei Verbindungen tauschen über CLIPBOARD aus (ROSTER_LIVE_X11=1).
func TestLiveX11Clipboard(t *testing.T) {
	if _, err := exec.LookPath("Xvfb"); err != nil || os.Getenv("ROSTER_LIVE_X11") == "" {
		t.Skip("Xvfb/ROSTER_LIVE_X11 fehlt")
	}
	x := exec.Command("Xvfb", ":78", "-screen", "0", "640x480x24", "-nolisten", "tcp")
	if err := x.Start(); err != nil {
		t.Fatal(err)
	}
	defer x.Process.Kill()
	time.Sleep(time.Second)
	a, err := newX11Source(":78", "", os.Getenv("HOME"))
	if err != nil || a.clip == nil {
		t.Fatal(err, a.clip)
	}
	defer a.Close()
	b, err := newX11Source(":78", "", os.Getenv("HOME"))
	if err != nil || b.clip == nil {
		t.Fatal(err)
	}
	defer b.Close()
	a.SetClipboard("Grüße aus Roster")
	if got, ok := waitClip(b.GetClipboard, nil); !ok || got != "Grüße aus Roster" {
		t.Fatalf("b las %q (%v)", got, ok)
	}
	if _, ok := a.GetClipboard(); ok {
		t.Fatal("Echo beim Besitzer")
	}
	b.SetClipboard("zurück")
	if got, ok := waitClip(a.GetClipboard, nil); !ok || got != "zurück" {
		t.Fatalf("a las %q (%v)", got, ok)
	}
}

// Zwischenablage Wayland gegen die laufende Sitzung mit wl-copy/wl-paste; stellt den
// vorherigen Inhalt wieder her (ROSTER_LIVE_WAYLAND=1).
func TestLiveWaylandClipboard(t *testing.T) {
	if os.Getenv("ROSTER_LIVE_WAYLAND") == "" {
		t.Skip("ROSTER_LIVE_WAYLAND nicht gesetzt")
	}
	if _, err := exec.LookPath("wl-paste"); err != nil {
		t.Skip("wl-clipboard fehlt")
	}
	orig, _ := exec.Command("wl-paste", "-n").Output()
	defer exec.Command("wl-copy", "--", string(orig)).Run()
	sock := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), os.Getenv("WAYLAND_DISPLAY"))
	s, err := newWaylandSource(sock, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.clip.active() {
		t.Fatal("kein data-control")
	}
	s.GetClipboard() // Anfangsinhalt verwerfen
	s.SetClipboard("Roster → Gerät äöü")
	s.pump()
	done := make(chan []byte)
	go func() { out, _ := exec.Command("wl-paste", "-n").Output(); done <- out }()
	var out []byte
loop:
	for i := 0; i < 100; i++ {
		select {
		case out = <-done:
			break loop
		default:
			s.pump()
			time.Sleep(20 * time.Millisecond)
		}
	}
	if string(out) != "Roster → Gerät äöü" {
		t.Fatalf("wl-paste: %q", out)
	}
	if err := exec.Command("wl-copy", "--", "vom Desktop 123").Run(); err != nil {
		t.Fatal(err)
	}
	if got, ok := waitClip(s.GetClipboard, nil); !ok || got != "vom Desktop 123" {
		t.Fatalf("gelesen %q (%v)", got, ok)
	}
}
