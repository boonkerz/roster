package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/config"
)

func TestAttemptLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := newAttemptLimiter(3, 10*time.Minute, 15*time.Minute)
	l.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		l.fail("ip:1")
	}
	if _, b := l.blocked("ip:1"); b {
		t.Fatal("zu früh gesperrt")
	}
	l.fail("ip:1")
	if rest, b := l.blocked("ip:1"); !b || rest != 15*time.Minute {
		t.Fatal("nicht gesperrt", rest, b)
	}
	now = now.Add(16 * time.Minute)
	if _, b := l.blocked("ip:1"); b {
		t.Fatal("Sperre nicht abgelaufen")
	}
	l.fail("ip:1") // neues Fenster: zählt wieder von vorn
	if _, b := l.blocked("ip:1"); b {
		t.Fatal("altes Fenster zählt weiter")
	}
	l.reset("ip:1")
	if l.m["ip:1"] != nil {
		t.Fatal("reset")
	}
}

func TestSameSiteAndOrigins(t *testing.T) {
	s := &Server{cfg: config.Config{ExternalURL: "https://inventory.example.de", BehindProxy: true}}
	req := func(method, host, origin, sfs string) *http.Request {
		r := httptest.NewRequest(method, "http://"+host+"/api/v1/x", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if sfs != "" {
			r.Header.Set("Sec-Fetch-Site", sfs)
		}
		return r
	}
	cases := []struct {
		r    *http.Request
		want bool
	}{
		{req("GET", "inventory.example.de", "https://evil.example", "cross-site"), true}, // lesend: egal
		{req("POST", "inventory.example.de", "https://inventory.example.de", "same-origin"), true},
		{req("POST", "inventory.example.de", "https://evil.example", "cross-site"), false},
		{req("POST", "inventory.example.de", "https://evil.example", ""), false},
		{req("POST", "10.0.0.5:8080", "https://inventory.example.de", ""), true},    // externe URL hinter Proxy
		{req("POST", "inventory.example.de", "", ""), true},                         // curl mit Cookie: keine Header
		{req("POST", "127.0.0.1:8443", "http://localhost:5173", "same-site"), true}, // Vite-Dev
		{req("POST", "inventory.example.de", "http://localhost:5173", ""), false},
	}
	for i, c := range cases {
		if got := s.sameSiteRequest(c.r); got != c.want {
			t.Errorf("fall %d: %v", i, got)
		}
	}
	r := req("GET", "inventory.example.de", "", "")
	r.Header.Set("X-Forwarded-Host", "inv2.example.de")
	pats := s.wsOriginPatterns(r)
	if len(pats) != 3 || pats[0] != "inventory.example.de" || pats[2] != "inv2.example.de" {
		t.Fatal(pats)
	}
	if got := s.wsOriginPatterns(req("GET", "localhost:8443", "", "")); len(got) != 5 {
		t.Fatal(got)
	}
}

func TestClientIPForwarded(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:4444"
	r.Header.Set("X-Forwarded-For", "10.9.9.9, 192.0.2.1")
	if ip := (&Server{}).clientIP(r); ip != "203.0.113.9" {
		t.Fatal("ohne Proxy wurde XFF geglaubt:", ip)
	}
	if ip := (&Server{cfg: config.Config{BehindProxy: true}}).clientIP(r); ip != "10.9.9.9" {
		t.Fatal("hinter Proxy:", ip)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := &Server{cfg: config.Config{BehindProxy: true}}
	rec := httptest.NewRecorder()
	s.securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).
		ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for k, v := range map[string]string{"X-Frame-Options": "DENY", "X-Content-Type-Options": "nosniff", "Strict-Transport-Security": "max-age=31536000"} {
		if rec.Header().Get(k) != v {
			t.Errorf("%s = %q", k, rec.Header().Get(k))
		}
	}
}
