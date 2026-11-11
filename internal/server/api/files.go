package api

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// maxFileTransfer begrenzt eine Datei-Übertragung. Die Daten liegen nie im Speicher, sondern
// als Zwischendatei im Transfer-Verzeichnis (neben der Datenbank) – zusätzlich muss dort
// genug Platz frei bleiben (spoolReserve).
const (
	maxFileTransfer = 4 << 30 // 4 GiB
	spoolReserve    = 1 << 30 // so viel muss nach der Übertragung frei bleiben
	spoolTTL        = 30 * time.Minute
)

var diskFreeFn = diskFree // in Tests ersetzbar

var (
	errTransferTooLarge = errors.New("Datei zu groß (max 4 GB)")
	errSpoolFull        = errors.New("zu wenig freier Speicher auf dem Server für diese Datei")
)

// fileBlob ist eine zwischengelagerte Datei-Übertragung (Download vom Agent oder
// Upload vom Browser) als Datei im Transfer-Verzeichnis, bis zur Abholung/TTL.
type fileBlob struct {
	name string
	path string
	size int64
	ts   time.Time
}

// fileHub hält laufende Datei-Übertragungen (keyed by Transfer-/Command-ID).
type fileHub struct {
	dir   string
	mu    sync.Mutex
	blobs map[string]fileBlob
}

// spoolDirFor legt das Transfer-Verzeichnis neben eine SQLite-Datenbank (dort ist Platz,
// /tmp ist oft ein RAM-Dateisystem); sonst ins Temp-Verzeichnis.
func spoolDirFor(dbURL string) string {
	if p, ok := strings.CutPrefix(dbURL, "sqlite://"); ok && p != "" && p != ":memory:" {
		if i := strings.IndexByte(p, '?'); i >= 0 {
			p = p[:i]
		}
		return filepath.Join(filepath.Dir(p), "file-transfers")
	}
	return filepath.Join(os.TempDir(), "roster-transfers")
}

func newFileHub(dir string) *fileHub {
	_ = os.MkdirAll(dir, 0o700)
	if old, err := filepath.Glob(filepath.Join(dir, "xfer-*")); err == nil { // Reste eines früheren Laufs
		for _, f := range old {
			os.Remove(f)
		}
	}
	h := &fileHub{dir: dir, blobs: map[string]fileBlob{}}
	go h.gc()
	return h
}

// receive schreibt einen Datenstrom als Zwischendatei und registriert ihn unter id.
// declared ist die angekündigte Größe (Content-Length, -1 = unbekannt).
func (h *fileHub) receive(id, name string, r io.Reader, declared int64) (int64, error) {
	if declared > maxFileTransfer {
		return 0, errTransferTooLarge
	}
	if free, ok := diskFreeFn(h.dir); ok && declared > 0 && free < uint64(declared)+spoolReserve {
		return 0, errSpoolFull
	}
	_ = os.MkdirAll(h.dir, 0o700)
	f, err := os.CreateTemp(h.dir, "xfer-*")
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxFileTransfer+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxFileTransfer {
		err = errTransferTooLarge
	}
	if err != nil {
		os.Remove(f.Name())
		return 0, err
	}
	h.mu.Lock()
	if old, ok := h.blobs[id]; ok {
		os.Remove(old.path)
	}
	h.blobs[id] = fileBlob{name: name, path: f.Name(), size: n, ts: time.Now()}
	h.mu.Unlock()
	return n, nil
}

// take entnimmt eine Übertragung; der Aufrufer liefert sie aus und ruft danach done().
func (h *fileHub) take(id string) (fileBlob, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.blobs[id]
	if ok {
		delete(h.blobs, id)
	}
	return b, ok
}

// drop entfernt eine Übertragung samt Datei.
func (h *fileHub) drop(id string) {
	if b, ok := h.take(id); ok {
		os.Remove(b.path)
	}
}

// serveBlob streamt eine Zwischendatei (mit Content-Length und Range-Unterstützung).
func serveBlob(w http.ResponseWriter, r *http.Request, b fileBlob, attachment string) {
	f, err := os.Open(b.path)
	if err != nil {
		http.Error(w, "Datei nicht (mehr) verfügbar", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if attachment != "" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment}))
	}
	http.ServeContent(w, r, "", time.Time{}, f)
}

// gc entfernt Übertragungen, die länger als spoolTTL nicht angefasst wurden. Eine gerade
// gelesene Datei bleibt dank offenem Dateihandle bis zum Ende der Auslieferung lesbar.
func (h *fileHub) gc() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		h.mu.Lock()
		for id, b := range h.blobs {
			if time.Since(b.ts) > spoolTTL {
				os.Remove(b.path)
				delete(h.blobs, id)
			}
		}
		h.mu.Unlock()
	}
}

// streamingAgentVersion: ab dieser Agent-Version werden Dateien gestreamt; ältere Agents
// holen höchstens 32 MB ab (und würden größere Dateien abgeschnitten schreiben).
const (
	streamingAgentVersion = "0.27.0"
	legacyTransferLimit   = 32 << 20
)

// agentCanReceive lehnt große Uploads an veraltete Agents ab, bevor die Bytes fließen.
func (s *Server) agentCanReceive(w http.ResponseWriter, r *http.Request, deviceID string) bool {
	if r.ContentLength >= 0 && r.ContentLength <= legacyTransferLimit {
		return true
	}
	d, err := s.store.GetDevice(r.Context(), deviceID)
	if err != nil || agentSupportsVersion(d.AgentVersion, streamingAgentVersion) {
		return true
	}
	s.writeErr(w, http.StatusConflict, "Der Agent auf dem Gerät ("+d.AgentVersion+") kann nur Dateien bis 32 MB empfangen – bitte auf "+streamingAgentVersion+" oder neuer aktualisieren.")
	return false
}

// receiveErr schreibt die passende Antwort zu einem receive-Fehler.
func (s *Server) receiveErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTransferTooLarge):
		s.writeErr(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, errSpoolFull):
		s.writeErr(w, http.StatusInsufficientStorage, err.Error())
	default:
		s.log.Warn("datei-übertragung", "err", err)
		s.writeErr(w, http.StatusBadRequest, "Übertragung abgebrochen: "+err.Error())
	}
}

// --- Agent-Endpoints (Bearer-Token) ---

// handleAgentFileUpload nimmt die vom Agent gelesene Datei entgegen (Download-Richtung).
func (s *Server) handleAgentFileUpload(w http.ResponseWriter, r *http.Request) {
	cmd := r.URL.Query().Get("cmd")
	if cmd == "" {
		s.writeErr(w, http.StatusBadRequest, "cmd fehlt")
		return
	}
	if _, err := s.files.receive(cmd, r.URL.Query().Get("name"), r.Body, r.ContentLength); err != nil {
		s.receiveErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAgentFileDownload liefert dem Agent die vom Browser hochgeladenen Bytes (Upload-Richtung).
func (s *Server) handleAgentFileDownload(w http.ResponseWriter, r *http.Request) {
	// Entnehmen statt nur ansehen: nach der Auslieferung wird die Zwischendatei sofort
	// gelöscht (das offene Handle hält sie bis dahin lesbar).
	b, ok := s.files.take(r.URL.Query().Get("cmd"))
	if !ok {
		s.writeErr(w, http.StatusNotFound, "kein Transfer")
		return
	}
	defer os.Remove(b.path)
	serveBlob(w, r, b, "")
}

// --- Nutzer-Endpoints (Techniker/Admin) ---

// handleBrowse reiht eine Verzeichnisauflistung ein (Ergebnis per Polling).
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "browse_dir", "Dateien auflisten",
		map[string]any{"path": req.Path})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleReadFile reiht einen Datei-Download ein (Agent lädt die Datei zum Server).
func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		s.writeErr(w, http.StatusBadRequest, "pfad fehlt")
		return
	}
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "read_file", "Datei lesen: "+req.Path,
		map[string]any{"path": req.Path})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleServeFile streamt die vom Agent hochgeladene Datei an den Browser.
func (s *Server) handleServeFile(w http.ResponseWriter, r *http.Request) {
	b, ok := s.files.take(chi.URLParam(r, "cmd"))
	if !ok {
		s.writeErr(w, http.StatusNotFound, "Datei nicht (mehr) verfügbar")
		return
	}
	defer os.Remove(b.path)
	name := b.name
	if name == "" {
		name = "download"
	}
	serveBlob(w, r, b, name)
}

// handleWriteFile nimmt eine hochgeladene Datei entgegen und reiht das Schreiben ein.
// Body = rohe Datei; Pfad im Query-Parameter „path".
func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		s.writeErr(w, http.StatusBadRequest, "path fehlt")
		return
	}
	if !s.agentCanReceive(w, r, chi.URLParam(r, "id")) {
		return
	}
	xfer := newSessionID()
	size, err := s.files.receive(xfer, "", r.Body, r.ContentLength)
	if err != nil {
		s.receiveErr(w, err)
		return
	}
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "write_file", "Datei schreiben: "+path,
		map[string]any{"path": path, "xfer": xfer, "size": size})
	if err != nil {
		s.files.drop(xfer)
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}
