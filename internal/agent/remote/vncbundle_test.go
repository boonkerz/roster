package remote

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestUnzipTo(t *testing.T) {
	// ZIP im Speicher bauen: eine Datei im Wurzelverzeichnis + eine in einem Ordner.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range map[string]string{
		"winvnc.exe":   "MZ-fake",
		"sub/extra.dll": "dll-bytes",
	} {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(content))
	}
	// Zip-Slip-Versuch – muss ignoriert werden.
	if f, err := zw.Create("../evil.txt"); err == nil {
		_, _ = f.Write([]byte("nope"))
	}
	_ = zw.Close()

	dir := t.TempDir()
	if err := unzipTo(buf.Bytes(), dir); err != nil {
		t.Fatalf("unzipTo: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "winvnc.exe")); err != nil || string(b) != "MZ-fake" {
		t.Fatalf("winvnc.exe: %q / %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "sub", "extra.dll")); err != nil || string(b) != "dll-bytes" {
		t.Fatalf("extra.dll: %q / %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); err == nil {
		t.Fatalf("Zip-Slip nicht verhindert")
	}
}
