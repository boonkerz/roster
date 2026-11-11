package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// TestFileAuditBatch: Einzelaktionen je ein Eintrag, Sammelübertragungen (begin…end) ein
// zusammengefasster Eintrag mit Anzahl und Datenmenge – auch ohne „end“ (Abbruch).
func TestFileAuditBatch(t *testing.T) {
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := &Server{store: st, cfg: config.Config{}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	fa := &fileAudit{s: s, r: httptest.NewRequest("GET", "/", nil), user: "admin"}
	req := func(r shared.FileReq) { b, _ := json.Marshal(r); fa.request(b) }
	resp := func(r shared.FileResp) { b, _ := json.Marshal(r); fa.response(b) }

	req(shared.FileReq{Op: "put", Path: "/tmp/einzeln.txt", Size: 2048})
	req(shared.FileReq{Op: "list", Path: "/tmp"}) // nicht protokolliert

	req(shared.FileReq{Op: "begin", Path: "/srv/ziel", Label: "projekt/, readme.txt"})
	req(shared.FileReq{Op: "mkdir", Path: "/srv/ziel/projekt"})
	for i := 0; i < 300; i++ {
		req(shared.FileReq{Op: "put", Path: "/srv/ziel/projekt/x", Size: 1 << 20})
	}
	req(shared.FileReq{Op: "put", Path: "/srv/ziel/gross", Size: 10 << 20, Offset: 4 << 20})
	req(shared.FileReq{Op: "end"})

	req(shared.FileReq{Op: "begin", Path: "/home/me/Downloads", Label: "logs/"})
	req(shared.FileReq{Op: "get", Path: "/var/log/a"})
	resp(shared.FileResp{OK: true, Size: 3 << 20})
	resp(shared.FileResp{OK: true, EOF: true})
	req(shared.FileReq{Op: "get", Path: "/var/log/b"})
	resp(shared.FileResp{OK: true, Size: 1 << 20})
	fa.flush() // Viewer weg ohne „end“

	items, _ := st.ListAudit(context.Background(), 50)
	var got []string
	for _, it := range items {
		got = append(got, it.Action)
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{
		"Datei hochgeladen (Viewer): /tmp/einzeln.txt (2 KB)",
		"Hochgeladen (Viewer): projekt/, readme.txt → /srv/ziel – 301 Datei(en), 306.0 MB, 1 Ordner, 1 fortgesetzt",
		"Heruntergeladen (Viewer): logs/ → lokal /home/me/Downloads – 2 Datei(en), 4.0 MB",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("fehlt: %q\nvorhanden:\n%s", want, all)
		}
	}
	if len(got) != 3 {
		t.Fatalf("%d Einträge statt 3:\n%s", len(got), all)
	}
	if items[0].Username != "admin" || items[0].Method != "FILE" {
		t.Fatalf("%+v", items[0])
	}
}
