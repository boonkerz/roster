package remote

import (
	"strings"
	"testing"
)

func TestWlKeymap(t *testing.T) {
	km := buildWlKeymap()
	if len(km.ordered) > 247 {
		t.Fatalf("zu viele Tasten: %d", len(km.ordered))
	}
	for _, ks := range []uint32{'a', 'A', '1', 0xe4 /* ä */, 0xdf /* ß */, 0xff0d, 0xffe1, 0xffe3, 0xffeb, 0x20ac} {
		if _, ok := km.keycode(ks); !ok {
			t.Errorf("keysym %#x fehlt", ks)
		}
	}
	if c, ok := km.keycode(0x10000e4); !ok || c != km.codes[0xe4] {
		t.Error("Unicode-Keysym für ä")
	}
	ca, _ := km.keycode('a')
	cA, _ := km.keycode('A')
	if ca == cA {
		t.Error("a und A brauchen eigene Tasten")
	}
	for _, want := range []string{"xkb_keycodes", "include \"complete\"", "modifier_map Shift", "modifier_map Control", "modifier_map Mod4", "[ 0x00000061 ]"} {
		if !strings.Contains(km.text, want) {
			t.Errorf("Keymap enthält %q nicht", want)
		}
	}
}
