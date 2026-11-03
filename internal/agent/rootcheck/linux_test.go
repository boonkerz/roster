//go:build linux

package rootcheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsELF(t *testing.T) {
	dir := t.TempDir()
	elf := filepath.Join(dir, "a.bin")
	_ = os.WriteFile(elf, []byte("\x7fELF\x02\x01\x01"), 0o644)
	txt := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(txt, []byte("hello"), 0o644)
	if !isELF(elf) || isELF(txt) {
		t.Fatal("ELF-Erkennung")
	}
}

func TestKnownPathsGlob(t *testing.T) {
	// Nur Syntaxprüfung der Muster – auf dem Entwicklungsrechner darf nichts gefunden werden.
	for _, p := range knownRootkitPaths {
		if _, err := filepath.Match(p, ""); err != nil {
			t.Errorf("ungültiges Muster %q: %v", p, err)
		}
	}
	if f := knownPaths(); len(f) != 0 {
		t.Logf("Hinweis: Rootkit-Marker auf diesem System: %+v", f)
	}
}
