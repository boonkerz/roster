package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// slowRequest ist die Schwelle, ab der eine Anfrage im Protokoll landet.
const slowRequest = 5 * time.Second

// logSlowRequests protokolliert Anfragen, die länger als slowRequest dauern – mit Pfad,
// Status und Dauer. Long-Polls und WebSockets sind ausgenommen, die sind absichtlich lang.
func (s *Server) logSlowRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasSuffix(p, "/agent/wait") || strings.HasSuffix(p, "/terminal") || strings.HasSuffix(p, "-ws") || strings.HasSuffix(p, "/ws") {
			next.ServeHTTP(w, r)
			return
		}
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		if d := time.Since(start); d > slowRequest {
			s.log.Warn("langsame anfrage", "method", r.Method, "path", p, "status", ww.Status(), "dauer", d.Round(time.Millisecond).String())
		}
	})
}
