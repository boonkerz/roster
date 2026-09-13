package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNormalizeBase(t *testing.T) {
	cases := map[string]string{
		"roster.example.com":            "https://roster.example.com",
		"https://roster.example.com/":   "https://roster.example.com",
		"http://localhost:8443/devices": "http://localhost:8443",
		"  https://a.b:9/x?y=1  ":       "https://a.b:9",
		"":                              "",
	}
	for in, want := range cases {
		if got := normalizeBase(in); got != want {
			t.Errorf("normalizeBase(%q) = %q, erwartet %q", in, got, want)
		}
	}
}

func TestWSBase(t *testing.T) {
	if got := wsBase("https://a.b"); got != "wss://a.b" {
		t.Errorf("https → %q", got)
	}
	if got := wsBase("http://a.b"); got != "ws://a.b" {
		t.Errorf("http → %q", got)
	}
}

// TestLoginTOTP prüft den zweistufigen Login: ohne Code meldet login()
// errTOTPRequired, mit Code läuft er durch und legt das API-Token an.
func TestLoginTOTP(t *testing.T) {
	var gotPending, gotCode, gotLabel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"totp_required": true, "pending": "P1"})
		case "/api/v1/auth/login/totp":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotPending, gotCode = body["pending"], body["code"]
			w.WriteHeader(http.StatusOK)
		case "/api/v1/auth/api-tokens":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotLabel = body["label"]
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "T1", "token": "geheim"})
		case "/api/v1/auth/logout":
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unerwarteter Pfad %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	if _, _, err := login(context.Background(), srv.URL, "admin", "pw", "", false); err != errTOTPRequired {
		t.Fatalf("ohne Code: %v, erwartet errTOTPRequired", err)
	}
	tok, id, err := login(context.Background(), srv.URL, "admin", "pw", " 123456 ", false)
	if err != nil {
		t.Fatalf("mit Code: %v", err)
	}
	if tok != "geheim" || id != "T1" {
		t.Errorf("token=%q id=%q", tok, id)
	}
	if gotPending != "P1" || gotCode != "123456" {
		t.Errorf("pending=%q code=%q (Code muss getrimmt werden)", gotPending, gotCode)
	}
	if gotLabel == "" {
		t.Error("Token ohne Bezeichnung angelegt")
	}
}

// TestLoginBadCredentials reicht die Server-Fehlermeldung durch und erkennt 401.
func TestLoginBadCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "ungültige anmeldedaten"})
	}))
	defer srv.Close()

	_, _, err := login(context.Background(), srv.URL, "admin", "falsch", "", false)
	if err == nil || err.Error() != "ungültige anmeldedaten" {
		t.Fatalf("err = %v", err)
	}
	if !isUnauthorized(err) {
		t.Error("401 nicht als Anmeldefehler erkannt")
	}
}

// TestDevicesUsesBearer stellt sicher, dass die Geräteliste das API-Token schickt.
func TestDevicesUsesBearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode([]device{{ID: "d1", Hostname: "srv1", ChecksTotal: 3, ChecksFailing: 1}})
	}))
	defer srv.Close()

	devs, err := newClient(srv.URL, "tok", false).devices(context.Background())
	if err != nil {
		t.Fatalf("devices: %v", err)
	}
	if len(devs) != 1 || devs[0].ChecksFailing != 1 {
		t.Fatalf("devices = %+v", devs)
	}
}

// TestLoginLive läuft nur mit ROSTER_TEST_SERVER (z. B. http://127.0.0.1:18443) und
// prüft Anmeldung, Token-Erzeugung, Geräteliste und Widerruf gegen einen echten Server.
func TestLoginLive(t *testing.T) {
	base := os.Getenv("ROSTER_TEST_SERVER")
	if base == "" {
		t.Skip("ROSTER_TEST_SERVER nicht gesetzt")
	}
	user, pass := envOr("ROSTER_TEST_USER", "admin"), envOr("ROSTER_TEST_PASSWORD", "admin1234")
	ctx := context.Background()
	tok, id, err := login(ctx, base, user, pass, os.Getenv("ROSTER_TEST_TOTP"), false)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	c := newClient(base, tok, false)
	me, err := c.me(ctx)
	if err != nil || me.Username != user {
		t.Fatalf("me: %+v %v", me, err)
	}
	if _, err := c.devices(ctx); err != nil {
		t.Fatalf("devices: %v", err)
	}
	if err := c.revokeToken(ctx, id); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := c.devices(ctx); !isUnauthorized(err) {
		t.Fatalf("Token nach Widerruf noch gültig: %v", err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// TestViewerLaunchCode prüft das Startcode-Format für roster-viewer (base64url ohne
// Padding, JSON mit url/device/session/token) – der Viewer dekodiert es genauso.
func TestViewerLaunchCode(t *testing.T) {
	cfg := &config{URL: "https://roster.example.com", Insecure: true}
	d := device{ID: "dev1", Hostname: "srv1"}
	code, err := viewerLaunchCode(cfg, d, &remoteStart{Session: "s1", Token: "t1"})
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if strings.ContainsAny(code, "+/=") {
		t.Errorf("Startcode ist nicht url-sicher: %q", code)
	}
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		t.Fatalf("dekodieren: %v", err)
	}
	var blob map[string]any
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatalf("json: %v", err)
	}
	for k, want := range map[string]any{
		"url": "https://roster.example.com", "device": "dev1",
		"session": "s1", "token": "t1", "insecure": true,
		"title": "Fernsteuerung srv1",
	} {
		if blob[k] != want {
			t.Errorf("%s = %v, erwartet %v", k, blob[k], want)
		}
	}
}

// TestDeviceAddress prüft die Adresswahl für SFTP: echte IPv4 vor Hostname,
// Loopback/Link-Local und virtuelle Schnittstellen (Docker/Bridges) fliegen raus.
func TestDeviceAddress(t *testing.T) {
	cases := []struct {
		name string
		dev  device
		want string
	}{
		{"erste echte IPv4", device{Hostname: "srv1", Interfaces: []iface{
			{Name: "lo", IPv4: "127.0.0.1"},
			{Name: "docker0", IPv4: "172.17.0.1"},
			{Name: "eth0", IPv4: "192.168.1.10"},
		}}, "192.168.1.10"},
		{"link-local übersprungen", device{Hostname: "srv2", Interfaces: []iface{
			{Name: "eth0", IPv4: "169.254.3.4"},
			{Name: "eth1", IPv4: "10.0.0.5"},
		}}, "10.0.0.5"},
		{"ohne IP der Hostname", device{Hostname: "srv3", Interfaces: []iface{
			{Name: "lo", IPv4: "127.0.0.1"},
		}}, "srv3"},
		{"ohne alles die öffentliche IP", device{PublicIP: "203.0.113.7"}, "203.0.113.7"},
		{"nichts bekannt", device{}, ""},
	}
	for _, c := range cases {
		if got := deviceAddress(c.dev); got != c.want {
			t.Errorf("%s: deviceAddress = %q, erwartet %q", c.name, got, c.want)
		}
	}
}

// TestSFTPURL prüft die erzeugte Adresse mit und ohne Benutzernamen.
func TestSFTPURL(t *testing.T) {
	d := device{Hostname: "srv1", Interfaces: []iface{{Name: "eth0", IPv4: "192.168.1.10"}}}
	got, err := sftpURL(&config{}, d)
	if err != nil || got != "sftp://192.168.1.10/" {
		t.Errorf("ohne Benutzer: %q (%v)", got, err)
	}
	got, err = sftpURL(&config{SFTPUser: " admin "}, d)
	if err != nil || got != "sftp://admin@192.168.1.10/" {
		t.Errorf("mit Benutzer: %q (%v)", got, err)
	}
	if _, err := sftpURL(&config{}, device{}); err == nil {
		t.Error("ohne Adresse muss ein Fehler kommen")
	}
}

// TestExpandSFTPCommand prüft die Platzhalter des eigenen Kommandos.
func TestExpandSFTPCommand(t *testing.T) {
	d := device{Hostname: "srv1", Interfaces: []iface{{Name: "eth0", IPv4: "192.168.1.10"}}}
	cfg := &config{SFTPUser: "admin", SFTPCommand: "filezilla {{url}} --name={{name}} {{user}}@{{host}}"}
	got := expandSFTPCommand(cfg.SFTPCommand, cfg, d, "sftp://admin@192.168.1.10/")
	want := []string{"filezilla", "sftp://admin@192.168.1.10/", "--name=srv1", "admin@192.168.1.10"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, erwartet %q", i, got[i], want[i])
		}
	}
}

// TestOpenSFTPCustomCommand startet über den konfigurierten Befehl (hier /bin/echo,
// damit der Test kein Fenster öffnet) und meldet das Programm zurück.
func TestOpenSFTPCustomCommand(t *testing.T) {
	if _, err := os.Stat("/bin/echo"); err != nil {
		t.Skip("kein /bin/echo")
	}
	cfg := &config{SFTPUser: "admin", SFTPCommand: "/bin/echo {{url}}"}
	d := device{Hostname: "srv1", Interfaces: []iface{{Name: "eth0", IPv4: "192.168.1.10"}}}
	prog, err := openSFTP(cfg, d)
	if err != nil {
		t.Fatalf("openSFTP: %v", err)
	}
	if prog != "/bin/echo" {
		t.Errorf("prog = %q", prog)
	}
	if _, err := openSFTP(cfg, device{}); err == nil {
		t.Error("Gerät ohne Adresse muss einen Fehler liefern")
	}
}
