package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/store"
)

// Zentrale Zeitzone. Sie entscheidet, was "täglich 21:00" bedeutet – für die
// serverseitig geplanten Backups ebenso wie für die Tasks/Checks, die der Agent selbst
// plant (er bekommt den Namen im Policy-Bundle mitgeschickt). Ohne sie rechnete jedes
// Gerät in seiner eigenen Systemzeit, und derselbe Zeitplan liefe überall anders.
//
// Leer = wie bisher: der Server nimmt time.Local, der Agent seine eigene lokale Zone.

// location liefert die eingestellte Zone. Der Wert wird bei jedem Aufruf gelesen (ein
// Index-Treffer) und nur beim Auflösen gecacht, damit eine Änderung in den Einstellungen
// ohne Serverneustart greift.
func (s *Server) location(ctx context.Context) *time.Location {
	name := s.timezoneName(ctx)
	if name == "" {
		return time.Local
	}
	s.tzMu.RLock()
	if s.tzName == name && s.tzLoc != nil {
		loc := s.tzLoc
		s.tzMu.RUnlock()
		return loc
	}
	s.tzMu.RUnlock()

	loc, err := time.LoadLocation(name)
	if err != nil {
		// Unbekannter Name (z.B. Tippfehler): lieber weiterlaufen als Zeitpläne verlieren.
		s.log.Warn("zeitzone nicht auflösbar", "zone", name, "err", err)
		return time.Local
	}
	s.tzMu.Lock()
	s.tzName, s.tzLoc = name, loc
	s.tzMu.Unlock()
	return loc
}

// timezoneName liefert den eingestellten IANA-Namen ("" = nicht gesetzt).
func (s *Server) timezoneName(ctx context.Context) string {
	name, err := s.store.GetSetting(ctx, store.SettingTimezone)
	if err != nil {
		s.log.Error("zeitzone lesen", "err", err)
		return ""
	}
	return strings.TrimSpace(name)
}

type generalSettings struct {
	Timezone  string `json:"timezone"`             // "" = Systemzeit des jeweiligen Geräts
	ServerNow string `json:"server_now,omitempty"` // aktuelle Zeit in der wirksamen Zone (nur lesend)
	Effective string `json:"effective,omitempty"`  // tatsächlich verwendete Zone (Fallback sichtbar machen)
}

// handleGetGeneralSettings liefert die allgemeinen Einstellungen samt aktueller Serverzeit
// – so sieht man in der Oberfläche sofort, ob die Zone die gemeinte ist.
func (s *Server) handleGetGeneralSettings(w http.ResponseWriter, r *http.Request) {
	loc := s.location(r.Context())
	s.writeJSON(w, http.StatusOK, generalSettings{
		Timezone:  s.timezoneName(r.Context()),
		ServerNow: time.Now().In(loc).Format(time.RFC3339),
		Effective: loc.String(),
	})
}

// handleSetGeneralSettings speichert die Zeitzone. Der Name wird geprüft, bevor er in die
// Datenbank geht – ein Tippfehler würde sonst still auf time.Local zurückfallen.
func (s *Server) handleSetGeneralSettings(w http.ResponseWriter, r *http.Request) {
	var req generalSettings
	if !s.decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Timezone)
	if name != "" {
		if _, err := time.LoadLocation(name); err != nil {
			s.writeErr(w, http.StatusBadRequest, "unbekannte Zeitzone: "+name)
			return
		}
	}
	if err := s.store.SetSetting(r.Context(), store.SettingTimezone, name); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.tzMu.Lock()
	s.tzName, s.tzLoc = "", nil // Cache verwerfen, nächster Zugriff löst neu auf
	s.tzMu.Unlock()
	s.log.Info("zeitzone gesetzt", "zone", name)
	s.handleGetGeneralSettings(w, r)
}
