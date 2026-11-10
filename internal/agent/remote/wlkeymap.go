package remote

import (
	"fmt"
	"strings"
)

// XKB-Keymap für die virtuelle Wayland-Tastatur: jeder unterstützte Keysym bekommt einen
// eigenen Keycode mit nur einer Ebene. So müssen wir das Layout des Geräts nicht kennen –
// der VNC-Client schickt Keysyms (inkl. Shift/Ctrl/Alt separat), wir drücken die passende
// Taste. Rein, ohne libxkbcommon; der Compositor kompiliert die Keymap selbst.

type wlKeymap struct {
	text    string
	codes   map[uint32]uint32 // keysym -> evdev-Keycode (xkb - 8)
	mods    map[uint32]string // keysym -> Modifier-Name
	ordered []uint32
}

var wlSpecialKeysyms = []uint32{
	0xff08, 0xff09, 0xff0d, 0xff1b, 0xffff, 0xff50, 0xff51, 0xff52, 0xff53, 0xff54, 0xff55, 0xff56, 0xff57,
	0xff63, 0xff67, 0xff61, 0xff13, 0xff14, 0xff7f, 0xff8d,
	0xffbe, 0xffbf, 0xffc0, 0xffc1, 0xffc2, 0xffc3, 0xffc4, 0xffc5, 0xffc6, 0xffc7, 0xffc8, 0xffc9,
	0xffe1, 0xffe2, 0xffe3, 0xffe4, 0xffe5, 0xffe9, 0xffea, 0xffeb, 0xffec, 0xfe03, 0xffe7, 0xffe8,
	0x20ac, // €
}

var wlModifierMap = map[uint32]string{
	0xffe1: "Shift", 0xffe2: "Shift", 0xffe3: "Control", 0xffe4: "Control", 0xffe5: "Lock",
	0xffe9: "Mod1", 0xffea: "Mod1", 0xffe7: "Mod1", 0xffe8: "Mod1", 0xffeb: "Mod4", 0xffec: "Mod4", 0xfe03: "Mod5",
}

func buildWlKeymap() *wlKeymap {
	km := &wlKeymap{codes: map[uint32]uint32{}, mods: map[uint32]string{}}
	var syms []uint32
	syms = append(syms, wlSpecialKeysyms...)
	for k := uint32(0x20); k <= 0x7e; k++ {
		syms = append(syms, k)
	}
	for k := uint32(0xa0); k <= 0xff; k++ {
		syms = append(syms, k)
	}
	var keycodes, symbols strings.Builder
	code := uint32(9)
	maxCode := code
	for _, ks := range syms {
		if code > 255 {
			break
		}
		if _, dup := km.codes[ks]; dup {
			continue
		}
		km.codes[ks] = code - 8
		km.ordered = append(km.ordered, ks)
		fmt.Fprintf(&keycodes, "    <K%d> = %d;\n", code, code)
		fmt.Fprintf(&symbols, "    key <K%d> { [ 0x%08x ] };\n", code, ks)
		if m, ok := wlModifierMap[ks]; ok {
			km.mods[ks] = m
			fmt.Fprintf(&symbols, "    modifier_map %s { <K%d> };\n", m, code)
		}
		maxCode = code
		code++
	}
	km.text = fmt.Sprintf("xkb_keymap {\n  xkb_keycodes \"roster\" {\n    minimum = 8;\n    maximum = %d;\n%s  };\n  xkb_types \"roster\" { include \"complete\" };\n  xkb_compat \"roster\" { include \"complete\" };\n  xkb_symbols \"roster\" {\n%s  };\n};\n",
		maxCode, keycodes.String(), symbols.String())
	return km
}

// keycode liefert den evdev-Keycode für einen Keysym; Großbuchstaben ohne eigenen Eintrag
// fallen auf den Kleinbuchstaben zurück (Shift kommt vom Client).
func (km *wlKeymap) keycode(ks uint32) (uint32, bool) {
	if c, ok := km.codes[ks]; ok {
		return c, true
	}
	if ks >= 0x1000000 { // Unicode-Keysym für Latin-1 auf den direkten Wert abbilden
		if c, ok := km.codes[ks-0x1000000]; ok && ks-0x1000000 <= 0xff {
			return c, true
		}
	}
	return 0, false
}
