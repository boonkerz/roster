package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Live gegen Server+Agent: ROSTER_E2E_URL, ROSTER_E2E_DEVICE, ROSTER_E2E_TOKEN, ROSTER_E2E_DIR.
func TestLiveFileSession(t *testing.T) {
	url, dev, tok, dir := os.Getenv("ROSTER_E2E_URL"), os.Getenv("ROSTER_E2E_DEVICE"), os.Getenv("ROSTER_E2E_TOKEN"), os.Getenv("ROSTER_E2E_DIR")
	if url == "" {
		t.Skip("ROSTER_E2E_URL fehlt")
	}
	cfg := &launchConfig{URL: url, Device: dev, Token: tok, Session: "x"}
	fm := &fileManager{cl: newFMClient(cfg), cfg: cfg}
	t0 := time.Now()
	if _, err := fm.fileSession(); err != nil {
		t.Fatal(err)
	}
	t.Logf("sitzung aufgebaut in %v", time.Since(t0))

	local := t.TempDir()
	// viele kleine Dateien
	small := filepath.Join(local, "klein")
	os.MkdirAll(small, 0o755)
	for i := 0; i < 300; i++ {
		os.WriteFile(filepath.Join(small, fmt.Sprintf("f%03d.txt", i)), []byte(fmt.Sprintf("datei %d", i)), 0o644)
	}
	big := filepath.Join(local, "gross.bin")
	f, _ := os.Create(big)
	f.Truncate(1 << 30)
	f.Close()

	t0 = time.Now()
	if err := fm.runTransfer(context.Background(), []fmEntry{{name: "klein", path: small, dir: true}}, dir, false); err != nil {
		t.Fatal(err)
	}
	t.Logf("300 kleine Dateien hoch: %v", time.Since(t0))

	t0 = time.Now()
	if err := fm.runTransfer(context.Background(), []fmEntry{{name: "gross.bin", path: big, size: 1 << 30}}, dir, false); err != nil {
		t.Fatal(err)
	}
	d := time.Since(t0)
	t.Logf("1 GB hoch: %v (%.0f MB/s)", d, 1024/d.Seconds())

	back := t.TempDir()
	t0 = time.Now()
	if err := fm.runTransfer(context.Background(), []fmEntry{{name: "gross.bin", path: filepath.Join(dir, "gross.bin"), size: 1 << 30}}, back, true); err != nil {
		t.Fatal(err)
	}
	d = time.Since(t0)
	t.Logf("1 GB runter: %v (%.0f MB/s)", d, 1024/d.Seconds())
	if fi, err := os.Stat(filepath.Join(back, "gross.bin")); err != nil || fi.Size() != 1<<30 {
		t.Fatal("download größe")
	}
	// Vergleich alter Weg: 20 kleine Dateien über die Befehlswarteschlange
	old := filepath.Join(local, "alt")
	os.MkdirAll(old, 0o755)
	for i := 0; i < 20; i++ {
		os.WriteFile(filepath.Join(old, fmt.Sprintf("a%02d.txt", i)), []byte("x"), 0o644)
	}
	t0 = time.Now()
	if err := fm.legacyTransfer([]fmEntry{{name: "alt", path: old, dir: true}}, dir, false); err != nil {
		t.Fatal(err)
	}
	t.Logf("alter Weg, 20 kleine Dateien: %v", time.Since(t0))
}
