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
	procGetCursorInfo    = modUser32.NewProc("GetCursorInfo")
	procGetIconInfo      = modUser32.NewProc("GetIconInfo")
	procDrawIconEx       = modUser32.NewProc("DrawIconEx")
)

const (
	cursorShowing = 1
	diNormal      = 0x0003
)

type cursorInfo struct {
	cbSize      uint32
	flags       uint32
	hCursor     uintptr
	ptScreenPos struct{ x, y int32 }
}

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

// drawCursor komponiert den (von BitBlt nicht erfassten) Mauszeiger in den memDC.
// offX/offY = Ursprung des aufgenommenen Bereichs (für Nicht-Primär-Monitore /
// virtuellen Desktop), damit der Zeiger an der richtigen Stelle landet.
func drawCursor(memDC uintptr, offX, offY int) {
	var ci cursorInfo
	ci.cbSize = uint32(unsafe.Sizeof(ci))
	if r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r == 0 || ci.flags != cursorShowing {
		return
	}
	var ii iconInfo
	if r, _, _ := procGetIconInfo.Call(ci.hCursor, uintptr(unsafe.Pointer(&ii))); r == 0 {
		return
	}
	procDrawIconEx.Call(memDC,
		uintptr(ci.ptScreenPos.x-int32(ii.xHotspot)-int32(offX)),
		uintptr(ci.ptScreenPos.y-int32(ii.yHotspot)-int32(offY)),
		ci.hCursor, 0, 0, 0, 0, diNormal)
	if ii.hbmMask != 0 {
		procDeleteObject.Call(ii.hbmMask)
	}
	if ii.hbmColor != 0 {
		procDeleteObject.Call(ii.hbmColor)
	}
}

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
	w, h       int
	srcX, srcY int // Ursprung des aufgenommenen Bereichs (Monitor-Auswahl)
	screen     uintptr
	memDC      uintptr
	bitmap     uintptr
	buf        []byte
	bmi        bitmapInfo
	prevMask   int // zuletzt gesehene Maustasten-Maske (für Down/Up-Erkennung)
	winClipboard
}

func newGDISource(log *slog.Logger, monitor int) (screenSource, error) {
	x, y, w, h := captureRect(monitor)
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("bildschirmgröße unbekannt")
	}
	screen, _, _ := procGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("GetDC fehlgeschlagen")
	}
	memDC, _, _ := procCreateCompatDC.Call(screen)
	bitmap, _, _ := procCreateCompatBmp.Call(screen, uintptr(w), uintptr(h))

	s := &gdiSource{w: w, h: h, srcX: x, srcY: y, screen: screen, memDC: memDC, bitmap: bitmap}
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
	r, _, _ := procBitBlt.Call(s.memDC, 0, 0, uintptr(s.w), uintptr(s.h), s.screen, uintptr(s.srcX), uintptr(s.srcY), srcCopy)
	drawCursor(s.memDC, s.srcX, s.srcY) // Mauszeiger einzeichnen (solange die Bitmap selektiert ist)
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
