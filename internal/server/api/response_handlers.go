package api

import (
	"context"
	"errors"
	"fmt"
	"net"
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

// Trigger und Aktionen der Active Response.
var (
	responseTriggers  = map[string]bool{"brute_force": true, "brute_force_success": true, "new_source_ip": true, "account_change": true, "lockout": true, "fim_change": true, "scan": true}
	responseActions   = map[string]bool{"block_ip": true, "lock_user": true, "run_script": true}
	ipTriggers        = map[string]bool{"brute_force": true, "brute_force_success": true, "new_source_ip": true, "scan": true}
	userTriggers      = map[string]bool{"brute_force_success": true, "account_change": true, "lockout": true, "new_source_ip": true}
	protectedAccounts = map[string]bool{"root": true, "administrator": true, "system": true}
)

type responseRequest struct {
	Trigger   string   `json:"trigger"`
	Action    string   `json:"action"`
	Minutes   *int     `json:"minutes"`
	ScriptID  string   `json:"script_id"`
	Platforms []string `json:"platforms"`
	Enabled   *bool    `json:"enabled"`
}

func (req responseRequest) apply(r *model.PolicyResponse) error {
	if !responseTriggers[req.Trigger] {
		return errors.New("unbekannter Auslöser")
	}
	if !responseActions[req.Action] {
		return errors.New("unbekannte Aktion")
	}
	switch req.Action {
	case "block_ip":
		if !ipTriggers[req.Trigger] {
			return errors.New("IP-Sperre ist nur bei Auslösern mit Quell-IP möglich (Brute-Force, Anmeldung nach Brute-Force, neue Quell-IP, Angriffsmuster)")
		}
	case "lock_user":
		if !userTriggers[req.Trigger] {
			return errors.New("Kontosperre ist nur bei Auslösern mit Benutzer möglich")
		}
	case "run_script":
		if strings.TrimSpace(req.ScriptID) == "" {
			return errors.New("script_id fehlt")
		}
	}
	r.Trigger, r.Action = req.Trigger, req.Action
	r.Minutes = 60
	if req.Minutes != nil {
		if *req.Minutes < 0 || *req.Minutes > 43200 {
			return errors.New("minutes muss zwischen 0 (dauerhaft) und 43200 liegen")
		}
		r.Minutes = *req.Minutes
	}
	r.ScriptID = ""
	if req.Action == "run_script" {
		r.ScriptID = strings.TrimSpace(req.ScriptID)
	}
	r.Platforms = []string{}
	for _, p := range req.Platforms {
		switch p = strings.ToLower(strings.TrimSpace(p)); p {
		case "windows", "linux", "darwin":
			r.Platforms = append(r.Platforms, p)
		case "":
		default:
			return fmt.Errorf("unbekannte Plattform %q", p)
		}
	}
	r.Enabled = true
	if req.Enabled != nil {
		r.Enabled = *req.Enabled
	}
	return nil
}

func (s *Server) handleAddResponse(w http.ResponseWriter, r *http.Request) {
	var req responseRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	rule := model.PolicyResponse{PolicyID: chi.URLParam(r, "id")}
	if err := req.apply(&rule); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveResponse(r.Context(), &rule); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleUpdateResponse(w http.ResponseWriter, r *http.Request) {
	var req responseRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	rule, err := s.store.GetResponse(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if err := req.apply(rule); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveResponse(r.Context(), rule); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleDeleteResponse(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteResponse(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Ausführung ---

// platformOf ordnet den OS-String eines Geräts einer Plattform zu.
func platformOf(os string) string {
	l := strings.ToLower(os)
	switch {
	case strings.Contains(l, "windows"):
		return "windows"
	case l == "darwin" || strings.Contains(l, "mac"):
		return "darwin"
	}
	return "linux"
}

func platformMatches(platforms []string, platform string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		if p == platform {
			return true
		}
	}
	return false
}

// isPrivateIP erkennt private/interne Adressen (RFC 1918, ULA, Link-Local, CGNAT) – z. B.
// Docker-Netze oder Reverse-Proxys, deren Sperre den eigenen Dienst lahmlegen würde.
func isPrivateIP(ip string) bool {
	p := net.ParseIP(strings.Split(ip, "/")[0])
	if p == nil {
		return false
	}
	if p.IsPrivate() || p.IsLinkLocalUnicast() || p.IsLinkLocalMulticast() || p.IsLoopback() {
		return true
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(p)
}

// neverBlocked prüft eine IP gegen die Nie-sperren-Liste (Loopback immer).
func neverBlocked(ip string, cidrs []string) bool {
	p := net.ParseIP(ip)
	if p == nil || p.IsLoopback() || p.IsUnspecified() {
		return true
	}
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil && n.Contains(p) {
			return true
		}
		if net.ParseIP(c) != nil && net.ParseIP(c).Equal(p) {
			return true
		}
	}
	return false
}

// runResponses führt die konfigurierten Reaktionen für neue Vorfälle aus: zuerst die
// Regeln der zugewiesenen Richtlinien; hat keine Regel den Auslöser abgedeckt, greift die
// globale automatische Sperre aus den Einstellungen (Brute-Force, Anmeldung nach
// Brute-Force, Angriffsmuster).
func (s *Server) runResponses(ctx context.Context, device *model.Device, incidents []model.SecurityIncident) {
	if len(incidents) == 0 {
		return
	}
	rules, err := s.store.ResponsesForDevice(ctx, device.ID)
	if err != nil {
		return
	}
	cfg := s.securitySettings(ctx)
	platform := platformOf(device.OS)
	for _, inc := range incidents {
		handled := false
		for _, rule := range rules {
			if rule.Trigger != inc.Kind || !platformMatches(rule.Platforms, platform) {
				continue
			}
			s.executeResponse(ctx, device, inc, rule, cfg, "system")
			handled = true
			break // eine Reaktion je Vorfall und Regel-Trigger reicht … pro Aktion weiter unten unterschieden
		}
		if !handled && cfg.AutoBlock && autoBlockTriggers[inc.Kind] && inc.SourceIP != "" {
			s.executeResponse(ctx, device, inc, model.PolicyResponse{Trigger: inc.Kind, Action: "block_ip", Minutes: cfg.AutoBlockMinutes}, cfg, "system")
		}
	}
}

// autoBlockTriggers sind die Vorfallsarten, die die globale automatische Sperre auslöst.
var autoBlockTriggers = map[string]bool{"brute_force": true, "brute_force_success": true, "scan": true}

// subnet24 liefert das /24-Netz einer IPv4-Adresse ("" bei IPv6/ungültig).
func subnet24(ip string) string {
	p := net.ParseIP(ip)
	if p == nil || p.To4() == nil {
		return ""
	}
	return p.Mask(net.CIDRMask(24, 32)).String() + "/24"
}

// blockTarget entscheidet, ob statt der einzelnen IP das ganze /24-Netz gesperrt wird: wenn
// aus demselben Netz in den letzten 24 h mindestens cfg.SubnetIPs verschiedene Quell-IPs
// aufgefallen sind und keine Nie-sperren-Adresse im Netz liegt.
func (s *Server) blockTarget(ctx context.Context, deviceID, ip string, cfg securitySettings) string {
	if cfg.SubnetIPs <= 0 {
		return ip
	}
	cidr := subnet24(ip)
	if cidr == "" {
		return ip
	}
	n, err := s.store.FailSourcesInSubnet(ctx, deviceID, ip, time.Now().Add(-24*time.Hour))
	if err != nil || n < cfg.SubnetIPs {
		return ip
	}
	_, netw, _ := net.ParseCIDR(cidr)
	for _, c := range cfg.NeverBlock {
		if p := net.ParseIP(strings.Split(c, "/")[0]); p != nil && netw.Contains(p) {
			return ip
		}
	}
	return cidr
}

// blockOnDevice reiht eine IP-/Netz-Sperre auf einem Gerät ein und pflegt ip_blocks: eine
// bestehende Sperre derselben Adresse wird erneuert (kein Duplikat), sonst neu angelegt.
// Liefert die Befehls-ID, den Eintrag und ob er neu ist.
func (s *Server) blockOnDevice(ctx context.Context, deviceID, target string, minutes int, reason, incidentID, by string) (*model.IPBlock, bool, error) {
	label := fmt.Sprintf("IP %s sperren", target)
	if by == "system" {
		label = "Active Response: " + label
	}
	cmdID, err := s.queueCommand(ctx, deviceID, "block_ip", label, map[string]any{"ip": target, "minutes": minutes})
	if err != nil {
		return nil, false, err
	}
	var until *time.Time
	if minutes > 0 {
		t := time.Now().Add(time.Duration(minutes) * time.Minute)
		until = &t
	}
	if existing, _ := s.store.ActiveIPBlock(ctx, deviceID, target); existing != nil {
		if err := s.store.RenewIPBlock(ctx, existing.ID, until, reason, cmdID, by); err != nil {
			return nil, false, err
		}
		existing.Until, existing.Reason, existing.CommandID, existing.CreatedBy, existing.Status = until, reason, cmdID, by, "pending"
		return existing, false, nil
	}
	b := &model.IPBlock{DeviceID: deviceID, IP: target, Reason: reason, IncidentID: incidentID, Until: until, CommandID: cmdID, CreatedBy: by}
	if err := s.store.InsertIPBlock(ctx, b); err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// blockFleet sperrt target zusätzlich auf allen anderen überwachten Geräten (Anmeldungs-
// Überwachung oder Log-Quelle aktiv) – in einer Transaktion; bereits gesperrte Adressen
// werden übersprungen. Liefert die Zahl der neu eingereihten Sperren.
func (s *Server) blockFleet(ctx context.Context, originID, target string, minutes int, reason, incidentID, by string) int {
	devices, err := s.store.MonitoredDeviceIDs(ctx)
	if err != nil {
		return 0
	}
	ids := make([]string, 0, len(devices))
	for _, d := range devices {
		if d.ID == originID || platformOf(d.OS) == "" {
			continue
		}
		ids = append(ids, d.ID)
	}
	var until *time.Time
	if minutes > 0 {
		t := time.Now().Add(time.Duration(minutes) * time.Minute)
		until = &t
	}
	label := fmt.Sprintf("IP %s sperren", target)
	if by == "system" {
		label = "Active Response: " + label
	}
	queued, err := s.store.BlockIPOnDevices(ctx, ids, target, minutes, until, reason, incidentID, by, label)
	if err != nil {
		s.log.Error("flottensperre", "ip", target, "err", err)
		return 0
	}
	for _, id := range queued {
		s.term.requestWake(id, shared.WaitResponse{Type: "checkin"})
	}
	if len(queued) > 0 {
		_ = s.store.InsertAudit(ctx, model.AuditEntry{TS: time.Now(), Username: by, Action: fmt.Sprintf("IP %s auf %d weiteren Geräten gesperrt (Flotte)", target, len(queued)), Method: "AUTO", Path: "/security/response", Status: 201})
	}
	return len(queued)
}

// responseJob ist ein Auftrag für den Reaktions-Worker.
type responseJob struct {
	device    *model.Device
	incidents []model.SecurityIncident
}

// queueResponses übergibt Vorfälle an den Hintergrund-Worker, damit Sperren (insbesondere
// die Flottensperre auf vielen Geräten) den Checkin nicht aufhalten. Der Worker arbeitet
// seriell – so laufen Reaktionen nie parallel gegen die Datenbank und die Prüfung „schon
// gesperrt?“ bleibt verlässlich. Ist die Warteschlange voll, bleibt der Vorfall ohne
// automatische Reaktion (er ist gespeichert und gemeldet).
func (s *Server) queueResponses(device *model.Device, incidents []model.SecurityIncident) {
	if len(incidents) == 0 {
		return
	}
	select {
	case s.responses <- responseJob{device: device, incidents: incidents}:
	default:
		s.log.Warn("reaktions-warteschlange voll, vorfälle ohne automatische reaktion", "device", device.Hostname, "n", len(incidents))
	}
}

// responseWorker arbeitet die Reaktionsaufträge nacheinander ab.
func (s *Server) responseWorker() {
	for job := range s.responses {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		s.runResponses(ctx, job.device, job.incidents)
		cancel()
	}
}

// executeResponse reiht die Aktion ein, protokolliert sie und vermerkt sie am Vorfall.
func (s *Server) executeResponse(ctx context.Context, device *model.Device, inc model.SecurityIncident, rule model.PolicyResponse, cfg securitySettings, by string) {
	neverBlock := cfg.NeverBlock
	cooldown := time.Duration(cfg.ResponseCooldown) * time.Minute
	var label, payloadKey string
	payload := map[string]any{}
	switch rule.Action {
	case "block_ip":
		if inc.SourceIP == "" {
			return
		}
		if neverBlocked(inc.SourceIP, neverBlock) {
			_ = s.store.SetIncidentResponse(ctx, inc.ID, "block_ip "+inc.SourceIP, "skipped: never-block", "")
			return
		}
		if !cfg.BlockPrivate && isPrivateIP(inc.SourceIP) {
			// Interne Adresse (Docker-Netz, Reverse-Proxy, LAN): automatisch nie sperren – die
			// Ereignisse stammen meist von einem Proxy, der die echte Quell-IP verdeckt.
			_ = s.store.SetIncidentResponse(ctx, inc.ID, "block_ip "+inc.SourceIP, "skipped: private address", "")
			return
		}
		target := s.blockTarget(ctx, device.ID, inc.SourceIP, cfg)
		payloadKey = "block_ip " + target
		if b, _ := s.store.ActiveIPBlock(ctx, device.ID, target); b != nil {
			_ = s.store.SetIncidentResponse(ctx, inc.ID, payloadKey, "already blocked", b.CommandID)
			if cfg.FleetBlock {
				s.blockFleet(ctx, device.ID, target, rule.Minutes, inc.Title, inc.ID, by)
			}
			return
		}
		if target != inc.SourceIP {
			if b, _ := s.store.ActiveIPBlock(ctx, device.ID, inc.SourceIP); b != nil {
				_ = s.store.SetIPBlockStatus(ctx, b.ID, "released", "durch Netzbereichs-Sperre "+target+" ersetzt")
			}
		}
		label = fmt.Sprintf("Active Response: IP %s sperren", target)
		payload["ip"], payload["minutes"] = target, rule.Minutes
	case "lock_user":
		if inc.Username == "" || protectedAccounts[strings.ToLower(inc.Username)] {
			return
		}
		payloadKey = "lock_user " + inc.Username
		label = fmt.Sprintf("Active Response: Konto %s sperren", inc.Username)
		payload["user"] = inc.Username
	case "run_script":
		scripts, err := s.store.ListScripts(ctx)
		if err != nil {
			return
		}
		var sc *model.Script
		for i := range scripts {
			if scripts[i].ID == rule.ScriptID {
				sc = &scripts[i]
			}
		}
		if sc == nil {
			return
		}
		payloadKey = "run_script " + sc.Name
		label = "Active Response: " + sc.Name
		content := sc.Content
		if agent, client, site, ferr := s.store.FieldMapsForDevice(ctx, device.ID); ferr == nil {
			content = store.SubstituteFields(content, agent, client, site)
		}
		payload["shell"], payload["script"], payload["platforms"] = sc.Shell, content, sc.Platforms
		payload["env"] = map[string]string{"ROSTER_TRIGGER": inc.Kind, "ROSTER_SOURCE_IP": inc.SourceIP, "ROSTER_USER": inc.Username,
			"ROSTER_DEVICE": device.Hostname, "ROSTER_INCIDENT": inc.ID, "ROSTER_TITLE": inc.Title}
	default:
		return
	}
	if cooldown > 0 && by == "system" {
		if recent, _ := s.store.RecentResponse(ctx, device.ID, payloadKey, time.Now().Add(-cooldown)); recent {
			_ = s.store.SetIncidentResponse(ctx, inc.ID, payloadKey, "skipped: cooldown", "")
			return
		}
	}
	cmdType := rule.Action
	cmdID, err := s.queueCommand(ctx, device.ID, cmdType, label, payload)
	if err != nil {
		_ = s.store.SetIncidentResponse(ctx, inc.ID, payloadKey, "failed: "+err.Error(), "")
		return
	}
	_ = s.store.SetIncidentResponse(ctx, inc.ID, payloadKey, "queued", cmdID)
	if rule.Action == "block_ip" {
		target, _ := payload["ip"].(string)
		var until *time.Time
		if rule.Minutes > 0 {
			t := time.Now().Add(time.Duration(rule.Minutes) * time.Minute)
			until = &t
		}
		_ = s.store.InsertIPBlock(ctx, &model.IPBlock{DeviceID: device.ID, IP: target, Reason: inc.Title, IncidentID: inc.ID, Until: until, CommandID: cmdID, CreatedBy: by})
		if cfg.FleetBlock {
			if n := s.blockFleet(ctx, device.ID, target, rule.Minutes, inc.Title, inc.ID, by); n > 0 {
				_ = s.store.SetIncidentResponse(ctx, inc.ID, payloadKey, fmt.Sprintf("queued (+%d Geräte)", n), cmdID)
			}
		}
	}
	_ = s.store.InsertAudit(ctx, model.AuditEntry{TS: time.Now(), Username: by, Action: label + " (" + device.Hostname + ")", Method: "AUTO", Path: "/security/response", Status: 201})
	s.log.Info("active response", "device", device.Hostname, "action", payloadKey, "incident", inc.Kind)
}

// completeResponses wertet Befehlsergebnisse der Reaktionen aus (Sperrstatus, Vorfall).
func (s *Server) completeResponses(ctx context.Context, results []shared.CommandResult) {
	for _, res := range results {
		if b, _ := s.store.IPBlockByCommand(ctx, res.CommandID); b != nil {
			status := "active"
			if res.ExitCode != 0 {
				status = "failed"
			}
			_ = s.store.SetIPBlockStatus(ctx, b.ID, status, strings.TrimSpace(res.Output))
		}
		if inc, _ := s.store.IncidentByResponseCommand(ctx, res.CommandID); inc != nil {
			status := "done"
			if res.ExitCode != 0 {
				status = "failed: " + truncateStr(strings.TrimSpace(res.Output), 200)
			}
			_ = s.store.SetIncidentResponse(ctx, inc.ID, inc.Response, status, res.CommandID)
		}
	}
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// --- Manuelle Aktionen & Sperrliste ---

type manualBlockRequest struct {
	IP      string `json:"ip"`
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason"`
	Fleet   bool   `json:"fleet"` // zusätzlich auf allen überwachten Geräten sperren
}

// parseBlockTarget akzeptiert eine IP oder ein Netz in CIDR-Schreibweise (max. /16 bzw. /48).
func parseBlockTarget(s string) (string, error) {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(s); ip != nil {
		return ip.String(), nil
	}
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		return "", errors.New("ungültige IP oder CIDR")
	}
	ones, bits := n.Mask.Size()
	if (bits == 32 && ones < 16) || (bits == 128 && ones < 48) {
		return "", errors.New("Netz zu groß (mindestens /16 bzw. /48)")
	}
	_ = ip
	return n.String(), nil
}

func (s *Server) handleBlockIP(w http.ResponseWriter, r *http.Request) {
	var req manualBlockRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	target, err := parseBlockTarget(req.IP)
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Minutes < 0 || req.Minutes > 43200 {
		s.writeErr(w, http.StatusBadRequest, "minutes 0–43200")
		return
	}
	cfg := s.securitySettings(r.Context())
	if neverBlocked(strings.Split(target, "/")[0], cfg.NeverBlock) {
		s.writeErr(w, http.StatusBadRequest, "IP steht auf der Nie-sperren-Liste")
		return
	}
	device, err := s.store.GetDevice(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	by := "system"
	if u := userFrom(r.Context()); u != nil {
		by = u.Username
	}
	reason := firstNonEmptyStr(req.Reason, "manuell")
	b, created, err := s.blockOnDevice(r.Context(), device.ID, target, req.Minutes, reason, "", by)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if req.Fleet {
		s.blockFleet(r.Context(), device.ID, target, req.Minutes, reason, "", by)
	}
	if created {
		s.writeJSON(w, http.StatusCreated, b)
		return
	}
	s.writeJSON(w, http.StatusOK, b)
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (s *Server) handleUnblockIP(w http.ResponseWriter, r *http.Request) {
	var req manualBlockRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	target, err := parseBlockTarget(req.IP)
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	deviceID := chi.URLParam(r, "id")
	cmdID, err := s.queueCommand(r.Context(), deviceID, "unblock_ip", fmt.Sprintf("IP %s entsperren", target), map[string]any{"ip": target})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	blocks, _ := s.store.IPBlocks(r.Context(), nil, deviceID, 0)
	for _, b := range blocks {
		if b.IP == target && (b.Status == "active" || b.Status == "pending" || b.Status == "expired") {
			_ = s.store.SetIPBlockStatus(r.Context(), b.ID, "released", "aufgehoben")
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"command_id": cmdID})
}

type manualUserRequest struct {
	User string `json:"user"`
}

func (s *Server) handleLockUser(w http.ResponseWriter, r *http.Request) {
	s.userAction(w, r, "lock_user", "Konto %s sperren")
}

func (s *Server) handleUnlockUser(w http.ResponseWriter, r *http.Request) {
	s.userAction(w, r, "unlock_user", "Konto %s entsperren")
}

func (s *Server) userAction(w http.ResponseWriter, r *http.Request, typ, labelFmt string) {
	var req manualUserRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	user := strings.TrimSpace(req.User)
	if user == "" || strings.ContainsAny(user, " \t\n;&|$`\"'\\/") {
		s.writeErr(w, http.StatusBadRequest, "ungültiger Benutzername")
		return
	}
	if typ == "lock_user" && protectedAccounts[strings.ToLower(user)] {
		s.writeErr(w, http.StatusBadRequest, "dieses Konto wird nicht gesperrt")
		return
	}
	s.queueDeviceCmd(w, r, typ, fmt.Sprintf(labelFmt, user), map[string]any{"user": user})
}

func (s *Server) handleIPBlocks(w http.ResponseWriter, r *http.Request) {
	qv := r.URL.Query()
	limit, _ := strconv.Atoi(qv.Get("limit"))
	rows, err := s.store.IPBlocksFiltered(r.Context(), store.IPBlockFilter{Allowed: s.scopeFilter(r), DeviceID: qv.Get("device"), Query: qv.Get("q"), Status: qv.Get("status"), Limit: limit})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleUnblockIPFleet hebt eine Sperre auf allen Geräten im Scope auf, auf denen sie
// wirksam oder eingereiht ist (ein Klick statt je Gerät).
func (s *Server) handleUnblockIPFleet(w http.ResponseWriter, r *http.Request) {
	var req manualBlockRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	target, err := parseBlockTarget(req.IP)
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := s.store.IPBlocksFiltered(r.Context(), store.IPBlockFilter{Allowed: s.scopeFilter(r), Query: target, Status: "active", Limit: 5000})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	done := map[string]bool{}
	n := 0
	for _, b := range rows {
		if b.IP != target || done[b.DeviceID] {
			continue
		}
		done[b.DeviceID] = true
		if _, err := s.queueCommand(r.Context(), b.DeviceID, "unblock_ip", fmt.Sprintf("IP %s entsperren", target), map[string]any{"ip": target}); err != nil {
			continue
		}
		_ = s.store.SetIPBlockStatus(r.Context(), b.ID, "released", "aufgehoben (alle Geräte)")
		n++
	}
	if u := userFrom(r.Context()); u != nil {
		_ = s.store.InsertAudit(r.Context(), model.AuditEntry{Username: u.Username, Method: "POST", Action: fmt.Sprintf("IP %s auf %d Geräten freigegeben", target, n)})
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"devices": n})
}

func (s *Server) handleDeviceIPBlocks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.IPBlocks(r.Context(), nil, chi.URLParam(r, "id"), 0)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

var _ = secdetect.Private
