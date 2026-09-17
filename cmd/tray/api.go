package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// client spricht die Roster-API mit einem User-API-Token (Authorization: Bearer …).
// Dasselbe Token trägt auch die Terminal-WebSocket und den Start der Fernsteuerung.
type client struct {
	base  string // https://host[:port] ohne abschließenden /
	token string
	http  *http.Client
}

func newClient(base, token string, insecure bool) *client {
	hc := &http.Client{Timeout: 20 * time.Second}
	if insecure {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	return &client{base: strings.TrimRight(base, "/"), token: token, http: hc}
}

// device ist der Ausschnitt aus /devices, den die Taskleisten-App anzeigt.
type device struct {
	ID         string     `json:"id"`
	Hostname   string     `json:"hostname"`
	OS         string     `json:"os"`
	Status     string     `json:"status"` // online | offline | unmanaged
	Managed    bool       `json:"managed"`
	Revoked    bool       `json:"revoked"`
	LastSeen   *time.Time `json:"last_seen"`
	Interfaces []iface    `json:"interfaces"`

	Temperatures []shared.Temperature `json:"temperatures"`
	Fans         []shared.Fan         `json:"fans"`

	ProxmoxVersion string     `json:"proxmox_version"` // gesetzt = Agent läuft auf einem PVE-Host
	ProxmoxGuests  []pveGuest `json:"proxmox_guests"`
	PublicIP       string     `json:"public_ip"`
	SiteName       string     `json:"site_name"`
	ClientName     string     `json:"client_name"`
	ChecksTotal    int        `json:"checks_total"`
	ChecksFailing  int        `json:"checks_failing"`
	TasksTotal     int        `json:"tasks_total"`
	TasksFailing   int        `json:"tasks_failing"`
	VulnCount      int        `json:"vuln_count"`
}

// pveGuest ist eine VM/ein Container auf einem Proxmox-Host samt Backup-Stand.
type pveGuest struct {
	Node             string     `json:"node"`
	VMID             int        `json:"vmid"`
	Type             string     `json:"type"` // qemu | lxc
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	Template         bool       `json:"template"`
	CPUs             float64    `json:"cpus"`
	MaxMem           int64      `json:"maxmem"`
	BackupAt         *time.Time `json:"backup_at"`
	BackupTaskStatus string     `json:"backup_task_status"`
	BackupTaskMsg    string     `json:"backup_task_msg"`
	BackupJob        *bool      `json:"backup_job"`
}

// iface ist eine Netzwerkschnittstelle des Geräts – Quelle der Adresse für SFTP.
type iface struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

type meResponse struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

type remoteStart struct {
	Session  string `json:"session"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

// apiError trägt den HTTP-Status, damit die UI 401 (Token weg) von Netzfehlern trennt.
type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return e.Msg }

func isUnauthorized(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized)
}

func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/api/v1"+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return &apiError{Status: resp.StatusCode, Msg: apiErrMessage(resp)}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// apiErrMessage zieht das "error"-Feld aus der Antwort; sonst der Status-Text.
func apiErrMessage(resp *http.Response) string {
	var e struct {
		Error string `json:"error"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return e.Error
	}
	return strings.ToLower(http.StatusText(resp.StatusCode))
}

func (c *client) devices(ctx context.Context) ([]device, error) {
	var out []device
	err := c.do(ctx, http.MethodGet, "/devices", nil, &out)
	return out, err
}

func (c *client) me(ctx context.Context) (*meResponse, error) {
	var out meResponse
	err := c.do(ctx, http.MethodGet, "/auth/me", nil, &out)
	return &out, err
}

// startRemote fordert eine Fernsteuerungs-Sitzung an (weckt den Agent) und liefert
// die Daten für den nativen Viewer.
func (c *client) startRemote(ctx context.Context, deviceID string) (*remoteStart, error) {
	var out remoteStart
	err := c.do(ctx, http.MethodPost, "/devices/"+url.PathEscape(deviceID)+"/remote/start",
		map[string]any{"monitor": 1}, &out)
	return &out, err
}

// commandState ist der Stand eines Agent-Befehls (GET /commands/{id}).
type commandState struct {
	Status   string `json:"status"` // pending | sent | done
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

// proxmoxControl reiht Start/Stop/Herunterfahren/Neustart eines Gasts beim Agent des
// Proxmox-Hosts ein und liefert die Befehls-ID zum Nachverfolgen.
func (c *client) proxmoxControl(ctx context.Context, deviceID string, g pveGuest, action string) (string, error) {
	var out struct {
		CommandID string `json:"command_id"`
	}
	err := c.do(ctx, http.MethodPost, "/devices/"+url.PathEscape(deviceID)+"/proxmox-control",
		map[string]any{"node": g.Node, "type": g.Type, "vmid": g.VMID, "action": action}, &out)
	return out.CommandID, err
}

func (c *client) command(ctx context.Context, id string) (*commandState, error) {
	var out commandState
	err := c.do(ctx, http.MethodGet, "/commands/"+url.PathEscape(id), nil, &out)
	return &out, err
}

// login meldet mit Benutzer/Passwort (+ TOTP) an und legt anschließend ein
// langlebiges API-Token an – die Session (12 h) dient nur dazu. Ist ein zweiter
// Faktor nötig und kein Code angegeben, kommt errTOTPRequired zurück.
var errTOTPRequired = errors.New("zwei-faktor-code erforderlich")

func login(ctx context.Context, base, user, pass, code string, insecure bool) (token, tokenID string, err error) {
	base = strings.TrimRight(base, "/")
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", "", err
	}
	hc := &http.Client{Timeout: 20 * time.Second, Jar: jar}
	if insecure {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	c := &client{base: base, http: hc}

	var step1 struct {
		TOTPRequired bool   `json:"totp_required"`
		Pending      string `json:"pending"`
	}
	if err := c.do(ctx, http.MethodPost, "/auth/login",
		map[string]string{"username": user, "password": pass}, &step1); err != nil {
		return "", "", err
	}
	if step1.TOTPRequired {
		if strings.TrimSpace(code) == "" {
			return "", "", errTOTPRequired
		}
		if err := c.do(ctx, http.MethodPost, "/auth/login/totp",
			map[string]string{"pending": step1.Pending, "code": strings.TrimSpace(code)}, nil); err != nil {
			return "", "", err
		}
	}

	var tok struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	label := "Taskleisten-App"
	if h, herr := os.Hostname(); herr == nil && h != "" {
		label += " (" + h + ")"
	}
	if err := c.do(ctx, http.MethodPost, "/auth/api-tokens", map[string]string{"label": label}, &tok); err != nil {
		return "", "", fmt.Errorf("api-token anlegen: %w", err)
	}
	// Session gleich wieder schließen – ab jetzt zählt nur das Token.
	_ = c.do(ctx, http.MethodPost, "/auth/logout", nil, nil)
	return tok.Token, tok.ID, nil
}

// revokeToken meldet die App ab (Token serverseitig widerrufen).
func (c *client) revokeToken(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return c.do(ctx, http.MethodDelete, "/auth/api-tokens/"+url.PathEscape(id), nil, nil)
}

// normalizeBase ergänzt fehlendes Schema (https) und entfernt Pfad-Reste.
func normalizeBase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return strings.TrimRight(s, "/")
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return strings.TrimRight(u.String(), "/")
}

// wsBase macht aus der http(s)-Basis die ws(s)-Basis.
func wsBase(u string) string {
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://")
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://")
	default:
		return u
	}
}
