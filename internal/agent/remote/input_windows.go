//go:build windows

package remote

import (
	"encoding/binary"
	"unsafe"
)

// Maus-/Tastatureingaben per SendInput. Läuft in der Nutzer-Session (direkt im
// interaktiven Agent oder im __capture-Helfer) – aus Session 0 hätte SendInput
// keine Wirkung.

var procSendInput = modUser32.NewProc("SendInput")

const (
	inputMouse    = 0
	inputKeyboard = 1
	inputSize     = 40 // sizeof(INPUT) auf amd64

	meMove        = 0x0001
	meLeftDown    = 0x0002
	meLeftUp      = 0x0004
	meRightDown   = 0x0008
	meRightUp     = 0x0010
	meMiddleDown  = 0x0020
	meMiddleUp    = 0x0040
	meWheel       = 0x0800
	meAbsolute    = 0x8000
	meVirtualDesk = 0x4000

	keDown    = 0x0000
	keKeyUp   = 0x0002
	keUnicode = 0x0004

	wheelDelta = 120
)

func sendInput(b *[inputSize]byte) {
	procSendInput.Call(1, uintptr(unsafe.Pointer(b)), uintptr(inputSize))
}

// mouseInput baut ein INPUT (MOUSEINPUT). Offsets: type@0, dx@8, dy@12,
// mouseData@16, dwFlags@20, time@24, dwExtraInfo@32.
func mouseInput(dx, dy int32, mouseData, flags uint32) [inputSize]byte {
	var b [inputSize]byte
	binary.LittleEndian.PutUint32(b[0:], inputMouse)
	binary.LittleEndian.PutUint32(b[8:], uint32(dx))
	binary.LittleEndian.PutUint32(b[12:], uint32(dy))
	binary.LittleEndian.PutUint32(b[16:], mouseData)
	binary.LittleEndian.PutUint32(b[20:], flags)
	return b
}

// keyInput baut ein INPUT (KEYBDINPUT). Offsets: type@0, wVk@8, wScan@10,
// dwFlags@12, time@16, dwExtraInfo@24.
func keyInput(vk, scan uint16, flags uint32) [inputSize]byte {
	var b [inputSize]byte
	binary.LittleEndian.PutUint32(b[0:], inputKeyboard)
	binary.LittleEndian.PutUint16(b[8:], vk)
	binary.LittleEndian.PutUint16(b[10:], scan)
	binary.LittleEndian.PutUint32(b[12:], flags)
	return b
}

// pointerEvent verarbeitet einen RFB-PointerEvent: absolute Mausbewegung + Tasten +
// Mausrad. buttonMask-Bits: 0=links, 1=mitte, 2=rechts, 3=Rad hoch, 4=Rad runter.
func pointerEvent(prev *int, mask, x, y, srcX, srcY int) {
	// Framebuffer-Koordinate -> Bildschirmkoordinate -> absolute Position auf dem
	// gesamten virtuellen Desktop (funktioniert auch für Nicht-Primär-Monitore).
	sx, sy := srcX+x, srcY+y
	vl, vt := sysMetric(smXVirtualScreen), sysMetric(smYVirtualScreen)
	vw, vh := sysMetric(smCXVirtualScreen), sysMetric(smCYVirtualScreen)
	var ax, ay int32
	if vw > 1 {
		ax = int32((sx - vl) * 65535 / (vw - 1))
	}
	if vh > 1 {
		ay = int32((sy - vt) * 65535 / (vh - 1))
	}
	mv := mouseInput(ax, ay, 0, meMove|meAbsolute|meVirtualDesk)
	sendInput(&mv)

	type btn struct {
		bit        int
		downF, upF uint32
	}
	for _, b := range []btn{
		{0, meLeftDown, meLeftUp},
		{1, meMiddleDown, meMiddleUp},
		{2, meRightDown, meRightUp},
	} {
		nowDown := mask&(1<<b.bit) != 0
		wasDown := *prev&(1<<b.bit) != 0
		if nowDown && !wasDown {
			ev := mouseInput(0, 0, 0, b.downF)
			sendInput(&ev)
		} else if !nowDown && wasDown {
			ev := mouseInput(0, 0, 0, b.upF)
			sendInput(&ev)
		}
	}
	// Mausrad: Flanke 0->1 auf Bit 3 (hoch) bzw. 4 (runter).
	if mask&8 != 0 && *prev&8 == 0 {
		ev := mouseInput(0, 0, uint32(wheelDelta), meWheel)
		sendInput(&ev)
	}
	if mask&16 != 0 && *prev&16 == 0 {
		down := int32(-wheelDelta) // Laufzeit-Variable vermeidet Konstanten-Overflow
		ev := mouseInput(0, 0, uint32(down), meWheel)
		sendInput(&ev)
	}
	*prev = mask
}

// keyEvent verarbeitet einen RFB-KeyEvent. Bekannte Tasten (Modifikatoren,
// Sondertasten, Buchstaben, Ziffern) werden als virtueller Tastencode gesendet
// (damit Kürzel wie Strg+C funktionieren), sonstige druckbare Zeichen als Unicode.
func keyEvent(down bool, keysym uint32) {
	flags := uint32(keDown)
	if !down {
		flags = keKeyUp
	}
	if vk, ok := keysymToVK(keysym); ok {
		ev := keyInput(vk, 0, flags)
		sendInput(&ev)
		return
	}
	// Druckbares Zeichen als Unicode (keysym == Codepoint für Latin-1 / 0x01000000+N).
	var cp rune
	switch {
	case keysym >= 0x20 && keysym <= 0x7e:
		cp = rune(keysym)
	case keysym >= 0xa0 && keysym <= 0xff:
		cp = rune(keysym)
	case keysym&0xff000000 == 0x01000000:
		cp = rune(keysym & 0x00ffffff)
	default:
		return // unbekannt
	}
	ev := keyInput(0, uint16(cp), flags|keUnicode)
	sendInput(&ev)
}

// keysymToVK bildet gängige X11-Keysyms auf Windows-VK-Codes ab.
func keysymToVK(ks uint32) (uint16, bool) {
	switch {
	case ks >= 'a' && ks <= 'z':
		return uint16(ks - 'a' + 0x41), true
	case ks >= 'A' && ks <= 'Z':
		return uint16(ks - 'A' + 0x41), true
	case ks >= '0' && ks <= '9':
		return uint16(ks - '0' + 0x30), true
	case ks >= 0xffbe && ks <= 0xffc9: // F1..F12
		return uint16(0x70 + (ks - 0xffbe)), true
	}
	if vk, ok := specialVK[ks]; ok {
		return vk, true
	}
	return 0, false
}

var specialVK = map[uint32]uint16{
	0xff08: 0x08, // BackSpace
	0xff09: 0x09, // Tab
	0xff0d: 0x0d, // Return
	0xff1b: 0x1b, // Escape
	0xff63: 0x2d, // Insert
	0xffff: 0x2e, // Delete
	0xff50: 0x24, // Home
	0xff57: 0x23, // End
	0xff55: 0x21, // Page Up
	0xff56: 0x22, // Page Down
	0xff51: 0x25, // Left
	0xff52: 0x26, // Up
	0xff53: 0x27, // Right
	0xff54: 0x28, // Down
	0xffe1: 0x10, // Shift L
	0xffe2: 0x10, // Shift R
	0xffe3: 0x11, // Control L
	0xffe4: 0x11, // Control R
	0xffe9: 0x12, // Alt L
	0xffea: 0x12, // Alt R
	0xffeb: 0x5b, // Super L (Win)
	0xffec: 0x5c, // Super R
	0x0020: 0x20, // Space
}

// gdiSource setzt Eingaben direkt um (Agent läuft in der Nutzer-Session).
func (s *gdiSource) Pointer(mask, x, y int)       { pointerEvent(&s.prevMask, mask, x, y, s.srcX, s.srcY) }
func (s *gdiSource) Key(down bool, keysym uint32) { keyEvent(down, keysym) }
