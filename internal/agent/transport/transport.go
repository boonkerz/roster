// Package transport kapselt die HTTPS-Kommunikation des Agents mit dem Server.
package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/boonkerz/roster/internal/shared"
)

// Client spricht die Agent-API des Servers an.
type Client struct {
	baseURL string
	http    *http.Client
	stream  *http.Client // ohne Timeout, für Long-Poll (Wake) und WebSockets
}

// New erstellt einen Client. caCertPath pinnt optional die Server-CA;
// insecure schaltet die Zertifikatsprüfung ab (nur für lokale Tests).
func New(baseURL, caCertPath string, insecure bool) (*Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure} //nolint:gosec
	if caCertPath != "" {
		pem, err := os.ReadFile(caCertPath)
		if err != nil {
			return nil, fmt.Errorf("ca-cert lesen: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("ca-cert konnte nicht geladen werden")
		}
		tlsCfg.RootCAs = pool
	}
	tr := &http.Transport{TLSClientConfig: tlsCfg}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second, Transport: tr},
		stream:  &http.Client{Transport: tr}, // kein Timeout: Wake-Poll & WS dürfen lange offen sein
	}, nil
}

// Enroll tauscht das Enrollment-Token gegen ein dauerhaftes Agent-Token.
func (c *Client) Enroll(ctx context.Context, req shared.EnrollRequest) (shared.EnrollResponse, error) {
	var resp shared.EnrollResponse
	err := c.do(ctx, "/api/v1/agent/enroll", "", req, &resp)
	return resp, err
}

// Checkin sendet das Inventar und empfängt ausstehende Befehle.
func (c *Client) Checkin(ctx context.Context, agentToken string, req shared.CheckinRequest) (shared.CheckinResponse, error) {
	var resp shared.CheckinResponse
	err := c.do(ctx, "/api/v1/agent/checkin", agentToken, req, &resp)
	return resp, err
}

// ReportProgress meldet einen Zwischenstand eines lang laufenden Befehls
// (z.B. Verzeichnis-Scan) – damit der Server/Client den Fortschritt live sieht.
func (c *Client) ReportProgress(ctx context.Context, agentToken, commandID, output string) error {
	return c.do(ctx, "/api/v1/agent/command-progress", agentToken,
		shared.CommandProgress{CommandID: commandID, Output: output}, nil)
}

// UploadFile lädt eine Datei (aus einem read_file-Befehl) gestreamt zum Server hoch, wo
// der Browser sie abholt. Ohne Gesamt-Zeitlimit (große Dateien), abbrechbar über ctx.
func (c *Client) UploadFile(ctx context.Context, agentToken, xferID, name string, body io.Reader, size int64) error {
	u := c.baseURL + "/api/v1/agent/file-upload?cmd=" + url.QueryEscape(xferID) + "&name=" + url.QueryEscape(name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Authorization", "Bearer "+agentToken)
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.stream.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return fmt.Errorf("upload: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// DownloadPayload öffnet die vom Browser hochgeladenen Bytes eines write_file-Befehls als
// Datenstrom (Größe -1, wenn unbekannt). Der Aufrufer schließt den Body.
func (c *Client) DownloadPayload(ctx context.Context, agentToken, xferID string) (io.ReadCloser, int64, error) {
	u := c.baseURL + "/api/v1/agent/file-download?cmd=" + url.QueryEscape(xferID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+agentToken)
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("download: status %d", resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}

// DownloadAgent lädt das Agent-Binary für eine Plattform ("<os>-<arch>").
// Der Aufrufer muss den zurückgegebenen Reader schließen.
func (c *Client) DownloadAgent(ctx context.Context, platform string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/agents/"+platform, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download fehlgeschlagen: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// Wait parkt den Wake-Long-Poll und liefert den nächsten Realtime-Auftrag des Servers.
// Kehrt nach dem serverseitigen Timeout mit Type=="" (idle) zurück.
func (c *Client) Wait(ctx context.Context, agentToken string) (shared.WaitResponse, error) {
	var wr shared.WaitResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/agent/wait", nil)
	if err != nil {
		return wr, err
	}
	req.Header.Set("Authorization", "Bearer "+agentToken)
	resp, err := c.stream.Do(req)
	if err != nil {
		return wr, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return wr, fmt.Errorf("wait: status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return wr, json.Unmarshal(data, &wr)
}

// DialTerminal öffnet die WebSocket für eine Terminal-Session beim Server.
func (c *Client) DialTerminal(ctx context.Context, agentToken, session string) (*websocket.Conn, error) {
	u := wsURL(c.baseURL) + "/api/v1/agent/terminal?session=" + url.QueryEscape(session)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+agentToken)
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPClient:      c.stream,
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionContextTakeover, // komprimiert u.a. die RFB-Pixel
	})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(4 << 20)
	return conn, nil
}

// DialFiles verbindet eine Datei-Sitzung (gleiche Paarung wie das Terminal, aber ohne
// Kompression – Dateien sind oft schon komprimiert, deflate kostet nur CPU).
func (c *Client) DialFiles(ctx context.Context, agentToken, session string) (*websocket.Conn, error) {
	u := wsURL(c.baseURL) + "/api/v1/agent/terminal?session=" + url.QueryEscape(session)
	header := http.Header{}
	header.Set("Authorization", "Bearer "+agentToken)
	conn, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPClient:      c.stream,
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(4 << 20)
	return conn, nil
}

// wsURL wandelt eine http(s)-Basis-URL in das passende ws(s)-Schema um.
func wsURL(base string) string {
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://")
	default:
		return base
	}
}

func (c *Client) do(ctx context.Context, path, bearer string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server antwortete %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}
