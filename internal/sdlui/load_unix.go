//go:build !windows

package sdlui

import (
	"fmt"
	"os"

	"github.com/ebitengine/purego"
)

// dlopenSDL öffnet die SDL3-Laufzeit erneut (dlopen ist referenzgezählt, es ist
// dieselbe Instanz, die das purego-Binding bereits geladen hat). Zuerst neben dem
// Binary suchen – so funktioniert eine mitgelieferte Bibliothek wie beim Viewer.
func dlopenSDL(name string) (uintptr, error) {
	if p, err := purego.Dlopen("."+string(os.PathSeparator)+name, purego.RTLD_LAZY); err == nil {
		return p, nil
	}
	h, err := purego.Dlopen(name, purego.RTLD_LAZY)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return h, nil
}

func dlsymSDL(lib uintptr, name string) (uintptr, error) {
	return purego.Dlsym(lib, name)
}
