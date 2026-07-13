//go:build linux && cgo

package main

import "github.com/veandco/go-sdl2/sdl"

// Tastatur-Abbildung SDL → X11-Keysym (RFB). Der Agent (keysymToVK/keyEvent auf
// dem Endpunkt) versteht Buchstaben/Ziffern als virtuelle Tasten (Groß-/
// Kleinschreibung folgt dem gehaltenen Shift) und alle übrigen druckbaren Zeichen
// als Unicode (Shift-unabhängig). Daraus folgt die Strategie:
//
//   - Modifikatoren (Shift/Strg/Alt/Win) → eigene Key-Events, damit Kürzel wie
//     Strg+C, Alt+Tab, Win+D am Endpunkt ankommen.
//   - Sondertasten (Enter, Esc, Pfeile, F-Tasten …) → feste Keysyms.
//   - Druckbare Zeichen: nur bei gehaltenem Strg/Alt/Win direkt als Kürzel senden;
//     sonst über SDL-TextInput (liefert layout-/deadkey-korrekte Zeichen inkl.
//     Umlaute und AltGr-Symbole) als Unicode-Keysym.

// specialKeysym bildet Sondertasten ab (Werte, die der Agent in specialVK kennt).
var specialKeysym = map[sdl.Keycode]uint32{
	sdl.K_RETURN:    0xff0d,
	sdl.K_KP_ENTER:  0xff0d,
	sdl.K_ESCAPE:    0xff1b,
	sdl.K_BACKSPACE: 0xff08,
	sdl.K_TAB:       0xff09,
	sdl.K_DELETE:    0xffff,
	sdl.K_INSERT:    0xff63,
	sdl.K_HOME:      0xff50,
	sdl.K_END:       0xff57,
	sdl.K_PAGEUP:    0xff55,
	sdl.K_PAGEDOWN:  0xff56,
	sdl.K_LEFT:      0xff51,
	sdl.K_UP:        0xff52,
	sdl.K_RIGHT:     0xff53,
	sdl.K_DOWN:      0xff54,
}

// modifierKeysym bildet Modifikatoren ab. RALT (AltGr) wird bewusst NICHT
// weitergereicht: die damit erzeugten Zeichen kommen als Unicode über TextInput,
// ein zusätzlich gehaltenes AltGr würde am Endpunkt stören.
var modifierKeysym = map[sdl.Keycode]uint32{
	sdl.K_LSHIFT: 0xffe1,
	sdl.K_RSHIFT: 0xffe2,
	sdl.K_LCTRL:  0xffe3,
	sdl.K_RCTRL:  0xffe4,
	sdl.K_LALT:   0xffe9,
	sdl.K_LGUI:   0xffeb,
	sdl.K_RGUI:   0xffec,
}

func init() {
	// F1..F12 sind bei SDL-Keycodes fortlaufend; der Agent erwartet 0xffbe..0xffc9.
	for i := 0; i < 12; i++ {
		specialKeysym[sdl.K_F1+sdl.Keycode(i)] = uint32(0xffbe + i)
	}
}

// runeToKeysym liefert das X11-Keysym für ein druckbares Zeichen (Latin-1 direkt,
// sonst Unicode-Keysym 0x01000000+Codepoint) – passend zur Unicode-Behandlung im
// Agent.
func runeToKeysym(r rune) uint32 {
	switch {
	case r >= 0x20 && r < 0x7f:
		return uint32(r)
	case r >= 0xa0 && r <= 0xff:
		return uint32(r)
	default:
		return 0x01000000 + uint32(r)
	}
}

// shortcutMods sind die Modifikatoren, bei denen ein druckbares Zeichen als Kürzel
// (nicht als Text) gesendet wird. LALT statt ALT, damit AltGr (RALT) ausgenommen bleibt.
const shortcutMods = uint16(sdl.KMOD_CTRL | sdl.KMOD_LALT | sdl.KMOD_GUI)
