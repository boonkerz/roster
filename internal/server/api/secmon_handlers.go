package api

import (
	"context"
	"fmt"
	"github.com/boonkerz/roster/internal/server/threat"
	"github.com/boonkerz/roster/internal/shared/mitre"
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
	InvalidUserMax   int      `json:"invalid_user_max"`   // Versuche mit unbekanntem Benutzer je IP in 24 h (0 = aus)
	ScanThreshold    int      `json:"scan_threshold"`     // verdächtige Anfragen je IP im Fenster
	AutoBlock        bool     `json:"auto_block"`         // Brute-Force/Scan ohne Richtlinien-Regel sperren
	AutoBlockMinutes int      `json:"auto_block_minutes"` // 0 = dauerhaft
	FleetBlock       bool     `json:"fleet_block"`        // Sperren auf allen überwachten Geräten
	SubnetIPs        int      `json:"subnet_ips"`         // ab so vielen IPs aus einem /24 das Netz sperren (0 = aus)
	BlockPrivate     bool     `json:"block_private"`      // auch private/interne Adressen automatisch sperren (Standard aus)
	// Severity überschreibt je Vorfallsart den Standard-Schweregrad des Katalogs (leer = Standard).
	// "info" unterdrückt die Benachrichtigung, nicht die automatische Reaktion.
	Severity map[string]string `json:"severity"`
	// Bedrohungsdaten: Blocklisten abgleichen, gelistete IPs beim ersten Ereignis flottenweit sperren.
	ThreatEnabled bool     `json:"threat_enabled"`
	ThreatBlock   bool     `json:"threat_block"`
	ThreatFeeds   []string `json:"threat_feeds"`   // leer = alle Feeds
	PortscanPorts int      `json:"portscan_ports"` // verschiedene Zielports (Firewall-Drops) je IP im Fenster
}

func (s *Server) securitySettings(ctx context.Context) securitySettings {
	cfg := securitySettings{BFThreshold: 10, BFWindowMin: 10, NewIPAlert: true, LearnDays: 7, NeverBlock: []string{}, ResponseCooldown: 30,
		InvalidUserMax: 5, ScanThreshold: 20, AutoBlockMinutes: 7 * 24 * 60, PortscanPorts: 10}
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
	geti(store.SettingSecInvalidUserMax, &cfg.InvalidUserMax)
	geti(store.SettingSecScanThreshold, &cfg.ScanThreshold)
	geti(store.SettingSecPortscanPorts, &cfg.PortscanPorts)
	geti(store.SettingSecAutoBlockMinutes, &cfg.AutoBlockMinutes)
	geti(store.SettingSecSubnetIPs, &cfg.SubnetIPs)
	cfg.ThreatEnabled, cfg.ThreatBlock, cfg.ThreatFeeds = true, true, []string{}
	if v, _ := s.store.GetSetting(ctx, store.SettingSecThreatEnabled); v == "0" {
		cfg.ThreatEnabled = false
	}
	if v, _ := s.store.GetSetting(ctx, store.SettingSecThreatBlock); v == "0" {
		cfg.ThreatBlock = false
	}
	if v, _ := s.store.GetSetting(ctx, store.SettingSecThreatFeeds); v != "" {
		for _, id := range strings.Split(v, ",") {
			if _, ok := threat.FeedByID(id); ok {
				cfg.ThreatFeeds = append(cfg.ThreatFeeds, id)
			}
		}
	}
	cfg.Severity = map[string]string{}
	for _, k := range mitre.AllKinds() {
		if v, _ := s.store.GetSetting(ctx, store.SettingSecSeverityPrefix+k); mitre.ValidSeverity(v) {
			cfg.Severity[k] = v
		}
	}
	if v, _ := s.store.GetSetting(ctx, store.SettingSecAutoBlock); v == "1" {
		cfg.AutoBlock = true
	}
	if v, _ := s.store.GetSetting(ctx, store.SettingSecBlockPrivate); v == "1" {
		cfg.BlockPrivate = true
	}
	if v, _ := s.store.GetSetting(ctx, store.SettingSecFleetBlock); v == "1" {
		cfg.FleetBlock = true
	}
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
	if req.BFThreshold < 2 || req.BFThreshold > 1000 || req.BFWindowMin < 1 || req.BFWindowMin > 1440 || req.LearnDays < 0 || req.LearnDays > 365 || req.ResponseCooldown < 0 || req.ResponseCooldown > 10080 ||
		req.InvalidUserMax < 0 || req.InvalidUserMax > 1000 || req.ScanThreshold < 2 || req.ScanThreshold > 10000 || req.PortscanPorts < 2 || req.PortscanPorts > 1000 || req.AutoBlockMinutes < 0 || req.AutoBlockMinutes > 43200 || req.SubnetIPs < 0 || req.SubnetIPs > 254 {
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
		store.SettingSecInvalidUserMax:   strconv.Itoa(req.InvalidUserMax), store.SettingSecScanThreshold: strconv.Itoa(req.ScanThreshold),
		store.SettingSecAutoBlock: b2s(req.AutoBlock), store.SettingSecAutoBlockMinutes: strconv.Itoa(req.AutoBlockMinutes),
		store.SettingSecFleetBlock: b2s(req.FleetBlock), store.SettingSecSubnetIPs: strconv.Itoa(req.SubnetIPs),
		store.SettingSecBlockPrivate: b2s(req.BlockPrivate), store.SettingSecPortscanPorts: strconv.Itoa(req.PortscanPorts),
	}
	var feedIDs []string
	for _, id := range req.ThreatFeeds {
		if _, ok := threat.FeedByID(id); !ok {
			s.writeErr(w, http.StatusBadRequest, "unbekannter Feed: "+id)
			return
		}
		feedIDs = append(feedIDs, id)
	}
	set[store.SettingSecThreatEnabled] = b2s(req.ThreatEnabled)
	set[store.SettingSecThreatBlock] = b2s(req.ThreatBlock)
	set[store.SettingSecThreatFeeds] = strings.Join(feedIDs, ",")
	for k, v := range req.Severity {
		if !mitre.IsKind(k) || (v != "" && !mitre.ValidSeverity(v)) {
			s.writeErr(w, http.StatusBadRequest, "ungültiger Schweregrad für "+k)
			return
		}
	}
	for _, k := range mitre.AllKinds() {
		set[store.SettingSecSeverityPrefix+k] = req.Severity[k] // "" = Standard
	}
	for k, v := range set {
		if err := s.store.SetSetting(r.Context(), k, v); err != nil {
			s.mapStoreErr(w, err)
			return
		}
	}
	// Feeds im Hintergrund neu laden, damit Schalter/Feed-Auswahl sofort wirken.
	go s.RefreshThreat(context.Background(), false)
	s.handleGetSecuritySettings(w, r)
}

// applySeverity wendet den Schweregrad-Override aus den Einstellungen auf einen Vorfall an.
func applySeverity(cfg securitySettings, inc *model.SecurityIncident) {
	if v := cfg.Severity[inc.Kind]; v != "" {
		inc.Severity = v
	}
}

// handleIncidentKinds liefert den Katalog der Vorfallsarten (Standard-Schweregrad, ATT&CK-
// Techniken) – die Oberfläche baut daraus Filter und die Schweregrad-Tabelle.
func (s *Server) handleIncidentKinds(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, mitre.Kinds)
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
	dcfg.InvalidUserMax, dcfg.ScanThreshold = cfg.InvalidUserMax, cfg.ScanThreshold
	dcfg.ThreatAlert = cfg.ThreatEnabled && threat.Active()
	dcfg.PortscanPorts = cfg.PortscanPorts
	now := time.Now()
	// Die gerade eingefügten Ereignisse stecken schon in der Datenbank – herausrechnen,
	// damit sie nicht doppelt zählen: nur ältere als das erste neue Ereignis behalten.
	first := fresh[0].Time
	for _, e := range fresh {
		if e.Time.Before(first) {
			first = e.Time
		}
	}
	older := func(m map[string][]time.Time) map[string][]time.Time {
		for ip, ts := range m {
			var keep []time.Time
			for _, t := range ts {
				if t.Before(first) {
					keep = append(keep, t)
				}
			}
			m[ip] = keep
		}
		return m
	}
	prior, _ := s.store.FailedLoginsSince(ctx, device.ID, now.Add(-time.Hour))
	priorInvalid, _ := s.store.InvalidUserSince(ctx, device.ID, now.Add(-secdetect.InvalidUserWindow))
	priorProbes, _ := s.store.ProbesSince(ctx, device.ID, now.Add(-time.Hour))
	recentBF, _ := s.store.RecentIncidentIPs(ctx, device.ID, "brute_force", now.Add(-time.Hour))
	recentScan, _ := s.store.RecentIncidentIPs(ctx, device.ID, "scan", now.Add(-time.Hour))
	recentThreat, _ := s.store.RecentIncidentIPs(ctx, device.ID, "threat_listed", time.Now().Add(-24*time.Hour))
	recentPortscan, _ := s.store.RecentIncidentIPs(ctx, device.ID, "portscan", now.Add(-time.Hour))
	recentTitles, _ := s.store.RecentIncidentTitles(ctx, device.ID, []string{"state_change", "persistence", "rootkit", "malware"}, now.Add(-24*time.Hour))
	priorDrops, _ := s.store.DropsSince(ctx, device.ID, now.Add(-dcfg.BFWindow))
	for ip, hits := range priorDrops {
		var keep []store.PortHit
		for _, h := range hits {
			if h.Time.Before(first) {
				keep = append(keep, h)
			}
		}
		priorDrops[ip] = keep
	}
	drops := map[string][]secdetect.PortHit{}
	for ip, hits := range priorDrops {
		for _, h := range hits {
			drops[ip] = append(drops[ip], secdetect.PortHit{Time: h.Time, Port: h.Port})
		}
	}
	known, _ := s.store.KnownIPs(ctx, device.ID)
	incidents := secdetect.Detect(secdetect.Input{
		Now: now, Events: fresh, PriorFails: older(prior), PriorInvalid: older(priorInvalid), PriorProbes: older(priorProbes), RecentBF: recentBF, RecentScan: recentScan,
		KnownIP:         func(u, ip string) bool { return known[u+"@"+ip] },
		MonitoringSince: s.store.AuthMonitorSince(ctx, device.ID),
		ThreatLookup:    threat.Lookup,
		RecentThreat:    recentThreat,
		PriorDrops:      drops,
		RecentPortscan:  recentPortscan,
		RecentTitles:    recentTitles,
	}, dcfg)
	_ = s.store.UpsertKnownIPs(ctx, device.ID, fresh)
	var saved []model.SecurityIncident
	for _, inc := range incidents {
		m := model.SecurityIncident{DeviceID: device.ID, TS: inc.Time, Kind: inc.Kind, Severity: inc.Severity, SourceIP: inc.SourceIP,
			Username: inc.User, Title: inc.Title, Detail: inc.Detail, Status: "open"}
		applySeverity(cfg, &m)
		if err := s.store.InsertIncident(ctx, &m); err != nil {
			s.log.Error("vorfall speichern", "err", err)
			continue
		}
		saved = append(saved, m)
	}
	return saved
}

// alertIncidents meldet neue Vorfälle über die Alarmkanäle des Geräts – nach der Active
// Response, damit die Meldung die Reaktion enthält und die Richtlinie „automatisch
// gesperrte Vorfälle nicht melden“ greifen kann. Welche Arten gemeldet werden, steht im
// Bereich „Benachrichtigungen“ der Richtlinie (Standard: alle warning/critical).
func (s *Server) alertIncidents(ctx context.Context, device *model.Device, incidents []model.SecurityIncident) {
	if len(incidents) == 0 {
		return
	}
	setting := s.store.ResolveSecurityAlerts(ctx, device.ID)
	for _, inc := range incidents {
		if (inc.Severity != "warning" && inc.Severity != "critical") || !setting.Kinds[inc.Kind] {
			continue
		}
		cur, err := s.store.GetIncident(ctx, inc.ID)
		if err != nil {
			cur = &inc
		}
		blocked := strings.HasPrefix(cur.Response, "block_ip ") && !strings.HasPrefix(cur.ResponseStatus, "skipped") && !strings.HasPrefix(cur.ResponseStatus, "failed")
		if setting.SkipBlocked && blocked {
			continue
		}
		reaction := ""
		if cur.Response != "" {
			reaction = fmt.Sprintf("\n\nReaktion: %s (%s)", cur.Response, orDash(cur.ResponseStatus))
		}
		if inc.Kind == "fim_change" {
			s.dispatchDeviceAlert(ctx, device, inc.Severity,
				fmt.Sprintf("[Roster] %s auf %s", inc.Title, device.Hostname),
				fmt.Sprintf("Die Datei-Integritätsüberwachung hat auf %q Änderungen erkannt:\n\n%s%s", device.Hostname, truncateStr(inc.Detail, 4000), reaction))
			continue
		}
		s.dispatchDeviceAlert(ctx, device, inc.Severity,
			fmt.Sprintf("[Roster] Sicherheitsvorfall auf %s: %s", device.Hostname, inc.Title),
			fmt.Sprintf("Gerät %q, %s\n\n%s\n\nArt: %s · Quelle: %s · Benutzer: %s%s", device.Hostname, inc.TS.Format("02.01.2006 15:04:05"), inc.Detail, inc.Kind, orDash(inc.SourceIP), orDash(inc.Username), reaction))
	}
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
		Status: q.Get("status"), Kind: q.Get("kind"), Severity: q.Get("severity"), Query: q.Get("q"), Country: q.Get("country"),
		Technique: strings.ToUpper(strings.TrimSpace(q.Get("technique"))), Threat: q.Get("threat") == "1", Limit: limit}
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
		Kind: q.Get("kind"), IP: q.Get("ip"), User: q.Get("user"), Query: q.Get("q"), Threat: q.Get("threat") == "1", Limit: limit}
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

// securitySummary liefert die Sicherheitsübersicht aus einem 60-s-Cache je Sichtbarkeits-
// bereich: Dashboard und Sicherheitsseite fragen sie alle 15–30 s ab, und die Zählung der
// Sperren/Angriffsquellen über Wochen hinweg ist bei großen Tabellen teuer.
func (s *Server) securitySummary(ctx context.Context, allowed map[string]bool) (model.SecuritySummary, error) {
	key := scopeKey(allowed)
	s.secSumMu.Lock()
	if c, ok := s.secSumCache[key]; ok && time.Since(c.at) < time.Minute {
		s.secSumMu.Unlock()
		return c.sum, nil
	}
	s.secSumMu.Unlock()
	sum, err := s.store.SecuritySummary(ctx, allowed)
	if err != nil {
		return sum, err
	}
	s.secSumMu.Lock()
	if s.secSumCache == nil {
		s.secSumCache = map[string]secSumEntry{}
	}
	s.secSumCache[key] = secSumEntry{sum: sum, at: time.Now()}
	s.secSumMu.Unlock()
	return sum, nil
}

type secSumEntry struct {
	sum model.SecuritySummary
	at  time.Time
}

func (s *Server) handleSecuritySummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.securitySummary(r.Context(), s.scopeFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, sum)
}
