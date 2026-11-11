package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// fileCapTTL ist die (gleitende) Lebensdauer einer Datei-Capability des nativen
// Viewers. Bewusst von der VNC-Session entkoppelt (die nach remoteSessionTTL aus
// der Session-Map entfernt wird), damit der Dateimanager auch in langen Sitzungen
// funktioniert. Jede Nutzung verlängert die Frist.
const fileCapTTL = 30 * time.Minute

type fileCap struct {
	deviceID string
	user     string // wer die Fernsteuerung gestartet hat (für das Audit-Log)
	expiry   time.Time
}

// fileCapHub bildet ein Viewer-Datei-Token auf ein Gerät ab. Das Token ist eine
// hochentropische 128-bit-Capability (newSessionID), die nur ein Admin via
// /remote/start erhält – daher genügt der Map-Lookup als Prüfung.
type fileCapHub struct {
	mu   sync.Mutex
	caps map[string]fileCap
}

func newFileCapHub() *fileCapHub {
	h := &fileCapHub{caps: map[string]fileCap{}}
	go h.gc()
	return h
}

func (h *fileCapHub) add(token, deviceID string) { h.addFor(token, deviceID, "") }

func (h *fileCapHub) addFor(token, deviceID, user string) {
	h.mu.Lock()
	h.caps[token] = fileCap{deviceID: deviceID, user: user, expiry: time.Now().Add(fileCapTTL)}
	h.mu.Unlock()
}

// userOf liefert den Nutzer, für den das Token ausgestellt wurde.
func (h *fileCapHub) userOf(token string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.caps[token].user
}

// resolve liefert die Geräte-ID zum Token und verlängert die Frist (sliding TTL).
func (h *fileCapHub) resolve(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.caps[token]
	if !ok || time.Now().After(c.expiry) {
		return "", false
	}
	c.expiry = time.Now().Add(fileCapTTL)
	h.caps[token] = c
	return c.deviceID, true
}

func (h *fileCapHub) gc() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		h.mu.Lock()
		for tok, c := range h.caps {
			if time.Now().After(c.expiry) {
				delete(h.caps, tok)
			}
		}
		h.mu.Unlock()
	}
}

// auth prüft das Viewer-Token (Bearer-Header oder ?token=) und stellt sicher, dass
// es zum Gerät der URL passt. Liefert die Geräte-ID oder schreibt 401/403.
func (s *Server) viewerFileAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	tok := bearerToken(r)
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	devID, ok := s.fileCaps.resolve(tok)
	if !ok {
		s.writeErr(w, http.StatusUnauthorized, "viewer-token ungültig oder abgelaufen")
		return "", false
	}
	if devID != chi.URLParam(r, "id") {
		s.writeErr(w, http.StatusForbidden, "token gilt nicht für dieses Gerät")
		return "", false
	}
	return devID, true
}

// handleViewerBrowse reiht – wie die Web-UI – eine Verzeichnisauflistung ein.
func (s *Server) handleViewerBrowse(w http.ResponseWriter, r *http.Request) {
	devID, ok := s.viewerFileAuth(w, r)
	if !ok {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	id, err := s.queueCommand(r.Context(), devID, "browse_dir", "Dateien auflisten",
		map[string]any{"path": req.Path})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleViewerReadFile reiht einen Datei-Download ein (Agent lädt die Datei zum Server).
func (s *Server) handleViewerReadFile(w http.ResponseWriter, r *http.Request) {
	devID, ok := s.viewerFileAuth(w, r)
	if !ok {
		return
	}
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
	id, err := s.queueCommand(r.Context(), devID, "read_file", "Datei lesen: "+req.Path,
		map[string]any{"path": req.Path})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleViewerWriteFile nimmt Bytes entgegen und reiht das Schreiben ein (Upload).
func (s *Server) handleViewerWriteFile(w http.ResponseWriter, r *http.Request) {
	devID, ok := s.viewerFileAuth(w, r)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		s.writeErr(w, http.StatusBadRequest, "path fehlt")
		return
	}
	if !s.agentCanReceive(w, r, devID) {
		return
	}
	xfer := newSessionID()
	size, err := s.files.receive(xfer, "", r.Body, r.ContentLength)
	if err != nil {
		s.receiveErr(w, err)
		return
	}
	id, err := s.queueCommand(r.Context(), devID, "write_file", "Datei schreiben: "+path,
		map[string]any{"path": path, "xfer": xfer, "size": size})
	if err != nil {
		s.files.drop(xfer)
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleViewerMkdir reiht das Anlegen eines Verzeichnisses ein.
func (s *Server) handleViewerMkdir(w http.ResponseWriter, r *http.Request) {
	devID, ok := s.viewerFileAuth(w, r)
	if !ok {
		return
	}
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
	id, err := s.queueCommand(r.Context(), devID, "make_dir", "Ordner anlegen: "+req.Path,
		map[string]any{"path": req.Path})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleViewerDelete reiht das Löschen einer Datei/eines Verzeichnisses ein.
func (s *Server) handleViewerDelete(w http.ResponseWriter, r *http.Request) {
	devID, ok := s.viewerFileAuth(w, r)
	if !ok {
		return
	}
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
	id, err := s.queueCommand(r.Context(), devID, "delete_path", "Löschen: "+req.Path,
		map[string]any{"path": req.Path})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleViewerCommand liefert den Status/Ergebnis eines eingereihten Befehls
// (Polling), auf das Gerät der Capability beschränkt.
func (s *Server) handleViewerCommand(w http.ResponseWriter, r *http.Request) {
	devID, ok := s.viewerFileAuth(w, r)
	if !ok {
		return
	}
	cmd, err := s.store.CommandByID(r.Context(), chi.URLParam(r, "cmd"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if cmd.DeviceID != devID {
		s.writeErr(w, http.StatusForbidden, "fremdes gerät")
		return
	}
	s.writeJSON(w, http.StatusOK, cmd)
}

// handleViewerFileSession eröffnet eine Datei-Sitzung: der Agent wird geweckt und
// verbindet sich, der Viewer kommt über handleViewerFileWS dazu (Server relayed nur).
func (s *Server) handleViewerFileSession(w http.ResponseWriter, r *http.Request) {
	devID, ok := s.viewerFileAuth(w, r)
	if !ok {
		return
	}
	tok := bearerToken(r)
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	sessID := newSessionID()
	sess := &termSession{
		deviceID:  devID,
		agentConn: make(chan *websocket.Conn, 1),
		done:      make(chan struct{}),
		token:     tok,
	}
	s.term.addSession(sessID, sess)
	go func() {
		select {
		case <-sess.done:
		case <-time.After(remoteSessionTTL):
			s.term.takeSession(sessID)
		}
	}()
	s.term.requestWake(devID, shared.WaitResponse{Type: "open_files", Session: sessID})
	s.writeJSON(w, http.StatusCreated, map[string]string{"session": sessID})
}

// handleViewerFileWS nimmt die WS des Viewers an und reicht sie an den Agent durch.
func (s *Server) handleViewerFileWS(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.viewerFileAuth(w, r); !ok {
		return
	}
	sessID := r.URL.Query().Get("session")
	s.term.mu.Lock()
	sess := s.term.session[sessID]
	s.term.mu.Unlock()
	if sess == nil || sess.deviceID != chi.URLParam(r, "id") {
		s.writeErr(w, http.StatusNotFound, "unbekannte session")
		return
	}
	tok := bearerToken(r)
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	user := s.fileCaps.userOf(tok)
	defer s.term.takeSession(sessID)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	c.SetReadLimit(8 << 20)
	defer c.CloseNow()
	ctx := r.Context()
	var agent *websocket.Conn
	select {
	case agent = <-sess.agentConn:
	case <-time.After(agentConnectWindow):
		c.Close(websocket.StatusTryAgainLater, "agent nicht erreichbar")
		return
	case <-ctx.Done():
		return
	}
	// Wie relay, aber Anfragen des Viewers werden mitgelesen und Dateiaktionen protokolliert.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go pump(ctx, cancel, agent, c)
	func() {
		defer cancel()
		for {
			typ, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				s.auditFileReq(r, user, data)
			}
			if err := agent.Write(ctx, typ, data); err != nil {
				return
			}
		}
	}()
	close(sess.done)
}

// fileReqActions: welche Operationen einer Datei-Sitzung ins Audit-Log gehören.
var fileReqActions = map[string]string{
	"put": "Datei hochgeladen (Viewer)", "get": "Datei heruntergeladen (Viewer)",
	"delete": "Gelöscht (Viewer)", "mkdir": "Ordner angelegt (Viewer)",
}

func (s *Server) auditFileReq(r *http.Request, user string, data []byte) {
	var req shared.FileReq
	if json.Unmarshal(data, &req) != nil {
		return
	}
	action, ok := fileReqActions[req.Op]
	if !ok {
		return
	}
	text := action + ": " + req.Path
	if req.Op == "put" && req.Size > 0 {
		text += " (" + humanBytes(req.Size) + ")"
		if req.Offset > 0 {
			text += ", fortgesetzt ab " + humanBytes(req.Offset)
		}
	}
	_ = s.store.InsertAudit(context.WithoutCancel(r.Context()), model.AuditEntry{
		TS: time.Now().UTC(), Username: user, Action: text,
		Method: "FILE", Path: "/devices/" + chi.URLParam(r, "id"), Status: http.StatusOK, IP: clientIP(r),
	})
}

// handleViewerBlob streamt die vom Agent hochgeladene Datei an den Viewer.
func (s *Server) handleViewerBlob(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.viewerFileAuth(w, r); !ok {
		return
	}
	b, ok := s.files.take(chi.URLParam(r, "cmd"))
	if !ok {
		s.writeErr(w, http.StatusNotFound, "Datei nicht (mehr) verfügbar")
		return
	}
	defer os.Remove(b.path)
	serveBlob(w, r, b, "")
}
