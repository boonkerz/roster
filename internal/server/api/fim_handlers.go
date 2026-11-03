package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// --- Richtlinie: Bereich „Sicherheitsüberwachung“ ---

// validatePolicySecurity prüft Intervalle, Profil und Regel-IDs.
func validatePolicySecurity(sec model.PolicySecurity) (model.PolicySecurity, error) {
	if sec.FIMIntervalMin < 5 || sec.FIMIntervalMin > 1440 {
		return sec, errors.New("fim_interval_min muss zwischen 5 und 1440 liegen")
	}
	if sec.ComplianceProfile != "server" && sec.ComplianceProfile != "workstation" {
		return sec, errors.New("compliance_profile muss server|workstation sein")
	}
	if sec.ComplianceIntervalH < 1 || sec.ComplianceIntervalH > 168 {
		return sec, errors.New("compliance_interval_h muss zwischen 1 und 168 liegen")
	}
	clean := []string{}
	for _, id := range sec.ComplianceDisabled {
		if id = strings.TrimSpace(id); id != "" {
			clean = append(clean, id)
		}
	}
	sec.ComplianceDisabled = clean
	return sec, nil
}

func (s *Server) handleSetPolicySecurity(w http.ResponseWriter, r *http.Request) {
	var req model.PolicySecurity
	if !s.decodeJSON(w, r, &req) {
		return
	}
	sec, err := validatePolicySecurity(req)
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SetPolicySecurity(r.Context(), chi.URLParam(r, "id"), sec); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, sec)
}

var winAbsPath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

type fimRuleRequest struct {
	Path      string   `json:"path"`
	Recursive bool     `json:"recursive"`
	Exclude   []string `json:"exclude"`
	Platforms []string `json:"platforms"`
	Alert     *bool    `json:"alert"`
}

// apply validiert und überträgt die Felder: absoluter Pfad (Unix oder Windows), gültige
// Glob-Muster, bekannte Plattformen.
func (req fimRuleRequest) apply(r *model.PolicyFIMRule) error {
	p := strings.TrimSpace(req.Path)
	if p == "" {
		return errors.New("Pfad fehlt")
	}
	if shared.IsRegistryPath(p) {
		p = shared.NormalizeRegistryPath(p)
		if len(req.Platforms) == 0 {
			req.Platforms = []string{"windows"}
		}
	} else if !strings.HasPrefix(p, "/") && !winAbsPath.MatchString(p) {
		return errors.New("Pfad muss absolut sein (/…, C:\\… oder HKLM\\…)")
	}
	r.Path = p
	r.Recursive = req.Recursive
	r.Exclude = []string{}
	for _, ex := range req.Exclude {
		ex = strings.TrimSpace(ex)
		if ex == "" {
			continue
		}
		if _, err := path.Match(strings.ReplaceAll(ex, "\\", "/"), ""); err != nil {
			return fmt.Errorf("ungültiges Muster %q", ex)
		}
		r.Exclude = append(r.Exclude, ex)
	}
	r.Platforms = []string{}
	for _, pl := range req.Platforms {
		pl = strings.ToLower(strings.TrimSpace(pl))
		switch pl {
		case "windows", "linux", "darwin":
			r.Platforms = append(r.Platforms, pl)
		case "":
		default:
			return fmt.Errorf("unbekannte Plattform %q", pl)
		}
	}
	r.Alert = true
	if req.Alert != nil {
		r.Alert = *req.Alert
	}
	return nil
}

func (s *Server) handleAddFIMRule(w http.ResponseWriter, r *http.Request) {
	var req fimRuleRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	rule := model.PolicyFIMRule{PolicyID: chi.URLParam(r, "id")}
	if err := req.apply(&rule); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveFIMRule(r.Context(), &rule); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleUpdateFIMRule(w http.ResponseWriter, r *http.Request) {
	var req fimRuleRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	rule, err := s.store.GetFIMRule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if err := req.apply(rule); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveFIMRule(r.Context(), rule); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleDeleteFIMRule(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFIMRule(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Ereignisse ---

func (s *Server) fimFilter(r *http.Request) store.FIMFilter {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if days <= 0 || days > 3650 {
		days = 30
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	return store.FIMFilter{
		Allowed: s.scopeFilter(r), ClientID: q.Get("client"), SiteID: q.Get("site"),
		Since: time.Now().AddDate(0, 0, -days), Change: q.Get("change"), Query: q.Get("q"),
		OpenOnly: q.Get("open") == "1", Limit: limit,
	}
}

// handleFIMFleet liefert Datei-Änderungen aller Geräte (GET /security/fim).
func (s *Server) handleFIMFleet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.FIMEvents(r.Context(), s.fimFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleDeviceFIM liefert die Datei-Änderungen eines Geräts.
func (s *Server) handleDeviceFIM(w http.ResponseWriter, r *http.Request) {
	f := s.fimFilter(r)
	f.DeviceID = chi.URLParam(r, "id")
	rows, err := s.store.FIMEvents(r.Context(), f)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleAckFIM bestätigt Ereignisse (PUT /security/fim/ack {ids}).
func (s *Server) handleAckFIM(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	n, err := s.store.AckFIMEvents(r.Context(), req.IDs, s.scopeFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"acknowledged": n})
}

func (s *Server) handleFIMRescan(w http.ResponseWriter, r *http.Request) {
	s.queueDeviceCmd(w, r, "fim_rescan", "Datei-Integrität prüfen", nil)
}

func (s *Server) handleFIMReset(w http.ResponseWriter, r *http.Request) {
	s.queueDeviceCmd(w, r, "fim_reset", "Datei-Integrität: Baseline neu setzen", nil)
}

// alertFIMEvents bündelt alarmierende Datei-Änderungen eines Checkins zu einem Vorfall
// „fim_change“ (Rückgabe für Active Response und die Meldung über alertIncidents).
func (s *Server) alertFIMEvents(ctx context.Context, device *model.Device, events []shared.FIMEvent) *model.SecurityIncident {
	label := map[string]string{"created": "neu", "modified": "geändert", "deleted": "gelöscht", "attrs": "Rechte/Besitzer"}
	var lines []string
	for _, e := range events {
		if !e.Alert || label[e.Change] == "" {
			continue
		}
		detail := ""
		switch e.Change {
		case "modified":
			detail = " (" + short(e.OldHash) + " → " + short(e.NewHash) + ")"
		case "attrs":
			detail = " (" + e.OldMode + " " + e.OldOwner + " → " + e.NewMode + " " + e.NewOwner + ")"
		}
		lines = append(lines, fmt.Sprintf("  • [%s] %s%s", label[e.Change], e.Path, detail))
	}
	if len(lines) == 0 {
		return nil
	}
	// Die Meldung verschickt alertIncidents nach der Active Response (Richtlinien-Filter).
	inc := &model.SecurityIncident{DeviceID: device.ID, TS: time.Now(), Kind: "fim_change", Severity: "warning",
		Title: fmt.Sprintf("Datei-Änderungen (%d)", len(lines)), Detail: strings.Join(lines, "\n"), Status: "open"}
	applySeverity(s.securitySettings(ctx), inc)
	if err := s.store.InsertIncident(ctx, inc); err != nil {
		s.log.Error("fim-vorfall speichern", "err", err)
		return nil
	}
	return inc
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12] + "…"
	}
	if h == "" {
		return "—"
	}
	return h
}
