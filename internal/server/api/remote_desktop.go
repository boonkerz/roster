package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// Web-Fernsteuerung (Remote Desktop) über denselben On-demand-Tunnel wie das
// Terminal: der Browser rendert mit noVNC, am Endpunkt läuft on-demand ein nativer
// VNC-Server (127.0.0.1), unser Server reicht nur die RFB-Bytes durch.
//
//  1. Der Browser fordert per POST /remote/start eine Sitzung an. Der Server
//     erzeugt Session-Token + Einmalpasswort, weckt den Agent ("open_vnc") und
//     gibt {session, password} zurück.
//  2. Der Agent startet den VNC-Server mit dem Passwort und meldet sich mit einer
//     WebSocket für die Session (handleAgentTerminal – wiederverwendet).
//  3. Der Browser öffnet die noVNC-WS /remote/ws?session=…; der Server paart
//     Browser- und Agent-WS und relayed die Frames (relay – wiederverwendet).

const remoteSessionTTL = 120 * time.Second // so lange wartet eine Sitzung auf die Browser-/Viewer-WS

// vncPassword erzeugt ein 8-Zeichen-Einmalpasswort (RFB-Auth ist auf 8 Zeichen
// begrenzt) aus einem verwechslungsarmen Alphabet.
func vncPassword() string {
	const alpha = "abcdefghijkmnpqrstuvwxyz23456789"
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alpha[int(b[i])%len(alpha)]
	}
	return string(b)
}

type remoteStartResponse struct {
	Session  string `json:"session"`
	Password string `json:"password"`
	Token    string `json:"token"` // für den nativen Viewer (pcinv-viewer)
}

// handleRemoteStart erzeugt eine Remote-Desktop-Sitzung und weckt den Agent.
func (s *Server) handleRemoteStart(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	sessID := newSessionID()
	pass := vncPassword()
	viewerToken := newSessionID() // 128-bit Capability für die native Viewer-WS

	// Monitor-Auswahl (Default 1 = primär; 0 = alle/virtueller Desktop).
	monitor := 1
	var body struct {
		Monitor *int `json:"monitor"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Monitor != nil && *body.Monitor >= 0 {
		monitor = *body.Monitor
	}

	sess := &termSession{
		deviceID:  deviceID,
		agentConn: make(chan *websocket.Conn, 1),
		done:      make(chan struct{}),
		token:     viewerToken,
	}
	s.term.addSession(sessID, sess)

	// Sitzung aufräumen, falls die Browser-WS nie kommt (kein Leak).
	go func() {
		select {
		case <-sess.done:
		case <-time.After(remoteSessionTTL):
			s.term.takeSession(sessID)
		}
	}()

	consent := s.resolveRemoteConsent(r.Context(), deviceID)
	s.term.requestWake(deviceID, shared.WaitResponse{
		Type: "open_vnc", Session: sessID, Password: pass, Consent: consent, Monitor: monitor,
	})

	var uname string
	if u := userFrom(r.Context()); u != nil {
		uname = u.Username
	}
	_ = s.store.InsertAudit(r.Context(), model.AuditEntry{
		TS: time.Now().UTC(), Username: uname, Action: "Fernsteuerung gestartet",
		Method: r.Method, Path: "/devices/" + deviceID, Status: http.StatusOK,
	})
	s.writeJSON(w, http.StatusOK, remoteStartResponse{Session: sessID, Password: pass, Token: viewerToken})
}

// resolveRemoteConsent ermittelt, ob der Nutzer am Gerät die Fernsteuerung
// bestätigen muss (zielgruppenabhängig, Vererbung device→site→client).
func (s *Server) resolveRemoteConsent(ctx context.Context, deviceID string) bool {
	mode, _ := s.store.ResolveRemoteConsent(ctx, deviceID)
	return mode == "prompt"
}

// handleGetDeviceConsent liefert den effektiven und den explizit gesetzten
// Zustimmungs-Modus eines Geräts.
func (s *Server) handleGetDeviceConsent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	eff, _ := s.store.ResolveRemoteConsent(r.Context(), id)
	own, _ := s.store.GetRemoteConsent(r.Context(), "device", id)
	s.writeJSON(w, http.StatusOK, map[string]string{"effective": eff, "device": own})
}

// handleSetRemoteConsent setzt den Modus für ein Ziel (device|site|client).
// mode "" = erben. requireAdmin.
func (s *Server) handleSetRemoteConsent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		Mode       string `json:"mode"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	switch req.TargetType {
	case "device", "site", "client":
	default:
		s.writeErr(w, http.StatusBadRequest, "target_type muss device|site|client sein")
		return
	}
	if req.Mode != "" && req.Mode != "unattended" && req.Mode != "prompt" {
		s.writeErr(w, http.StatusBadRequest, "mode muss unattended|prompt (oder leer) sein")
		return
	}
	if err := s.store.SetRemoteConsent(r.Context(), req.TargetType, req.TargetID, req.Mode); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// handleDeviceVNC nimmt die noVNC-WS des Browsers entgegen und relayed sie an die
// (bereits per /remote/start angeforderte) Agent-Session. Auth: Cookie (requireAdmin).
func (s *Server) handleDeviceVNC(w http.ResponseWriter, r *http.Request) {
	sessID := r.URL.Query().Get("session")
	s.term.mu.Lock()
	sess := s.term.session[sessID]
	s.term.mu.Unlock()
	if sess == nil || sess.deviceID != chi.URLParam(r, "id") {
		s.writeErr(w, http.StatusNotFound, "unbekannte session")
		return
	}
	s.relayRemote(w, r, sessID, sess)
}

// handleViewerVNC bedient den nativen Viewer (pcinv-viewer). Es gibt kein
// Session-Cookie; die Berechtigung wird über das pro-Sitzung erzeugte Viewer-Token
// nachgewiesen (Bearer-Header oder ?token=), das nur ein Admin via /remote/start
// erhalten kann.
func (s *Server) handleViewerVNC(w http.ResponseWriter, r *http.Request) {
	sessID := r.URL.Query().Get("session")
	tok := bearerToken(r)
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	s.term.mu.Lock()
	sess := s.term.session[sessID]
	s.term.mu.Unlock()
	if sess == nil || sess.deviceID != chi.URLParam(r, "id") {
		s.writeErr(w, http.StatusNotFound, "unbekannte session")
		return
	}
	if sess.token == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(sess.token)) != 1 {
		s.writeErr(w, http.StatusUnauthorized, "viewer-token ungültig")
		return
	}
	s.relayRemote(w, r, sessID, sess)
}

// relayRemote nimmt die Client-WS (Browser oder nativer Viewer) an und relayed die
// RFB-Bytes an die bereits gepaarte Agent-Session.
func (s *Server) relayRemote(w http.ResponseWriter, r *http.Request, sessID string, sess *termSession) {
	defer s.term.takeSession(sessID)

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  []string{"*"},
		CompressionMode: websocket.CompressionContextTakeover, // permessage-deflate: komprimiert die RFB-Pixel
	})
	if err != nil {
		return
	}
	c.SetReadLimit(4 << 20) // RFB-Frames können größer sein als Terminal-I/O
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

	relay(ctx, c, agent)
	close(sess.done)
}
