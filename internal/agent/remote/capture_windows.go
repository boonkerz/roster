//go:build windows

package remote

import (
	"fmt"
	"unsafe"

	"log/slog"

	"golang.org/x/sys/windows"
)

var (
	modUser32 = windows.NewLazySystemDLL("user32.dll")
	modGdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procGetDC            = modUser32.NewProc("GetDC")
	procReleaseDC        = modUser32.NewProc("ReleaseDC")
	procGetSystemMetrics = modUser32.NewProc("GetSystemMetrics")
	procCreateCompatDC   = modGdi32.NewProc("CreateCompatibleDC")
	procCreateCompatBmp  = modGdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject     = modGdi32.NewProc("SelectObject")
	procBitBlt           = modGdi32.NewProc("BitBlt")
	procGetDIBits        = modGdi32.NewProc("GetDIBits")
	procDeleteObject     = modGdi32.NewProc("DeleteObject")
	procDeleteDC         = modGdi32.NewProc("DeleteDC")
)

const (
	smCXScreen = 0
	smCYScreen = 1
	srcCopy    = 0x00CC0020
	biRGB      = 0
	dibRGB     = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes, Bits  uint16
	Compression   uint32
	SizeImage     uint32
	XPPM, YPPM    int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// gdiSource nimmt den primären Bildschirm per GDI auf (32 bpp BGRX, top-down).
// Hinweis: läuft der Agent als SYSTEM-Dienst in Session 0, liefert GetDC(0) nicht
// den Desktop des angemeldeten Nutzers (Session-0-Isolation) – dann ist die
// Aufnahme schwarz. Interaktiv/in der Nutzer-Session liefert sie den Bildschirm.
type gdiSource struct {
	w, h   int
	screen uintptr
	memDC  uintptr
	bitmap uintptr
	buf    []byte
	bmi    bitmapInfo
}

func newGDISource(log *slog.Logger) (screenSource, error) {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("bildschirmgröße unbekannt")
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("GetDC fehlgeschlagen")
	}
	memDC, _, _ := procCreateCompatDC.Call(screen)
	bitmap, _, _ := procCreateCompatBmp.Call(screen, w, h)

	s := &gdiSource{w: int(w), h: int(h), screen: screen, memDC: memDC, bitmap: bitmap}
	s.buf = make([]byte, s.w*s.h*4)
	s.bmi.Header = bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(s.w),
		Height:      -int32(s.h), // negativ = top-down
		Planes:      1,
		Bits:        32,
		Compression: biRGB,
	}
	log.Info("gdi-bildschirmaufnahme bereit", "size", fmt.Sprintf("%dx%d", s.w, s.h))
	return s, nil
}

func (s *gdiSource) Bounds() (int, int) { return s.w, s.h }

func (s *gdiSource) Capture() ([]byte, error) {
	// Bitmap für den BitBlt selektieren …
	old, _, _ := procSelectObject.Call(s.memDC, s.bitmap)
	r, _, _ := procBitBlt.Call(s.memDC, 0, 0, uintptr(s.w), uintptr(s.h), s.screen, 0, 0, srcCopy)
	// … und VOR GetDIBits wieder deselektieren (sonst liefert GetDIBits schwarz).
	procSelectObject.Call(s.memDC, old)
	if r == 0 {
		return nil, fmt.Errorf("BitBlt fehlgeschlagen")
	}
	r, _, _ = procGetDIBits.Call(s.memDC, s.bitmap, 0, uintptr(s.h),
		uintptr(unsafe.Pointer(&s.buf[0])), uintptr(unsafe.Pointer(&s.bmi)), dibRGB)
	if r == 0 {
		return nil, fmt.Errorf("GetDIBits fehlgeschlagen")
	}
	return s.buf, nil
}

func (s *gdiSource) Close() error {
	if s.bitmap != 0 {
		procDeleteObject.Call(s.bitmap)
	}
	if s.memDC != 0 {
		procDeleteDC.Call(s.memDC)
	}
	if s.screen != 0 {
		procReleaseDC.Call(0, s.screen)
	}
	return nil
}
