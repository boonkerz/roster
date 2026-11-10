//go:build linux

package remote

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgb/xtest"
)

// x11Source nimmt den Bildschirm einer X11-Sitzung auf (GetImage, Cursor über XFixes)
// und spielt Eingaben über XTEST ein. Reines Go (jezek/xgb), keine Xlib.

var envMu sync.Mutex // xgb liest XAUTHORITY/HOME aus der Umgebung

func init() { xgb.Logger = log.New(io.Discard, "", 0) } // eigene Fehler genügen

type x11Source struct {
	c        *xgb.Conn
	root     xproto.Window
	w, h     int
	cursor   bool
	input    bool
	minKC    xproto.Keycode
	perKC    int
	keysyms  []xproto.Keysym
	spare    xproto.Keycode
	prevMask int
	out      []byte
	lastGeo  time.Time
}

func newX11Source(display, xauth, home string) (*x11Source, error) {
	envMu.Lock()
	oldA, hadA := os.LookupEnv("XAUTHORITY")
	oldH := os.Getenv("HOME")
	if xauth != "" {
		os.Setenv("XAUTHORITY", xauth)
	} else {
		os.Unsetenv("XAUTHORITY")
		if home != "" {
			os.Setenv("HOME", home)
		}
	}
	c, err := xgb.NewConnDisplay(display)
	if hadA {
		os.Setenv("XAUTHORITY", oldA)
	} else {
		os.Unsetenv("XAUTHORITY")
	}
	os.Setenv("HOME", oldH)
	envMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("X11 %s: %v", display, err)
	}
	scr := xproto.Setup(c).DefaultScreen(c)
	if scr.RootDepth != 24 && scr.RootDepth != 32 {
		c.Close()
		return nil, fmt.Errorf("X11: Farbtiefe %d wird nicht unterstützt", scr.RootDepth)
	}
	s := &x11Source{c: c, root: scr.Root, w: int(scr.WidthInPixels), h: int(scr.HeightInPixels)}
	if xfixes.Init(c) == nil {
		if _, err := xfixes.QueryVersion(c, 4, 0).Reply(); err == nil {
			s.cursor = true
		}
	}
	if xtest.Init(c) == nil {
		s.input = true
		s.loadKeymap()
	}
	s.out = make([]byte, s.w*s.h*4)
	if _, err := s.Capture(); err != nil {
		c.Close()
		return nil, err
	}
	return s, nil
}

func (s *x11Source) Bounds() (int, int) { return s.w, s.h }

func (s *x11Source) Capture() ([]byte, error) {
	if time.Since(s.lastGeo) > 2*time.Second { // Auflösungswechsel erkennen
		s.lastGeo = time.Now()
		g, err := xproto.GetGeometry(s.c, xproto.Drawable(s.root)).Reply()
		if err != nil {
			return nil, err
		}
		if int(g.Width) != s.w || int(g.Height) != s.h {
			return nil, fmt.Errorf("X11: Größe geändert (%dx%d)", g.Width, g.Height)
		}
	}
	img, err := xproto.GetImage(s.c, xproto.ImageFormatZPixmap, xproto.Drawable(s.root), 0, 0, uint16(s.w), uint16(s.h), 0xffffffff).Reply()
	if err != nil {
		return nil, err
	}
	if len(img.Data) < s.w*s.h*4 {
		return nil, errors.New("X11: unerwartetes Bildformat (nicht 32 bpp)")
	}
	copy(s.out, img.Data[:s.w*s.h*4])
	if s.cursor {
		if cur, err := xfixes.GetCursorImage(s.c).Reply(); err == nil {
			blendCursor(s.out, s.w, s.h, int(cur.X)-int(cur.Xhot), int(cur.Y)-int(cur.Yhot), int(cur.Width), int(cur.Height), cur.CursorImage)
		}
	}
	return s.out, nil
}

// blendCursor legt ein ARGB-Cursorbild (vormultipliziert) auf den BGRX-Puffer (rein, getestet).
func blendCursor(dst []byte, w, h, x0, y0, cw, ch int, argb []uint32) {
	for cy := 0; cy < ch; cy++ {
		y := y0 + cy
		if y < 0 || y >= h {
			continue
		}
		for cx := 0; cx < cw; cx++ {
			x := x0 + cx
			if x < 0 || x >= w || cy*cw+cx >= len(argb) {
				continue
			}
			p := argb[cy*cw+cx]
			a := p >> 24
			if a == 0 {
				continue
			}
			i := (y*w + x) * 4
			inv := 255 - a
			dst[i+0] = byte((p & 0xff) + uint32(dst[i+0])*inv/255)
			dst[i+1] = byte((p>>8)&0xff + uint32(dst[i+1])*inv/255)
			dst[i+2] = byte((p>>16)&0xff + uint32(dst[i+2])*inv/255)
		}
	}
}

func (s *x11Source) loadKeymap() {
	setup := xproto.Setup(s.c)
	s.minKC = setup.MinKeycode
	n := byte(setup.MaxKeycode - setup.MinKeycode + 1)
	r, err := xproto.GetKeyboardMapping(s.c, setup.MinKeycode, n).Reply()
	if err != nil {
		return
	}
	s.perKC = int(r.KeysymsPerKeycode)
	s.keysyms = r.Keysyms
	// Freie Taste (ohne Belegung) für Zeichen, die das Layout nicht hat.
	for i := int(n) - 1; i >= 0; i-- {
		free := true
		for j := 0; j < s.perKC; j++ {
			if s.keysyms[i*s.perKC+j] != 0 {
				free = false
				break
			}
		}
		if free {
			s.spare = s.minKC + xproto.Keycode(i)
			break
		}
	}
}

// keycodeFor sucht den Keycode eines Keysyms (erste zwei Ebenen); sonst Ersatztaste.
func (s *x11Source) keycodeFor(ks uint32) (xproto.Keycode, bool) {
	if s.perKC == 0 {
		return 0, false
	}
	if ks >= 0x1000000 && ks-0x1000000 <= 0xff {
		ks -= 0x1000000
	}
	for i := 0; i*s.perKC < len(s.keysyms); i++ {
		for j := 0; j < s.perKC && j < 2; j++ {
			if uint32(s.keysyms[i*s.perKC+j]) == ks {
				return s.minKC + xproto.Keycode(i), true
			}
		}
	}
	if s.spare == 0 {
		return 0, false
	}
	row := make([]xproto.Keysym, s.perKC)
	row[0], row[1%s.perKC] = xproto.Keysym(ks), xproto.Keysym(ks)
	xproto.ChangeKeyboardMapping(s.c, 1, s.spare, byte(s.perKC), row)
	idx := int(s.spare-s.minKC) * s.perKC
	copy(s.keysyms[idx:idx+s.perKC], row)
	time.Sleep(10 * time.Millisecond)
	return s.spare, true
}

func (s *x11Source) Pointer(mask, x, y int) {
	if !s.input {
		return
	}
	xtest.FakeInput(s.c, xproto.MotionNotify, 0, 0, s.root, int16(clampInt(x, 0, s.w-1)), int16(clampInt(y, 0, s.h-1)), 0)
	for bit := 0; bit < 7; bit++ {
		was, is := s.prevMask&(1<<bit) != 0, mask&(1<<bit) != 0
		if was == is {
			continue
		}
		btn := byte(bit + 1) // RFB-Bits 0..6 = X-Tasten 1..7 (1 links, 2 Mitte, 3 rechts, 4-7 Rad)
		typ := byte(xproto.ButtonRelease)
		if is {
			typ = xproto.ButtonPress
		}
		xtest.FakeInput(s.c, typ, btn, 0, s.root, 0, 0, 0)
	}
	s.prevMask = mask
	s.c.Sync()
}

func (s *x11Source) Key(down bool, keysym uint32) {
	if !s.input {
		return
	}
	kc, ok := s.keycodeFor(keysym)
	if !ok {
		return
	}
	typ := byte(xproto.KeyRelease)
	if down {
		typ = xproto.KeyPress
	}
	xtest.FakeInput(s.c, typ, byte(kc), 0, s.root, 0, 0, 0)
	s.c.Sync()
}

func (s *x11Source) Close() error {
	if s.c != nil {
		s.c.Close()
		s.c = nil
	}
	return nil
}
