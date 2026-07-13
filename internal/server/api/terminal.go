package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// Das Remote-Terminal arbeitet on-demand, ohne Dauerverbindung:
//
//  1. Der Agent parkt einen leichten Long-Poll (handleAgentWait). Im Leerlauf
//     wartet er, bis ein Terminal angefordert wird, oder kehrt nach einem Timeout
//     zurück und pollt erneut.
//  2. Öffnet ein Admin im UI ein Terminal (handleDeviceTerminal), erzeugt der
//     Server eine kurzlebige Session und weckt den Agent über den Wake-Poll.
//  3. Der Agent „meldet sich" mit einer WebSocket für die Session
//     (handleAgentTerminal); der Server paart Browser- und Agent-WS und relayed
//     die Frames bidirektional.

const (
	wakePollTimeout    = 45 * time.Second // so lange parkt ein Wake-Poll maximal
	agentConnectWindow = 15 * time.Second // so lange wartet der Browser auf den Agent
)

// termHub hält den flüchtigen Zustand für Wake-Polls und Terminal-Sessions.
// Alles in-memory (Single-Binary-Server).
type termHub struct {
	mu      sync.Mutex
	pending map[string]shared.WaitResponse // deviceID -> nächster Wake-Auftrag
	signal  map[string]chan struct{}       // deviceID -> weckt den geparkten Poll
	session map[string]*termSession        // sessionID -> Session
}

// termSession ist eine angeforderte Terminal-Sitzung, auf deren Agent-WS gewartet wird.
type termSession struct {
	deviceID  string
	agentConn chan *websocket.Conn // Rendezvous: Agent reicht hier seine WS hinein
	done      chan struct{}        // geschlossen, wenn das Relay endet
	token     string               // optionales Viewer-Token (native Fernsteuerung ohne Cookie)
}

func newTermHub() *termHub {
	return &termHub{
		pending: map[string]shared.WaitResponse{},
		signal:  map[string]chan struct{}{},
		session: map[string]*termSession{},
	}
}

// wait parkt einen Wake-Poll für ein Gerät und liefert den nächsten Auftrag oder
// (nach Timeout / Kontextende) einen Idle-Auftrag.
func (h *termHub) wait(ctx context.Context, deviceID string) shared.WaitResponse {
	h.mu.Lock()
	if wr, ok := h.pending[deviceID]; ok {
		delete(h.pending, deviceID)
		h.mu.Unlock()
		return wr
	}
	sig := make(chan struct{}, 1)
	h.signal[deviceID] = sig
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		if h.signal[deviceID] == sig {
			delete(h.signal, deviceID)
		}
		h.mu.Unlock()
	}()

	select {
	case <-sig:
		h.mu.Lock()
		wr := h.pending[deviceID]
		delete(h.pending, deviceID)
		h.mu.Unlock()
		return wr
	case <-time.After(wakePollTimeout):
		return shared.WaitResponse{Type: ""}
	case <-ctx.Done():
		return shared.WaitResponse{Type: ""}
	}
}

// queueCommand reiht einen Befehl ein und stupst den Agent über den Wake-Kanal an,
// damit er sofort eincheckt und den Befehl abholt – statt bis zum nächsten
// Checkin-Intervall zu warten. Ohne aktiven Wake-Poll (disable_remote) greift
// weiterhin das reguläre Intervall.
func (s *Server) queueCommand(ctx context.Context, deviceID, typ, label string, payload map[string]any) (string, error) {
	id, err := s.store.CreateCommand(ctx, deviceID, typ, label, payload)
	if err != nil {
		return "", err
	}
	s.term.requestWake(deviceID, shared.WaitResponse{Type: "checkin"})
	return id, nil
}

// requestWake hinterlegt einen Auftrag für ein Gerät und weckt einen ggf. geparkten Poll.
func (h *termHub) requestWake(deviceID string, wr shared.WaitResponse) {
	h.mu.Lock()
	h.pending[deviceID] = wr
	if sig := h.signal[deviceID]; sig != nil {
		select {
		case sig <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *termHub) addSession(id string, s *termSession) {
	h.mu.Lock()
	h.session[id] = s
	h.mu.Unlock()
}

func (h *termHub) takeSession(id string) *termSession {
	h.mu.Lock()
	s := h.session[id]
	delete(h.session, id)
	h.mu.Unlock()
	return s
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// handleAgentWait ist der Wake-Long-Poll des Agents (Bearer-Auth via requireAgent).
func (s *Server) handleAgentWait(w http.ResponseWriter, r *http.Request) {
	dev := deviceFrom(r.Context())
	if dev == nil {
		s.writeErr(w, http.StatusUnauthorized, "kein gerät")
		return
	}
	wr := s.term.wait(r.Context(), dev.ID)
	s.writeJSON(w, http.StatusOK, wr)
}

// handleDeviceTerminal nimmt die Browser-WS eines Admins entgegen, weckt den Agent
// und relayed die Terminal-Session.
func (s *Server) handleDeviceTerminal(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	shell := r.URL.Query().Get("shell")
	runas := r.URL.Query().Get("runas")
	if runas != "user" {
		runas = "system"
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return // Accept hat bereits geantwortet
	}
	c.SetReadLimit(1 << 20)
	defer c.CloseNow()

	ctx := r.Context()
	sessID := newSessionID()
	sess := &termSession{
		deviceID:  deviceID,
		agentConn: make(chan *websocket.Conn, 1),
		done:      make(chan struct{}),
	}
	s.term.addSession(sessID, sess)
	defer s.term.takeSession(sessID)

	s.term.requestWake(deviceID, shared.WaitResponse{
		Type: "open_terminal", Session: sessID, Shell: shell, RunAs: runas,
	})

	var agent *websocket.Conn
	select {
	case agent = <-sess.agentConn:
	case <-time.After(agentConnectWindow):
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"exit","code":-1}`))
		c.Close(websocket.StatusTryAgainLater, "agent nicht erreichbar")
		return
	case <-ctx.Done():
		return
	}

	relay(ctx, c, agent)
	close(sess.done)
}

// handleAgentTerminal nimmt die Agent-WS für eine Session entgegen (requireAgent).
func (s *Server) handleAgentTerminal(w http.ResponseWriter, r *http.Request) {
	dev := deviceFrom(r.Context())
	if dev == nil {
		s.writeErr(w, http.StatusUnauthorized, "kein gerät")
		return
	}
	sessID := r.URL.Query().Get("session")
	s.term.mu.Lock()
	sess := s.term.session[sessID]
	s.term.mu.Unlock()
	if sess == nil || sess.deviceID != dev.ID {
		s.writeErr(w, http.StatusNotFound, "unbekannte session")
		return
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:  []string{"*"},
		CompressionMode: websocket.CompressionContextTakeover, // komprimiert u.a. die RFB-Pixel
	})
	if err != nil {
		return
	}
	c.SetReadLimit(4 << 20)
	defer c.CloseNow()

	select {
	case sess.agentConn <- c:
	default:
		c.Close(websocket.StatusPolicyViolation, "session bereits belegt")
		return
	}

	// Geöffnet halten, bis das Relay (im Browser-Handler) endet.
	select {
	case <-sess.done:
	case <-r.Context().Done():
	}
}

// relay kopiert Frames bidirektional zwischen zwei WebSockets und erhält dabei den
// Frame-Typ (Binär = rohe Terminal-I/O, Text = Steuerung wie resize/exit).
func relay(ctx context.Context, a, b *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go pump(ctx, cancel, a, b)
	pump(ctx, cancel, b, a)
}

func pump(ctx context.Context, cancel context.CancelFunc, src, dst *websocket.Conn) {
	defer cancel()
	for {
		typ, data, err := src.Read(ctx)
		if err != nil {
			return
		}
		if err := dst.Write(ctx, typ, data); err != nil {
			return
		}
	}
}
