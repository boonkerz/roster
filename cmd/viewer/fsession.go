package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/boonkerz/roster/internal/shared"
)

// fsClient spricht mit dem Agent über die Datei-Sitzung (direkter Kanal durch den
// Tunnel, siehe shared.FileReq). Eine Operation zur Zeit (mu).
type fsClient struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

var errNoFileSession = errors.New("Agent unterstützt den schnellen Datei-Kanal nicht (zu alt)")

// dialFileSession eröffnet die Sitzung (POST …/session) und verbindet die WS. Antwortet
// der Agent nicht (alte Version kennt „open_files“ nicht), kommt errNoFileSession.
func dialFileSession(cfg *launchConfig, cl *fmClient) (*fsClient, error) {
	req, _ := http.NewRequest(http.MethodPost, cl.url("/session"), nil)
	req.Header.Set("Authorization", "Bearer "+cl.token)
	resp, err := cl.http.Do(req)
	if err != nil {
		return nil, err
	}
	var r struct {
		Session string `json:"session"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, errNoFileSession // Server zu alt
	}
	if resp.StatusCode >= 300 || r.Session == "" {
		return nil, fmt.Errorf("datei-sitzung: %s", resp.Status)
	}
	hc := &http.Client{}
	if cfg.Insecure {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsBase(cfg.URL)+"/api/v1/devices/"+cfg.Device+"/viewer-files/ws?session="+url.QueryEscape(r.Session),
		&websocket.DialOptions{
			HTTPClient:      hc,
			HTTPHeader:      http.Header{"Authorization": {"Bearer " + cl.token}},
			CompressionMode: websocket.CompressionDisabled,
		})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(8 << 20)
	c := &fsClient{conn: conn}
	// Der Server wartet bis 15 s auf den Agent; kommt keiner, schließt er die WS.
	if _, err := c.call(shared.FileReq{Op: "hello"}, 20*time.Second); err != nil {
		conn.CloseNow()
		return nil, errNoFileSession
	}
	return c, nil
}

func (c *fsClient) close() {
	if c != nil && c.conn != nil {
		c.conn.CloseNow()
	}
}

func (c *fsClient) send(ctx context.Context, req shared.FileReq) error {
	b, _ := json.Marshal(req)
	return c.conn.Write(ctx, websocket.MessageText, b)
}

func (c *fsClient) recv(ctx context.Context) (shared.FileResp, error) {
	for {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return shared.FileResp{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var r shared.FileResp
		if err := json.Unmarshal(data, &r); err != nil {
			return r, err
		}
		return r, nil
	}
}

// call führt eine einfache Operation aus. Netzfehler kommen als error, fachliche
// Fehler des Agents als *remoteErr.
func (c *fsClient) call(req shared.FileReq, timeout time.Duration) (shared.FileResp, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := c.send(ctx, req); err != nil {
		return shared.FileResp{}, err
	}
	r, err := c.recv(ctx)
	if err != nil {
		return r, err
	}
	if r.Error != "" {
		return r, &remoteErr{r.Error}
	}
	return r, nil
}

type remoteErr struct{ msg string }

func (e *remoteErr) Error() string { return e.msg }

// isRemoteErr: fachlicher Fehler (Verbindung intakt) statt Netzfehler.
func isRemoteErr(err error) bool {
	var re *remoteErr
	return errors.As(err, &re)
}

// get lädt path ab offset nach w; on meldet die bisher übertragenen Bytes dieser Datei.
func (c *fsClient) get(ctx context.Context, path string, offset int64, w io.Writer, on func(int64)) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.send(ctx, shared.FileReq{Op: "get", Path: path, Offset: offset}); err != nil {
		return 0, err
	}
	r, err := c.recv(ctx)
	if err != nil {
		return 0, err
	}
	if r.Error != "" {
		return 0, &remoteErr{r.Error}
	}
	size := r.Size
	got := offset
	for got < size {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return size, err
		}
		if typ == websocket.MessageText {
			var e shared.FileResp
			_ = json.Unmarshal(data, &e)
			return size, &remoteErr{e.Error}
		}
		if _, err := w.Write(data); err != nil {
			c.conn.CloseNow() // Rest des Datenstroms ist nicht mehr abholbar
			return size, err
		}
		got += int64(len(data))
		if on != nil {
			on(got)
		}
	}
	end, err := c.recv(ctx)
	if err != nil {
		return size, err
	}
	if end.Error != "" {
		return size, &remoteErr{end.Error}
	}
	return size, nil
}

// put lädt size Bytes aus r nach path. Hat der Agent schon eine Teildatei der Länge
// resume, setzt er dort fort – r wird dann ab dieser Stelle gelesen (seek).
func (c *fsClient) put(ctx context.Context, path string, r io.ReadSeeker, size, resume int64, on func(int64)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.send(ctx, shared.FileReq{Op: "put", Path: path, Size: size, Offset: resume}); err != nil {
		return err
	}
	resp, err := c.recv(ctx)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return &remoteErr{resp.Error}
	}
	off := resp.Offset
	if _, err := r.Seek(off, io.SeekStart); err != nil {
		c.conn.CloseNow()
		return err
	}
	buf := make([]byte, shared.FileChunk)
	sent := off
	for sent < size {
		n, rerr := io.ReadFull(r, buf[:min(int64(len(buf)), size-sent)])
		if n > 0 {
			if err := c.conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
				return err
			}
			sent += int64(n)
			if on != nil {
				on(sent)
			}
		}
		if rerr != nil && sent < size {
			c.conn.CloseNow() // Agent wartet auf Bytes, die nicht mehr kommen
			return fmt.Errorf("lokale Datei: %v", rerr)
		}
	}
	end, err := c.recv(ctx)
	if err != nil {
		return err
	}
	if end.Error != "" {
		return &remoteErr{end.Error}
	}
	return nil
}
