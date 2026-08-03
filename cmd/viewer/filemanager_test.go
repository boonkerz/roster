package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteJoin(t *testing.T) {
	cases := []struct{ dir, name, want string }{
		{"/home/thomas", "a.txt", "/home/thomas/a.txt"},
		{"/home/thomas/", "a.txt", "/home/thomas/a.txt"},
		{`C:\Users\t`, "a.txt", `C:\Users\t\a.txt`},
		{`C:\`, "a.txt", `C:\a.txt`},
		{"", "a.txt", "a.txt"},
	}
	for _, c := range cases {
		if got := remoteJoin(c.dir, c.name); got != c.want {
			t.Errorf("remoteJoin(%q,%q) = %q, want %q", c.dir, c.name, got, c.want)
		}
	}
}

func TestHTTPBase(t *testing.T) {
	cases := map[string]string{
		"https://x.de":     "https://x.de",
		"http://x.de:8080": "http://x.de:8080",
		"wss://x.de":       "https://x.de",
		"ws://x.de":        "http://x.de",
		"x.de":             "https://x.de",
	}
	for in, want := range cases {
		if got := httpBase(in); got != want {
			t.Errorf("httpBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{0: "0B", 512: "512B", 2048: "2K", 3 << 20: "3.0M", 5 << 30: "5.0G"}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestFMClientBrowse prüft den kompletten Client-Protokollfluss (queue → poll →
// FileListing) gegen einen Stub, der die viewer-files-API nachbildet.
func TestFMClientBrowse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/viewer-files/browse"):
			_ = json.NewEncoder(w).Encode(map[string]string{"command_id": "c1"})
		case strings.Contains(r.URL.Path, "/viewer-files/command/"):
			listing := `{"path":"/home","parent":"/","entries":[{"name":"a.txt","path":"/home/a.txt","dir":false,"size":10},{"name":"sub","path":"/home/sub","dir":true}]}`
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "done", "exit_code": 0, "output": listing})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cl := &fmClient{http: srv.Client(), base: srv.URL, device: "dev1", token: "tok"}
	l := cl.browse("/home")
	if l.err != "" {
		t.Fatalf("browse err: %s", l.err)
	}
	if l.path != "/home" || len(l.entries) != 2 {
		t.Fatalf("unerwartetes Listing: %+v", l)
	}
	if !l.entries[1].dir || l.entries[0].size != 10 {
		t.Errorf("Einträge falsch geparst: %+v", l.entries)
	}
}

// TestCopyTreeRemoteToLocal überträgt rekursiv einen Geräte-Ordner (mit Unterordner)
// ins lokale Dateisystem und prüft, dass Struktur + Inhalte ankommen. Der Stub bildet
// browse/read/blob der viewer-files-API nach; die Command-ID kodiert den Zielpfad.
func TestCopyTreeRemoteToLocal(t *testing.T) {
	files := map[string]string{
		"/src/file1.txt":        "hello",
		"/src/subdir/file2.txt": "world",
	}
	browseOut := map[string]string{
		"/src":        `{"path":"/src","parent":"/","entries":[{"name":"file1.txt","path":"/src/file1.txt","dir":false,"size":5},{"name":"subdir","path":"/src/subdir","dir":true}]}`,
		"/src/subdir": `{"path":"/src/subdir","parent":"/src","entries":[{"name":"file2.txt","path":"/src/subdir/file2.txt","dir":false,"size":5}]}`,
	}
	enc := func(kind, p string) string { return kind + "." + base64.RawURLEncoding.EncodeToString([]byte(p)) }
	dec := func(id string) (kind, p string) {
		i := strings.IndexByte(id, '.')
		b, _ := base64.RawURLEncoding.DecodeString(id[i+1:])
		return id[:i], string(b)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/viewer-files/browse"):
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]string{"command_id": enc("browse", body.Path)})
		case strings.HasSuffix(r.URL.Path, "/viewer-files/read"):
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]string{"command_id": enc("read", body.Path)})
		case strings.Contains(r.URL.Path, "/viewer-files/command/"):
			kind, p := dec(path.Base(r.URL.Path))
			out := ""
			if kind == "browse" {
				out = browseOut[p]
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "done", "exit_code": 0, "output": out})
		case strings.Contains(r.URL.Path, "/viewer-files/blob/"):
			_, p := dec(path.Base(r.URL.Path))
			_, _ = w.Write([]byte(files[p]))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	fm := &fileManager{cl: &fmClient{http: srv.Client(), base: srv.URL, device: "d", token: "tok"}}
	dst := t.TempDir()
	count := 0
	src := fmEntry{name: "src", path: "/src", dir: true}
	if err := fm.copyTree(src, dst, true, false, &count); err != nil {
		t.Fatalf("copyTree: %v", err)
	}
	if count != 2 {
		t.Errorf("erwartete 2 kopierte Dateien, bekam %d", count)
	}
	for rel, want := range map[string]string{
		"src/file1.txt":        "hello",
		"src/subdir/file2.txt": "world",
	} {
		got, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("lesen %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

func TestFMClientBrowseAuthFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	cl := &fmClient{http: srv.Client(), base: srv.URL, device: "dev1", token: "bad"}
	if l := cl.browse("/x"); l.err == "" {
		t.Error("erwartete einen Fehler bei 401")
	}
}
