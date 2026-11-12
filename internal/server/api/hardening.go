package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Härtung der öffentlichen Angriffsfläche: Versuchszähler gegen Passwort-/Token-Raten,
// Herkunftsprüfung für Cookie-Sitzungen (CSRF) und WebSockets, Sicherheits-Header.

// attemptLimiter zählt Fehlversuche je Schlüssel (IP, Nutzer) und sperrt nach zu vielen.
type attemptLimiter struct {
	mu     sync.Mutex
	max    int           // Fehlversuche bis zur Sperre
	window time.Duration // Zählfenster
	block  time.Duration // Sperrdauer
	m      map[string]*attemptEntry
	now    func() time.Time
}

type attemptEntry struct {
	fails int
	first time.Time
	until time.Time
}

func newAttemptLimiter(max int, window, block time.Duration) *attemptLimiter {
	return &attemptLimiter{max: max, window: window, block: block, m: map[string]*attemptEntry{}, now: time.Now}
}

// blocked meldet, ob der Schlüssel gesperrt ist, und wie lange noch.
func (l *attemptLimiter) blocked(key string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[key]
	if e == nil {
		return 0, false
	}
	if rest := e.until.Sub(l.now()); rest > 0 {
		return rest, true
	}
	return 0, false
}

// fail zählt einen Fehlversuch; ab max im Fenster wird gesperrt (Sperre verlängert sich
// mit jedem weiteren Fehlversuch).
func (l *attemptLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.gc(now)
	e := l.m[key]
	if e == nil || now.Sub(e.first) > l.window && now.After(e.until) {
		e = &attemptEntry{first: now}
		l.m[key] = e
	}
	e.fails++
	if e.fails >= l.max {
		e.until = now.Add(l.block)
	}
}

// reset löscht den Zähler (erfolgreiche Anmeldung).
func (l *attemptLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}

func (l *attemptLimiter) gc(now time.Time) {
	if len(l.m) < 10000 {
		return
	}
	for k, e := range l.m {
		if now.Sub(e.first) > l.window && now.After(e.until) {
			delete(l.m, k)
		}
	}
}

// loginKeys liefert die Zähl-Schlüssel eines Anmeldeversuchs: Quell-IP und Nutzername.
func (s *Server) loginKeys(r *http.Request, username string) []string {
	keys := []string{"ip:" + s.clientIP(r)}
	if u := strings.ToLower(strings.TrimSpace(username)); u != "" {
		keys = append(keys, "user:"+u)
	}
	return keys
}

// tooMany antwortet mit 429, wenn einer der Schlüssel gesperrt ist.
func (s *Server) tooMany(w http.ResponseWriter, keys ...string) bool {
	for _, k := range keys {
		if rest, ok := s.limiter.blocked(k); ok {
			w.Header().Set("Retry-After", itoa(int(rest.Seconds())+1))
			s.writeErr(w, http.StatusTooManyRequests, "zu viele Fehlversuche – bitte in "+humanWait(rest)+" erneut versuchen")
			return true
		}
	}
	return false
}

func humanWait(d time.Duration) string {
	if d < time.Minute {
		return itoa(int(d.Seconds())+1) + " Sekunden"
	}
	return itoa(int(d.Minutes())+1) + " Minuten"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// acquirePasswordSlot begrenzt gleichzeitige argon2-Prüfungen (je 64 MB) – sonst kann
// eine Flut von Anmeldeversuchen den Server in den Speichermangel treiben.
func (s *Server) acquirePasswordSlot(w http.ResponseWriter) bool {
	select {
	case s.pwSem <- struct{}{}:
		return true
	case <-time.After(3 * time.Second):
		w.Header().Set("Retry-After", "5")
		s.writeErr(w, http.StatusTooManyRequests, "zu viele gleichzeitige Anmeldungen – bitte gleich erneut versuchen")
		return false
	}
}

func (s *Server) releasePasswordSlot() { <-s.pwSem }

// trustedHosts: Hostnamen, von denen Browser-Anfragen mit Cookie kommen dürfen – der
// angefragte Host selbst, die konfigurierte externe URL und (hinter Proxy) der
// weitergereichte Host.
func (s *Server) trustedHosts(r *http.Request) []string {
	hosts := []string{strings.ToLower(r.Host)}
	if s.cfg.ExternalURL != "" {
		if u, err := url.Parse(s.cfg.ExternalURL); err == nil && u.Host != "" {
			hosts = append(hosts, strings.ToLower(u.Host))
		}
	}
	if s.cfg.BehindProxy {
		if h := r.Header.Get("X-Forwarded-Host"); h != "" {
			hosts = append(hosts, strings.ToLower(strings.TrimSpace(strings.Split(h, ",")[0])))
		}
	}
	return hosts
}

// isLoopbackHost: Entwicklungsbetrieb (Vite-Dev-Server auf anderem Port) erkennen.
func isLoopbackHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

// originAllowed prüft einen Origin-Header gegen die vertrauenswürdigen Hosts.
func (s *Server) originAllowed(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	oh := strings.ToLower(u.Host)
	for _, h := range s.trustedHosts(r) {
		if oh == h {
			return true
		}
	}
	// Entwicklung: Frontend von localhost:5173 gegen Server auf 127.0.0.1:8443.
	return isLoopbackHost(r.Host) && isLoopbackHost(oh)
}

// sameSiteRequest prüft eine ändernde Anfrage mit Cookie-Sitzung auf Herkunft (CSRF-
// Schutz zusätzlich zu SameSite=Lax): Sec-Fetch-Site „cross-site“ und fremde Origins
// werden abgewiesen. Ohne beide Header (ältere Clients, curl) wird nicht blockiert.
func (s *Server) sameSiteRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs == "cross-site" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		return s.originAllowed(r, o)
	}
	return true
}

// wsAcceptOptions: WebSocket-Herkunft auf die eigenen Hosts beschränken. Browser senden
// immer einen Origin; native Clients (Viewer, Tray, Agent) keinen – die lässt die
// Bibliothek ohne Origin durch, sie weisen sich per Token aus.
func (s *Server) wsOriginPatterns(r *http.Request) []string {
	var pats []string
	for _, h := range s.trustedHosts(r) {
		pats = append(pats, h)
	}
	if isLoopbackHost(r.Host) {
		pats = append(pats, "localhost:*", "127.0.0.1:*", "[::1]:*")
	}
	return pats
}

// securityHeaders setzt Browser-Schutzmechanismen für alle Antworten.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil || s.cfg.BehindProxy {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
