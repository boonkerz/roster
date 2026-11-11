package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// Direkter Datei-Zugriff für die Web-UI: der Server spricht selbst mit dem Agent über eine
// Datei-Sitzung (wie der native Viewer) und reicht HTTP-Uploads/-Downloads durch – ohne
// Zwischendatei und ohne einen Befehl pro Datei. Ab Agent 0.29.0; ältere Agents bekommen
// 409 „legacy“, die Web-UI nimmt dann den bisherigen Weg.

const directFilesVersion = "0.29.0"

var errLegacyAgent = errors.New("legacy")

// agentFileConn ist eine vom Server gehaltene Datei-Sitzung zu einem Agent.
type agentFileConn struct {
	deviceID string
	c        *websocket.Conn
	sess     *termSession
	last     time.Time
}

// filePool hält je Gerät freie Sitzungen (eine Operation pro Sitzung zur Zeit).
type filePool struct {
	mu   sync.Mutex
	idle map[string][]*agentFileConn
}

func newFilePool() *filePool {
	p := &filePool{idle: map[string][]*agentFileConn{}}
	go p.gc()
	return p
}

func (p *filePool) gc() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for range t.C {
		p.mu.Lock()
		for dev, list := range p.idle {
			keep := list[:0]
			for _, fc := range list {
				if time.Since(fc.last) > 2*time.Minute {
					fc.close()
				} else {
					keep = append(keep, fc)
				}
			}
			p.idle[dev] = keep
		}
		p.mu.Unlock()
	}
}

func (fc *agentFileConn) close() {
	fc.c.CloseNow()
	select {
	case <-fc.sess.done:
	default:
		close(fc.sess.done) // gibt den Agent-Handler frei
	}
}

// acquireFileConn liefert eine freie oder neue Sitzung zum Agent des Geräts.
func (s *Server) acquireFileConn(ctx context.Context, d *model.Device) (*agentFileConn, error) {
	if !agentSupportsVersion(d.AgentVersion, directFilesVersion) || strings.TrimSpace(d.AgentVersion) == "" {
		return nil, errLegacyAgent
	}
	s.filePool.mu.Lock()
	if list := s.filePool.idle[d.ID]; len(list) > 0 {
		fc := list[len(list)-1]
		s.filePool.idle[d.ID] = list[:len(list)-1]
		s.filePool.mu.Unlock()
		return fc, nil
	}
	s.filePool.mu.Unlock()

	sessID := newSessionID()
	sess := &termSession{deviceID: d.ID, agentConn: make(chan *websocket.Conn, 1), done: make(chan struct{})}
	s.term.addSession(sessID, sess)
	defer s.term.takeSession(sessID)
	s.term.requestWake(d.ID, shared.WaitResponse{Type: "open_files", Session: sessID})
	var c *websocket.Conn
	select {
	case c = <-sess.agentConn:
	case <-time.After(agentConnectWindow):
		close(sess.done)
		return nil, errors.New("Agent nicht erreichbar (offline?)")
	case <-ctx.Done():
		close(sess.done)
		return nil, ctx.Err()
	}
	fc := &agentFileConn{deviceID: d.ID, c: c, sess: sess}
	if _, err := fc.call(ctx, shared.FileReq{Op: "hello"}); err != nil {
		fc.close()
		return nil, err
	}
	return fc, nil
}

// releaseFileConn gibt eine Sitzung zurück (ok) oder schließt sie (Datenstrom kaputt).
func (s *Server) releaseFileConn(fc *agentFileConn, ok bool) {
	if !ok {
		fc.close()
		return
	}
	fc.last = time.Now()
	s.filePool.mu.Lock()
	s.filePool.idle[fc.deviceID] = append(s.filePool.idle[fc.deviceID], fc)
	s.filePool.mu.Unlock()
}

func (fc *agentFileConn) send(ctx context.Context, req shared.FileReq) error {
	b, _ := json.Marshal(req)
	return fc.c.Write(ctx, websocket.MessageText, b)
}

func (fc *agentFileConn) recv(ctx context.Context) (shared.FileResp, error) {
	for {
		typ, data, err := fc.c.Read(ctx)
		if err != nil {
			return shared.FileResp{}, err
		}
		if typ != websocket.MessageText {
			return shared.FileResp{}, errors.New("unerwartete Binärdaten")
		}
		var r shared.FileResp
		return r, json.Unmarshal(data, &r)
	}
}

// fileOpErr ist ein fachlicher Fehler des Agents (Verbindung intakt).
type fileOpErr struct{ msg string }

func (e *fileOpErr) Error() string { return e.msg }

func (fc *agentFileConn) call(ctx context.Context, req shared.FileReq) (shared.FileResp, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := fc.send(ctx, req); err != nil {
		return shared.FileResp{}, err
	}
	r, err := fc.recv(ctx)
	if err == nil && r.Error != "" {
		err = &fileOpErr{r.Error}
	}
	return r, err
}

// withFileConn führt fn mit einer Sitzung aus und gibt sie danach passend zurück.
func (s *Server) withFileConn(w http.ResponseWriter, r *http.Request, fn func(*agentFileConn) error) {
	d, err := s.store.GetDevice(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	fc, err := s.acquireFileConn(r.Context(), d)
	if errors.Is(err, errLegacyAgent) {
		s.writeErr(w, http.StatusConflict, "legacy")
		return
	}
	if err != nil {
		s.writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	err = fn(fc)
	var opErr *fileOpErr
	s.releaseFileConn(fc, err == nil || errors.As(err, &opErr))
	if err != nil && errors.As(err, &opErr) {
		s.writeErr(w, http.StatusUnprocessableEntity, opErr.msg)
	} else if err != nil {
		s.log.Info("direkte datei-übertragung abgebrochen", "gerät", fc.deviceID, "err", err)
	}
}

// handleFSList: GET /devices/{id}/fs/list?path= – Verzeichnisliste (browse_dir-Format).
func (s *Server) handleFSList(w http.ResponseWriter, r *http.Request) {
	s.withFileConn(w, r, func(fc *agentFileConn) error {
		resp, err := fc.call(r.Context(), shared.FileReq{Op: "list", Path: r.URL.Query().Get("path")})
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp.Listing)
		return nil
	})
}

// handleFSStat: GET /devices/{id}/fs/stat?path= – vor dem Download prüfen (die Web-UI
// startet den Download per Navigation und könnte Fehler sonst nicht anzeigen).
func (s *Server) handleFSStat(w http.ResponseWriter, r *http.Request) {
	s.withFileConn(w, r, func(fc *agentFileConn) error {
		resp, err := fc.call(r.Context(), shared.FileReq{Op: "stat", Path: r.URL.Query().Get("path")})
		if err != nil {
			return err
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"exists": resp.Exists, "dir": resp.Dir, "size": resp.Size})
		return nil
	})
}

// handleFSDownload: GET /devices/{id}/fs/download?path= – streamt die Datei vom Agent.
func (s *Server) handleFSDownload(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	s.withFileConn(w, r, func(fc *agentFileConn) error {
		ctx := r.Context()
		if err := fc.send(ctx, shared.FileReq{Op: "get", Path: p}); err != nil {
			return err
		}
		resp, err := fc.recv(ctx)
		if err != nil {
			return err
		}
		if resp.Error != "" {
			return &fileOpErr{resp.Error}
		}
		s.auditFile(r, "Datei heruntergeladen", p, resp.Size)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.FormatInt(resp.Size, 10))
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": baseName(p)}))
		var got int64
		for got < resp.Size {
			typ, data, err := fc.c.Read(ctx)
			if err != nil {
				return err
			}
			if typ != websocket.MessageBinary {
				return errors.New("download vom agent abgebrochen")
			}
			if _, err := w.Write(data); err != nil {
				return err // Browser hat abgebrochen – Sitzung verwerfen
			}
			got += int64(len(data))
		}
		end, err := fc.recv(ctx)
		if err == nil && end.Error != "" {
			err = errors.New(end.Error)
		}
		return err
	})
}

// handleFSUpload: POST /devices/{id}/fs/upload?path= – Body direkt zum Agent (put).
func (s *Server) handleFSUpload(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	size := r.ContentLength
	if p == "" || size < 0 {
		s.writeErr(w, http.StatusBadRequest, "path oder Content-Length fehlt")
		return
	}
	if size > maxFileTransfer {
		s.writeErr(w, http.StatusRequestEntityTooLarge, errTransferTooLarge.Error())
		return
	}
	s.withFileConn(w, r, func(fc *agentFileConn) error {
		ctx := r.Context()
		resp, err := fc.call(ctx, shared.FileReq{Op: "put", Path: p, Size: size})
		if err != nil {
			return err
		}
		if resp.Offset != 0 {
			return errors.New("unerwarteter offset")
		}
		buf := make([]byte, shared.FileChunk)
		var sent int64
		for sent < size {
			n, rerr := io.ReadFull(r.Body, buf[:min(int64(len(buf)), size-sent)])
			if n > 0 {
				if err := fc.c.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
					return err
				}
				sent += int64(n)
			}
			if rerr != nil && sent < size {
				return fmt.Errorf("upload abgebrochen: %v", rerr) // Agent behält die Teildatei
			}
		}
		end, err := fc.recv(ctx)
		if err != nil {
			return err
		}
		if end.Error != "" {
			return &fileOpErr{end.Error}
		}
		s.auditFile(r, "Datei hochgeladen", p, size)
		s.writeJSON(w, http.StatusOK, map[string]any{"done": true, "size": size})
		return nil
	})
}

// auditFile protokolliert eine Dateiaktion mit Pfad und Größe.
func (s *Server) auditFile(r *http.Request, action, p string, size int64) {
	var uid, uname string
	if u := userFrom(r.Context()); u != nil {
		uid, uname = u.ID, u.Username
	}
	if action == "" {
		return
	}
	text := action + ": " + p
	if size > 0 {
		text += fmt.Sprintf(" (%s)", humanBytes(size))
	}
	_ = s.store.InsertAudit(context.WithoutCancel(r.Context()), model.AuditEntry{
		TS: time.Now().UTC(), UserID: uid, Username: uname, Action: text,
		Method: "FILE", Path: "/devices/" + chi.URLParam(r, "id"), Status: http.StatusOK, IP: clientIP(r),
	})
}

func baseName(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if b := path.Base(p); b != "." && b != "/" {
		return b
	}
	return "download"
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
