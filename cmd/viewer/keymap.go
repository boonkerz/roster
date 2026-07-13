package main

import "github.com/jupiterrider/purego-sdl3/sdl"

// Tastatur-Abbildung SDL → X11-Keysym (RFB). Der Agent (keysymToVK/keyEvent auf
// dem Endpunkt) versteht Buchstaben/Ziffern als virtuelle Tasten (Groß-/
// Kleinschreibung folgt dem gehaltenen Shift) und alle übrigen druckbaren Zeichen
// als Unicode. Daraus folgt die Strategie:
//
//   - Modifikatoren (Shift/Strg/Alt/Win) → eigene Key-Events, damit Kürzel wie
//     Strg+C, Alt+Tab, Win+D am Endpunkt ankommen.
//   - Sondertasten (Enter, Esc, Pfeile, F-Tasten …) → feste Keysyms.
//   - Druckbare Zeichen: nur bei gehaltenem Strg/Alt/Win direkt als Kürzel senden;
//     sonst über SDL-TextInput (liefert layout-/deadkey-korrekte Zeichen inkl.
//     Umlaute und AltGr-Symbole) als Unicode-Keysym.

// specialKeysym bildet Sondertasten ab (Werte, die der Agent in specialVK kennt).
var specialKeysym = map[sdl.Keycode]uint32{
	sdl.KeycodeReturn:    0xff0d,
	sdl.KeycodeKpEnter:   0xff0d,
	sdl.KeycodeEscape:    0xff1b,
	sdl.KeycodeBackspace: 0xff08,
	sdl.KeycodeTab:       0xff09,
	sdl.KeycodeDelete:    0xffff,
	sdl.KeycodeInsert:    0xff63,
	sdl.KeycodeHome:      0xff50,
	sdl.KeycodeEnd:       0xff57,
	sdl.KeycodePageUp:    0xff55,
	sdl.KeycodePageDown:  0xff56,
	sdl.KeycodeLeft:      0xff51,
	sdl.KeycodeUp:        0xff52,
	sdl.KeycodeRight:     0xff53,
	sdl.KeycodeDown:      0xff54,
}

// modifierKeysym bildet Modifikatoren ab. RAlt (AltGr) wird bewusst NICHT
// weitergereicht: die damit erzeugten Zeichen kommen als Unicode über TextInput,
// ein zusätzlich gehaltenes AltGr würde am Endpunkt stören.
var modifierKeysym = map[sdl.Keycode]uint32{
	sdl.KeycodeLShift: 0xffe1,
	sdl.KeycodeRShift: 0xffe2,
	sdl.KeycodeLCtrl:  0xffe3,
	sdl.KeycodeRCtrl:  0xffe4,
	sdl.KeycodeLAlt:   0xffe9,
	sdl.KeycodeLGui:   0xffeb,
	sdl.KeycodeRGui:   0xffec,
}

func init() {
	// F1..F12 sind bei SDL-Keycodes fortlaufend; der Agent erwartet 0xffbe..0xffc9.
	for i := 0; i < 12; i++ {
		specialKeysym[sdl.KeycodeF1+sdl.Keycode(i)] = uint32(0xffbe + i)
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
// (nicht als Text) gesendet wird. LAlt statt Alt, damit AltGr (RAlt) ausgenommen bleibt.
const shortcutMods = sdl.KeymodCtrl | sdl.KeymodLAlt | sdl.KeymodGui
