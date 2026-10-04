package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/alert"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

// --- Modulares Alerting: Master-Schalter + Provider-Katalog + Kanal-CRUD ---

type alertsResponse struct {
	Enabled               bool                 `json:"enabled"`
	AlertSoftware         bool                 `json:"alert_software"`
	AlertAfterRemediation bool                 `json:"alert_after_remediation"`
	Channels              []model.AlertChannel `json:"channels"`
}

// handleGetAlerts liefert den Master-Schalter und alle Kanäle (Secrets maskiert).
func (s *Server) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetAlertConfig(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	channels, err := s.store.AlertChannels(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	for i := range channels {
		maskSecrets(&channels[i])
	}
	s.writeJSON(w, http.StatusOK, alertsResponse{Enabled: cfg.Enabled, AlertSoftware: cfg.AlertSoftware,
		AlertAfterRemediation: cfg.AlertAfterRemediation, Channels: channels})
}

// handleSetAlertsEnabled schaltet das Alerting global an/aus (inkl. Software-Alarm).
func (s *Server) handleSetAlertsEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled               bool  `json:"enabled"`
		AlertSoftware         *bool `json:"alert_software"`
		AlertAfterRemediation *bool `json:"alert_after_remediation"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	cfg, err := s.store.GetAlertConfig(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	cfg.Enabled = req.Enabled
	if req.AlertSoftware != nil {
		cfg.AlertSoftware = *req.AlertSoftware
	}
	if req.AlertAfterRemediation != nil {
		cfg.AlertAfterRemediation = *req.AlertAfterRemediation
	}
	if err := s.store.SaveAlertConfig(r.Context(), cfg); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gespeichert"})
}

// handleListAlertProviders liefert den Provider-Katalog für die UI-Formulare.
func (s *Server) handleListAlertProviders(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, alert.Catalog())
}

type channelRequest struct {
	Type        string               `json:"type"`
	Name        string               `json:"name"`
	Enabled     bool                 `json:"enabled"`
	Config      map[string]string    `json:"config"`
	MinSeverity string               `json:"min_severity"`
	Assignments []model.ChannelScope `json:"assignments"`
}

func (s *Server) handleCreateAlertChannel(w http.ResponseWriter, r *http.Request) {
	var req channelRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if alert.ProviderByType(req.Type) == nil {
		s.writeErr(w, http.StatusBadRequest, "unbekannter kanal-typ")
		return
	}
	ch := model.AlertChannel{
		ID: store.NewID(), Type: req.Type, Name: req.Name, Enabled: req.Enabled, Config: req.Config,
		MinSeverity: req.MinSeverity, Assignments: req.Assignments,
	}
	if ch.Config == nil {
		ch.Config = map[string]string{}
	}
	if err := s.store.SaveAlertChannel(r.Context(), ch); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	maskSecrets(&ch)
	s.writeJSON(w, http.StatusCreated, ch)
}

func (s *Server) handleUpdateAlertChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cur, err := s.store.AlertChannel(r.Context(), id)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	var req channelRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	// Typ bleibt fix; Config übernehmen, aber leere Secrets = unverändert.
	cfg := req.Config
	if cfg == nil {
		cfg = map[string]string{}
	}
	for key := range alert.SecretKeys(cur.Type) {
		if cfg[key] == "" {
			if old, ok := cur.Config[key]; ok {
				cfg[key] = old
			}
		}
	}
	ch := model.AlertChannel{
		ID: id, Type: cur.Type, Name: req.Name, Enabled: req.Enabled, Config: cfg,
		MinSeverity: req.MinSeverity, Assignments: req.Assignments,
	}
	if err := s.store.SaveAlertChannel(r.Context(), ch); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	maskSecrets(&ch)
	s.writeJSON(w, http.StatusOK, ch)
}

func (s *Server) handleDeleteAlertChannel(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAlertChannel(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

// handleTestAlertChannel sendet synchron eine Testnachricht über den Kanal.
func (s *Server) handleTestAlertChannel(w http.ResponseWriter, r *http.Request) {
	ch, err := s.store.AlertChannel(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	p := alert.ProviderByType(ch.Type)
	if p == nil {
		s.writeErr(w, http.StatusBadRequest, "unbekannter kanal-typ")
		return
	}
	n := alert.Notification{
		Subject: "[Roster] Testbenachrichtigung",
		Body:    "Dies ist eine Testnachricht des Roster-Alertings über Kanal \"" + ch.Name + "\".",
	}
	if err := p.Send(r.Context(), ch.Config, n); err != nil {
		s.writeErr(w, http.StatusBadGateway, "versand fehlgeschlagen: "+err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gesendet"})
}

// maskSecrets leert geheime Felder, bevor ein Kanal an den Client geht.
func maskSecrets(ch *model.AlertChannel) {
	for key := range alert.SecretKeys(ch.Type) {
		if ch.Config[key] != "" {
			ch.Config[key] = ""
		}
	}
}
