package api

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileHubSpool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "xfer-alt"), []byte("rest"), 0o600)
	os.WriteFile(filepath.Join(dir, "fremd.txt"), []byte("bleibt"), 0o600)
	h := newFileHub(dir)
	if _, err := os.Stat(filepath.Join(dir, "xfer-alt")); !os.IsNotExist(err) {
		t.Fatal("Reste nicht entfernt")
	}
	if _, err := os.Stat(filepath.Join(dir, "fremd.txt")); err != nil {
		t.Fatal("fremde Datei gelöscht")
	}
	n, err := h.receive("a", "dev1", "x.bin", strings.NewReader("hallo welt"), 10)
	if err != nil || n != 10 {
		t.Fatal(n, err)
	}
	h.mu.Lock()
	b, ok := h.blobs["a"]
	h.mu.Unlock()
	if !ok || b.size != 10 {
		t.Fatal("blob")
	}
	rec := httptest.NewRecorder()
	serveBlob(rec, httptest.NewRequest("GET", "/", nil), b, "ä b.bin")
	if rec.Body.String() != "hallo welt" || rec.Header().Get("Content-Length") != "10" {
		t.Fatalf("%q %v", rec.Body.String(), rec.Header())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "filename*=utf-8''%C3%A4%20b.bin") {
		t.Fatal(rec.Header().Get("Content-Disposition"))
	}
	if _, ok := h.take("a", "dev2"); ok {
		t.Fatal("fremdes Gerät durfte abholen")
	}
	if _, ok := h.take("a", "dev1"); !ok {
		t.Fatal("eigenes Gerät konnte nicht abholen")
	}
	os.Remove(b.path)
	if _, err := h.receive("len", "dev1", "", strings.NewReader("x"), -1); !errors.Is(err, errLengthRequired) {
		t.Fatal("unbekannte Länge angenommen", err)
	}
	// angekündigte Größe über dem Limit -> sofort ablehnen, nichts schreiben
	if _, err := h.receive("b", "dev1", "", bytes.NewReader(nil), maxFileTransfer+1); !errors.Is(err, errTransferTooLarge) {
		t.Fatal(err)
	}
	// kein Platz: Reserve größer als der freie Platz
	diskFreeFn = func(string) (uint64, bool) { return 3 << 30, true }
	defer func() { diskFreeFn = diskFree }()
	if _, err := h.receive("c", "dev1", "", bytes.NewReader(nil), 2<<30+1); !errors.Is(err, errSpoolFull) {
		t.Fatal("Platzprüfung", err)
	}
	if _, err := h.receive("d", "dev1", "", strings.NewReader("klein"), 5); err != nil {
		t.Fatal(err)
	}
	h.drop("d")
	if m, _ := filepath.Glob(filepath.Join(dir, "xfer-*")); len(m) != 0 {
		t.Fatal("Zwischendateien übrig", m)
	}
}

func TestSpoolDirFor(t *testing.T) {
	if d := spoolDirFor("sqlite:///var/lib/roster/inventory.db?_pragma=x"); d != "/var/lib/roster/file-transfers" {
		t.Fatal(d)
	}
	if d := spoolDirFor("postgres://u@h/db"); !strings.HasSuffix(d, "roster-transfers") {
		t.Fatal(d)
	}
}
