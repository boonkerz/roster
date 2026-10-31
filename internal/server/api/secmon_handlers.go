package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/secdetect"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// --- Einstellungen der Anmeldungs-Überwachung ---

type securitySettings struct {
	BFThreshold      int      `json:"bf_threshold"`
	BFWindowMin      int      `json:"bf_window_min"`
	NewIPAlert       bool     `json:"new_ip_alert"`
	NewIPIgnorePriv  bool     `json:"new_ip_ignore_private"`
	LearnDays        int      `json:"learn_days"`
	NeverBlock       []string `json:"never_block"`
	ResponseCooldown int      `json:"response_cooldown_min"`
}

func (s *Server) securitySettings(ctx context.Context) securitySettings {
	cfg := securitySettings{BFThreshold: 10, BFWindowMin: 10, NewIPAlert: true, LearnDays: 7, NeverBlock: []string{}, ResponseCooldown: 30}
	geti := func(key string, dst *int) {
		if v, _ := s.store.GetSetting(ctx, key); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	geti(store.SettingSecBFThreshold, &cfg.BFThreshold)
	geti(store.SettingSecBFWindowMin, &cfg.BFWindowMin)
	geti(store.SettingSecLearnDays, &cfg.LearnDays)
	geti(store.SettingSecResponseCooldown, &cfg.ResponseCooldown)
	if v, _ := s.store.GetSetting(ctx, store.SettingSecNewIPAlert); v == "0" {
		cfg.NewIPAlert = false
	}
	if v, _ := s.store.GetSetting(ctx, store.SettingSecNewIPIgnorePriv); v == "1" {
		cfg.NewIPIgnorePriv = true
	}
	if v, _ := s.store.GetSetting(ctx, store.SettingSecNeverBlock); strings.TrimSpace(v) != "" {
		for _, c := range strings.Split(v, ",") {
			if c = strings.TrimSpace(c); c != "" {
				cfg.NeverBlock = append(cfg.NeverBlock, c)
			}
		}
	}
	return cfg
}

func (s *Server) handleGetSecuritySettings(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.securitySettings(r.Context()))
}

func (s *Server) handleSetSecuritySettings(w http.ResponseWriter, r *http.Request) {
	var req securitySettings
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.BFThreshold < 2 || req.BFThreshold > 1000 || req.BFWindowMin < 1 || req.BFWindowMin > 1440 || req.LearnDays < 0 || req.LearnDays > 365 || req.ResponseCooldown < 0 || req.ResponseCooldown > 10080 {
		s.writeErr(w, http.StatusBadRequest, "Werte außerhalb des erlaubten Bereichs")
		return
	}
	var cidrs []string
	for _, c := range req.NeverBlock {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if strings.Contains(c, ":") {
				c += "/128"
			} else {
				c += "/32"
			}
		}
		if _, _, err := parseCIDR(c); err != nil {
			s.writeErr(w, http.StatusBadRequest, "ungültiger CIDR: "+c)
			return
		}
		cidrs = append(cidrs, c)
	}
	b2s := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	set := map[string]string{
		store.SettingSecBFThreshold: strconv.Itoa(req.BFThreshold), store.SettingSecBFWindowMin: strconv.Itoa(req.BFWindowMin),
		store.SettingSecNewIPAlert: b2s(req.NewIPAlert), store.SettingSecNewIPIgnorePriv: b2s(req.NewIPIgnorePriv),
		store.SettingSecLearnDays: strconv.Itoa(req.LearnDays), store.SettingSecNeverBlock: strings.Join(cidrs, ","),
		store.SettingSecResponseCooldown: strconv.Itoa(req.ResponseCooldown),
	}
	for k, v := range set {
		if err := s.store.SetSetting(r.Context(), k, v); err != nil {
			s.mapStoreErr(w, err)
			return
		}
	}
	s.handleGetSecuritySettings(w, r)
}

// --- Checkin: Ereignisse speichern, Vorfälle ableiten, melden ---

// processAuthEvents speichert die Anmeldeereignisse eines Checkins, leitet Vorfälle ab,
// meldet sie (warning/critical) und lernt erfolgreiche (Benutzer, IP)-Paare.
func (s *Server) processAuthEvents(ctx context.Context, device *model.Device, events []shared.AuthEvent) []model.SecurityIncident {
	fresh, err := s.store.InsertAuthEvents(ctx, device.ID, events)
	if err != nil {
		s.log.Error("auth-ereignisse speichern", "err", err)
		return nil
	}
	if len(fresh) == 0 {
		return nil
	}
	cfg := s.securitySettings(ctx)
	dcfg := secdetect.DefaultConfig()
	dcfg.BFThreshold, dcfg.BFWindow = cfg.BFThreshold, time.Duration(cfg.BFWindowMin)*time.Minute
	dcfg.NewIPAlert, dcfg.IgnorePrivateIP, dcfg.LearnDays = cfg.NewIPAlert, cfg.NewIPIgnorePriv, cfg.LearnDays
	now := time.Now()
	prior, _ := s.store.FailedLoginsSince(ctx, device.ID, now.Add(-time.Hour))
	// Die gerade eingefügten Ereignisse stecken schon in prior – herausrechnen, damit sie
	// nicht doppelt zählen: wir zählen stattdessen nur ältere als das erste neue Ereignis.
	first := fresh[0].Time
	for _, e := range fresh {
		if e.Time.Before(first) {
			first = e.Time
		}
	}
	for ip, ts := range prior {
		var keep []time.Time
		for _, t := range ts {
			if t.Before(first) {
				keep = append(keep, t)
			}
		}
		prior[ip] = keep
	}
	recentBF, _ := s.store.RecentIncidentIPs(ctx, device.ID, "brute_force", now.Add(-time.Hour))
	known, _ := s.store.KnownIPs(ctx, device.ID)
	incidents := secdetect.Detect(secdetect.Input{
		Now: now, Events: fresh, PriorFails: prior, RecentBF: recentBF,
		KnownIP:         func(u, ip string) bool { return known[u+"@"+ip] },
		MonitoringSince: s.store.AuthMonitorSince(ctx, device.ID),
	}, dcfg)
	_ = s.store.UpsertKnownIPs(ctx, device.ID, fresh)
	var saved []model.SecurityIncident
	for _, inc := range incidents {
		m := model.SecurityIncident{DeviceID: device.ID, TS: inc.Time, Kind: inc.Kind, Severity: inc.Severity, SourceIP: inc.SourceIP,
			Username: inc.User, Title: inc.Title, Detail: inc.Detail, Status: "open"}
		if err := s.store.InsertIncident(ctx, &m); err != nil {
			s.log.Error("vorfall speichern", "err", err)
			continue
		}
		saved = append(saved, m)
		if inc.Severity == "warning" || inc.Severity == "critical" {
			s.dispatchDeviceAlert(ctx, device, inc.Severity,
				fmt.Sprintf("[Roster] Sicherheitsvorfall auf %s: %s", device.Hostname, inc.Title),
				fmt.Sprintf("Gerät %q, %s\n\n%s\n\nArt: %s · Quelle: %s · Benutzer: %s", device.Hostname, inc.Time.Format("02.01.2006 15:04:05"), inc.Detail, inc.Kind, orDash(inc.SourceIP), orDash(inc.User)))
		}
	}
	return saved
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

// --- Lesen / Bearbeiten ---

func (s *Server) incidentFilter(r *http.Request) store.IncidentFilter {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if days <= 0 || days > 3650 {
		days = 30
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	return store.IncidentFilter{Allowed: s.scopeFilter(r), ClientID: q.Get("client"), SiteID: q.Get("site"), Since: time.Now().AddDate(0, 0, -days),
		Status: q.Get("status"), Kind: q.Get("kind"), Severity: q.Get("severity"), Query: q.Get("q"), Limit: limit}
}

func (s *Server) handleIncidents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Incidents(r.Context(), s.incidentFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleDeviceIncidents(w http.ResponseWriter, r *http.Request) {
	f := s.incidentFilter(r)
	f.DeviceID = chi.URLParam(r, "id")
	rows, err := s.store.Incidents(r.Context(), f)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleSetIncidentStatus setzt open|acknowledged|resolved (nur im Daten-Scope).
func (s *Server) handleSetIncidentStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Status != "open" && req.Status != "acknowledged" && req.Status != "resolved" {
		s.writeErr(w, http.StatusBadRequest, "status muss open|acknowledged|resolved sein")
		return
	}
	inc, err := s.store.GetIncident(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if allowed := s.scopeFilter(r); allowed != nil {
		if site, _, _ := s.store.DeviceSiteID(r.Context(), inc.DeviceID); !allowed[site] {
			s.writeErr(w, http.StatusForbidden, "Gerät außerhalb Ihres Bereichs")
			return
		}
	}
	by := ""
	if u := userFrom(r.Context()); u != nil {
		by = u.Username
	}
	if err := s.store.SetIncidentStatus(r.Context(), inc.ID, req.Status, by); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	inc, _ = s.store.GetIncident(r.Context(), inc.ID)
	s.writeJSON(w, http.StatusOK, inc)
}

func (s *Server) authEventFilter(r *http.Request) store.AuthEventFilter {
	q := r.URL.Query()
	days, _ := strconv.Atoi(q.Get("days"))
	if days <= 0 || days > 3650 {
		days = 7
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	return store.AuthEventFilter{Allowed: s.scopeFilter(r), ClientID: q.Get("client"), SiteID: q.Get("site"), Since: time.Now().AddDate(0, 0, -days),
		Kind: q.Get("kind"), IP: q.Get("ip"), User: q.Get("user"), Query: q.Get("q"), Limit: limit}
}

func (s *Server) handleAuthEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.AuthEvents(r.Context(), s.authEventFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleDeviceAuthEvents(w http.ResponseWriter, r *http.Request) {
	f := s.authEventFilter(r)
	f.DeviceID = chi.URLParam(r, "id")
	rows, err := s.store.AuthEvents(r.Context(), f)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

func (s *Server) handleSecuritySummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.store.SecuritySummary(r.Context(), s.scopeFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, sum)
}
