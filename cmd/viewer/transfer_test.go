package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/boonkerz/roster/internal/agent/remote"
	"github.com/boonkerz/roster/internal/shared"
)

// fileSessionPair startet einen „Agent“ (ServeFileSession) hinter einem WS-Server und
// verbindet einen fsClient – ohne Roster-Server dazwischen (der reicht nur durch).
func fileSessionPair(t *testing.T) (*fsClient, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		c.SetReadLimit(8 << 20)
		remote.ServeFileSession(r.Context(), c, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}))
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetReadLimit(8 << 20)
	c := &fsClient{conn: conn}
	if _, err := c.call(shared.FileReq{Op: "hello"}, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	return c, func() { c.close(); srv.Close() }
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func TestFileSessionPutGetResume(t *testing.T) {
	c, done := fileSessionPair(t)
	defer done()
	dir := t.TempDir()
	data := randBytes(3*shared.FileChunk + 1234)
	dst := filepath.Join(dir, "sub", "ziel.bin") // Elternordner wird angelegt

	// Teildatei simulieren (abgebrochener Upload): erste 700 KB liegen schon da.
	os.MkdirAll(filepath.Dir(dst), 0o755)
	os.WriteFile(dst+shared.FilePartSuffix, data[:700<<10], 0o644)
	st, err := c.call(shared.FileReq{Op: "stat", Path: dst}, 5*time.Second)
	if err != nil || st.Exists || st.Part != 700<<10 {
		t.Fatalf("stat %+v %v", st, err)
	}
	var last int64
	if err := c.put(context.Background(), dst, bytes.NewReader(data), int64(len(data)), st.Part, func(n int64) { last = n }); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data) || last != int64(len(data)) {
		t.Fatal("upload mit Fortsetzen falsch")
	}
	if _, err := os.Stat(dst + shared.FilePartSuffix); !os.IsNotExist(err) {
		t.Fatal("Teildatei übrig")
	}
	// Falscher Offset (Teildatei passt nicht) -> Agent beginnt bei 0.
	if err := c.put(context.Background(), dst, bytes.NewReader(data[:10]), 10, 5, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, data[:10]) {
		t.Fatal("überschreiben")
	}
	// Download ab Offset.
	os.WriteFile(dst, data, 0o644)
	var buf bytes.Buffer
	size, err := c.get(context.Background(), dst, 1000, &buf, nil)
	if err != nil || size != int64(len(data)) || !bytes.Equal(buf.Bytes(), data[1000:]) {
		t.Fatal("get ab offset", err)
	}
	// Fehler: fehlende Datei -> fachlicher Fehler, Verbindung bleibt nutzbar.
	if _, err := c.get(context.Background(), filepath.Join(dir, "fehlt"), 0, &buf, nil); !isRemoteErr(err) {
		t.Fatal("remoteErr erwartet", err)
	}
	if _, err := c.call(shared.FileReq{Op: "walk", Path: dir}, 5*time.Second); err != nil {
		t.Fatal("verbindung nach fehler kaputt", err)
	}
	// Löschen der Wurzel wird verweigert.
	if _, err := c.call(shared.FileReq{Op: "delete", Path: "/"}, 5*time.Second); !isRemoteErr(err) {
		t.Fatal("wurzel löschen", err)
	}
}

// Ende-zu-Ende über fileManager: Ordner hochladen, Konfliktregeln, Download zurück.
func TestTransferPolicies(t *testing.T) {
	c, done := fileSessionPair(t)
	defer done()
	fm := &fileManager{fs: c}
	local, remoteDir := t.TempDir(), t.TempDir()
	src := filepath.Join(local, "projekt")
	os.MkdirAll(filepath.Join(src, "a", "leer"), 0o755)
	os.WriteFile(filepath.Join(src, "a", "x.txt"), []byte("neu-x"), 0o644)
	os.WriteFile(filepath.Join(src, "y.txt"), []byte("neu-y"), 0o644)
	os.WriteFile(filepath.Join(src, "gross.bin"), randBytes(2*shared.FileChunk), 0o644)
	os.WriteFile(filepath.Join(src, "rest"+shared.FilePartSuffix), []byte("nie mitkopieren"), 0o644)
	// Am Ziel: x.txt gleich groß (anderer Inhalt), y.txt anders groß.
	dst := filepath.Join(remoteDir, "projekt")
	os.MkdirAll(filepath.Join(dst, "a"), 0o755)
	os.WriteFile(filepath.Join(dst, "a", "x.txt"), []byte("alt-x"), 0o644)
	os.WriteFile(filepath.Join(dst, "y.txt"), []byte("alt-yy"), 0o644)

	items := []fmEntry{{name: "projekt", path: src, dir: true}}
	answer := func(policy string, want string) {
		go func() {
			for i := 0; i < 200; i++ {
				fm.mu.Lock()
				if fm.prompt.kind == "conflict" {
					if !strings.HasPrefix(fm.prompt.label, want+" Datei") {
						t.Errorf("label %q", fm.prompt.label)
					}
					fm.answerLocked(policy)
					fm.mu.Unlock()
					return
				}
				fm.mu.Unlock()
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	answer(policySkipSame, "2")
	if err := fm.runTransfer(context.Background(), items, remoteDir, false); err != nil {
		t.Fatal(err)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	if read(filepath.Join(dst, "a", "x.txt")) != "alt-x" || read(filepath.Join(dst, "y.txt")) != "neu-y" {
		t.Fatal("skipsame falsch", read(filepath.Join(dst, "a", "x.txt")), read(filepath.Join(dst, "y.txt")))
	}
	if fi, err := os.Stat(filepath.Join(dst, "a", "leer")); err != nil || !fi.IsDir() {
		t.Fatal("leerer Ordner fehlt")
	}
	if _, err := os.Stat(filepath.Join(dst, "rest"+shared.FilePartSuffix)); !os.IsNotExist(err) {
		t.Fatal("Teildatei mitkopiert")
	}
	if fm.xfer.skipped != 1 || fm.xfer.done != 3 {
		t.Fatalf("zähler %+v", fm.xfer)
	}
	// Überschreiben.
	fm.xfer = xferState{}
	answer(policyOverwrite, "3")
	if err := fm.runTransfer(context.Background(), items, remoteDir, false); err != nil {
		t.Fatal(err)
	}
	if read(filepath.Join(dst, "a", "x.txt")) != "neu-x" {
		t.Fatal("overwrite")
	}
	// Download des Ordners zurück in ein leeres lokales Ziel (keine Rückfrage).
	back := t.TempDir()
	fm.xfer = xferState{}
	if err := fm.runTransfer(context.Background(), []fmEntry{{name: "projekt", path: dst, dir: true}}, back, true); err != nil {
		t.Fatal(err)
	}
	if read(filepath.Join(back, "projekt", "gross.bin")) != read(filepath.Join(src, "gross.bin")) || read(filepath.Join(back, "projekt", "a", "x.txt")) != "neu-x" {
		t.Fatal("download")
	}
	// Abgebrochener Download wird fortgesetzt: Teildatei liegt schon da.
	g := filepath.Join(back, "projekt", "gross.bin")
	full := read(g)
	os.Remove(g)
	os.WriteFile(g+shared.FilePartSuffix, []byte(full[:1000]), 0o644)
	fm.xfer = xferState{}
	answer(policyOverwrite, "2") // x.txt und y.txt existieren, gross.bin nur als Teildatei
	if err := fm.runTransfer(context.Background(), []fmEntry{{name: "projekt", path: dst, dir: true}}, back, true); err != nil {
		t.Fatal(err)
	}
	if read(g) != full {
		t.Fatal("fortgesetzter download")
	}
}
