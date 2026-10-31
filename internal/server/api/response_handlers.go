package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
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
	responseTriggers  = map[string]bool{"brute_force": true, "brute_force_success": true, "new_source_ip": true, "account_change": true, "lockout": true, "fim_change": true}
	responseActions   = map[string]bool{"block_ip": true, "lock_user": true, "run_script": true}
	ipTriggers        = map[string]bool{"brute_force": true, "brute_force_success": true, "new_source_ip": true}
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
			return errors.New("IP-Sperre ist nur bei Auslösern mit Quell-IP möglich (Brute-Force, Anmeldung nach Brute-Force, neue Quell-IP)")
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

// runResponses führt die konfigurierten Reaktionen für neue Vorfälle aus.
func (s *Server) runResponses(ctx context.Context, device *model.Device, incidents []model.SecurityIncident) {
	if len(incidents) == 0 {
		return
	}
	rules, err := s.store.ResponsesForDevice(ctx, device.ID)
	if err != nil || len(rules) == 0 {
		return
	}
	cfg := s.securitySettings(ctx)
	cooldown := time.Duration(cfg.ResponseCooldown) * time.Minute
	platform := platformOf(device.OS)
	for _, inc := range incidents {
		for _, rule := range rules {
			if rule.Trigger != inc.Kind || !platformMatches(rule.Platforms, platform) {
				continue
			}
			s.executeResponse(ctx, device, inc, rule, cfg.NeverBlock, cooldown, "system")
			break // eine Reaktion je Vorfall und Regel-Trigger reicht … pro Aktion weiter unten unterschieden
		}
	}
}

// executeResponse reiht die Aktion ein, protokolliert sie und vermerkt sie am Vorfall.
func (s *Server) executeResponse(ctx context.Context, device *model.Device, inc model.SecurityIncident, rule model.PolicyResponse, neverBlock []string, cooldown time.Duration, by string) {
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
		if b, _ := s.store.ActiveIPBlock(ctx, device.ID, inc.SourceIP); b != nil {
			_ = s.store.SetIncidentResponse(ctx, inc.ID, "block_ip "+inc.SourceIP, "already blocked", b.CommandID)
			return
		}
		payloadKey = "block_ip " + inc.SourceIP
		label = fmt.Sprintf("Active Response: IP %s sperren", inc.SourceIP)
		payload["ip"], payload["minutes"] = inc.SourceIP, rule.Minutes
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
		var until *time.Time
		if rule.Minutes > 0 {
			t := time.Now().Add(time.Duration(rule.Minutes) * time.Minute)
			until = &t
		}
		_ = s.store.InsertIPBlock(ctx, &model.IPBlock{DeviceID: device.ID, IP: inc.SourceIP, Reason: inc.Title, IncidentID: inc.ID, Until: until, CommandID: cmdID, CreatedBy: by})
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
}

func (s *Server) handleBlockIP(w http.ResponseWriter, r *http.Request) {
	var req manualBlockRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	ip := net.ParseIP(strings.TrimSpace(req.IP))
	if ip == nil {
		s.writeErr(w, http.StatusBadRequest, "ungültige IP")
		return
	}
	if req.Minutes < 0 || req.Minutes > 43200 {
		s.writeErr(w, http.StatusBadRequest, "minutes 0–43200")
		return
	}
	cfg := s.securitySettings(r.Context())
	if neverBlocked(ip.String(), cfg.NeverBlock) {
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
	cmdID, err := s.queueCommand(r.Context(), device.ID, "block_ip", fmt.Sprintf("IP %s sperren", ip), map[string]any{"ip": ip.String(), "minutes": req.Minutes})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	var until *time.Time
	if req.Minutes > 0 {
		t := time.Now().Add(time.Duration(req.Minutes) * time.Minute)
		until = &t
	}
	b := &model.IPBlock{DeviceID: device.ID, IP: ip.String(), Reason: firstNonEmptyStr(req.Reason, "manuell"), Until: until, CommandID: cmdID, CreatedBy: by}
	if err := s.store.InsertIPBlock(r.Context(), b); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, b)
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
	ip := net.ParseIP(strings.TrimSpace(req.IP))
	if ip == nil {
		s.writeErr(w, http.StatusBadRequest, "ungültige IP")
		return
	}
	deviceID := chi.URLParam(r, "id")
	cmdID, err := s.queueCommand(r.Context(), deviceID, "unblock_ip", fmt.Sprintf("IP %s entsperren", ip), map[string]any{"ip": ip.String()})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	blocks, _ := s.store.IPBlocks(r.Context(), nil, deviceID, 0)
	for _, b := range blocks {
		if b.IP == ip.String() && (b.Status == "active" || b.Status == "pending" || b.Status == "expired") {
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
	rows, err := s.store.IPBlocks(r.Context(), s.scopeFilter(r), "", 0)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
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
