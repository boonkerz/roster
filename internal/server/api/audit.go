package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
)

// statusRecorder merkt sich den geschriebenen HTTP-Status für das Audit-Log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// auditActions bildet "METHOD Route-Muster" auf lesbare Aktionen ab.
var auditActions = map[string]string{
	"POST /devices/{id}/run":               "Skript ausgeführt",
	"POST /devices/{id}/checks/{checkID}/run": "Check neu ausgeführt",
	"POST /devices/{id}/tasks/{taskID}/run":   "Task neu gestartet",
	"POST /devices/{id}/external-scan":        "Außen-Erreichbarkeit geprüft",
	"POST /devices/{id}/reboot":            "Gerät neu gestartet",
	"POST /devices/{id}/read-file":         "Datei heruntergeladen",
	"POST /devices/{id}/write-file":        "Datei hochgeladen",
	"POST /devices/{id}/bitlocker":         "BitLocker-Status abgefragt",
	"POST /devices/{id}/revoke":            "Agent-Token widerrufen",
	"DELETE /devices/{id}":                 "Gerät gelöscht",
	"POST /devices/{id}/wake":              "Wake-on-LAN",
	"POST /devices/{id}/service-control":   "Dienst gesteuert",
	"POST /devices/{id}/process-kill":      "Prozess beendet",
	"POST /devices/{id}/scan-updates":      "Update-Scan gestartet",
	"POST /devices/{id}/install-updates":   "Updates installiert",
	"PUT /devices/{id}/patches/approve":    "Patch genehmigt",
	"PUT /devices/{id}/site":               "Standort geändert",
	"PUT /devices/{id}/notes":              "Notizen geändert",
	"PUT /devices/{id}/groups":             "Tags geändert",
	"POST /bulk/run-script":                "Sammelaktion: Skript",
	"POST /bulk/scan-updates":              "Sammelaktion: Update-Scan",
	"POST /policies":                       "Richtlinie angelegt",
	"PUT /policies/{id}":                   "Richtlinie geändert",
	"DELETE /policies/{id}":                "Richtlinie gelöscht",
	"POST /policies/{id}/checks":           "Check hinzugefügt",
	"DELETE /checks/{id}":                  "Check gelöscht",
	"POST /policies/{id}/tasks":            "Task hinzugefügt",
	"DELETE /tasks/{id}":                   "Task gelöscht",
	"POST /policies/{id}/assignments":      "Zuweisung hinzugefügt",
	"POST /scripts":                        "Skript angelegt",
	"PUT /scripts/{id}":                    "Skript geändert",
	"DELETE /scripts/{id}":                 "Skript gelöscht",
	"POST /maintenance":                    "Wartungsfenster angelegt",
	"DELETE /maintenance/{id}":             "Wartungsfenster gelöscht",
	"POST /report-schedules":               "Berichtsplan angelegt",
	"DELETE /report-schedules/{id}":        "Berichtsplan gelöscht",
	"POST /settings/alert-channels":        "Alarm-Kanal angelegt",
	"PUT /settings/alert-channels/{id}":    "Alarm-Kanal geändert",
	"DELETE /settings/alert-channels/{id}": "Alarm-Kanal gelöscht",
	"POST /auth/me/password":               "Passwort geändert",
	"POST /auth/2fa/enable":                "2FA aktiviert",
	"POST /auth/2fa/disable":               "2FA deaktiviert",
	"POST /auth/2fa/recovery":              "Backup-Codes erneuert",
	"POST /auth/logout":                    "Abgemeldet",
	"POST /enrollment-tokens":              "Enrollment-Token erstellt",
	"POST /users":                          "Benutzer angelegt",
	"DELETE /users/{id}":                   "Benutzer gelöscht",
	"POST /users/{id}/reset-2fa":           "2FA zurückgesetzt (Admin)",
}

func auditAction(method, pattern string) string {
	if a, ok := auditActions[method+" "+pattern]; ok {
		return a
	}
	return method + " " + pattern
}

// clientIP ermittelt die Client-IP (hinter Reverse-Proxy via X-Forwarded-For).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// handleListAudit liefert die jüngsten Audit-Einträge (Admin).
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListAudit(r.Context(), 300)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, items)
}

// logAudit schreibt einen expliziten Audit-Eintrag (z.B. Login) außerhalb der Middleware.
func (s *Server) logAudit(r *http.Request, userID, username, action string, status int) {
	_ = s.store.InsertAudit(context.Background(), model.AuditEntry{
		TS: time.Now().UTC(), UserID: userID, Username: username,
		Action: action, Method: r.Method, Path: r.URL.Path, Status: status, IP: clientIP(r),
	})
}

// audit protokolliert ändernde Anfragen (POST/PUT/DELETE) angemeldeter Nutzer.
func (s *Server) audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		pattern := ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			pattern = strings.TrimPrefix(rc.RoutePattern(), "/api/v1")
		}
		if pattern == "/auth/me/theme" {
			return // Theme-Wechsel ist nicht auditrelevant
		}
		var uid, uname string
		if u := userFrom(r.Context()); u != nil {
			uid, uname = u.ID, u.Username
		}
		// Eigener Kontext: die Antwort ist schon geschrieben, r.Context() ggf. beendet.
		_ = s.store.InsertAudit(context.Background(), model.AuditEntry{
			TS: time.Now().UTC(), UserID: uid, Username: uname,
			Action: auditAction(r.Method, pattern), Method: r.Method,
			Path: r.URL.Path, Status: rec.status, IP: clientIP(r),
		})
	})
}
