package clientinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInstallAndRemoveExecutable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "bin")
	dst, err := InstallExecutable(dir, ExeName("roster-testtool"))
	if err != nil {
		t.Fatalf("InstallExecutable: %v", err)
	}
	self, _ := Self()
	want, _ := os.ReadFile(self)
	got, err := os.ReadFile(dst)
	if err != nil || len(got) != len(want) {
		t.Fatalf("kopie unvollständig: err=%v len=%d want=%d", err, len(got), len(want))
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(dst); fi.Mode().Perm()&0o111 == 0 {
			t.Fatalf("kopie nicht ausführbar: %v", fi.Mode())
		}
	}
	// Zweiter Lauf überschreibt ohne Fehler (ersetzt eine ältere Installation).
	if _, err := InstallExecutable(dir, ExeName("roster-testtool")); err != nil {
		t.Fatalf("erneutes InstallExecutable: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("temporäre Dateien liegen gelassen: %v", entries)
	}
	if err := RemoveExecutable(dir, ExeName("roster-testtool")); err != nil {
		t.Fatalf("RemoveExecutable: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("leeres Verzeichnis sollte entfernt sein: %v", err)
	}
}

func TestPathHint(t *testing.T) {
	dir := t.TempDir()
	if h := PathHint(dir); h == "" {
		t.Fatal("PathHint sollte vor fehlendem PATH-Eintrag warnen")
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if h := PathHint(dir); h != "" {
		t.Fatalf("PathHint = %q, want leer", h)
	}
}
