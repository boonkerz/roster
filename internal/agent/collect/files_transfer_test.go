package collect

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failAfter struct{ r io.Reader }

func (f failAfter) Read(b []byte) (int, error) {
	n, err := f.r.Read(b)
	if err == io.EOF {
		return n, errors.New("verbindung weg")
	}
	return n, err
}

func TestWriteFromReader(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ziel.txt")
	os.WriteFile(p, []byte("alt"), 0o600)
	// Abbruch: alte Datei bleibt, keine Zwischendatei
	if _, err := WriteFromReader(p, failAfter{strings.NewReader("neu-halb")}, 100); err == nil {
		t.Fatal("Fehler erwartet")
	}
	if b, _ := os.ReadFile(p); string(b) != "alt" {
		t.Fatal(string(b))
	}
	// Unvollständig (Größe stimmt nicht)
	if _, err := WriteFromReader(p, strings.NewReader("abc"), 5); err == nil {
		t.Fatal("unvollständig nicht erkannt")
	}
	var seen int64
	n, err := WriteFromReader(p, CountingReader(strings.NewReader("neuer inhalt"), func(x int64) { seen = x }), 12)
	if err != nil || n != 12 || seen != 12 {
		t.Fatal(n, err, seen)
	}
	b, _ := os.ReadFile(p)
	fi, _ := os.Stat(p)
	if string(b) != "neuer inhalt" || fi.Mode().Perm() != 0o600 {
		t.Fatal(string(b), fi.Mode())
	}
	if m, _ := filepath.Glob(filepath.Join(dir, ".roster-upload-*")); len(m) != 0 {
		t.Fatal(m)
	}
	if FormatTransfer(512<<20, 1024<<20) != "512 / 1024 MB (50 %)" {
		t.Fatal(FormatTransfer(512<<20, 1024<<20))
	}
}
