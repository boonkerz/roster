package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/server/store"
)

// --- Skripte ---

func (s *Server) handleListScripts(w http.ResponseWriter, r *http.Request) {
	scripts, err := s.store.ListScripts(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, scripts)
}

type scriptRequest struct {
	Name      string   `json:"name"`
	Shell     string   `json:"shell"`
	Platforms []string `json:"platforms"`
	Content   string   `json:"content"`
	CheckOnly bool     `json:"check_only"`
}

func (s *Server) handleCreateScript(w http.ResponseWriter, r *http.Request) {
	var req scriptRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || (req.Shell != "powershell" && req.Shell != "shell") {
		s.writeErr(w, http.StatusBadRequest, "name und shell (powershell|shell) erforderlich")
		return
	}
	sc := &model.Script{ID: store.NewID(), Name: req.Name, Shell: req.Shell, Platforms: req.Platforms, Content: req.Content, CheckOnly: req.CheckOnly}
	if err := s.store.CreateScript(r.Context(), sc); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, sc)
}

func (s *Server) handleUpdateScript(w http.ResponseWriter, r *http.Request) {
	var req scriptRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	sc := &model.Script{ID: chi.URLParam(r, "id"), Name: req.Name, Shell: req.Shell, Platforms: req.Platforms, Content: req.Content, CheckOnly: req.CheckOnly}
	if err := s.store.UpdateScript(r.Context(), sc); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, sc)
}

func (s *Server) handleDeleteScript(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteScript(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

// --- Policies ---

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := s.store.ListPolicies(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, policies)
}

type policyRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req policyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		s.writeErr(w, http.StatusBadRequest, "name erforderlich")
		return
	}
	p := &model.Policy{ID: store.NewID(), Name: req.Name, Description: req.Description}
	if err := s.store.CreatePolicy(r.Context(), p); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	var req policyRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	p := &model.Policy{ID: chi.URLParam(r, "id"), Name: req.Name, Description: req.Description}
	if err := s.store.UpdatePolicy(r.Context(), p); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePolicy(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

type checkRequest struct {
	Name                string         `json:"name"`
	Type                string         `json:"type"`
	Config              map[string]any `json:"config"`
	ScriptID            *string        `json:"script_id"`
	Severity            string         `json:"severity"`
	Frequency           string         `json:"frequency"`
	RemediationScriptID *string        `json:"remediation_script_id"`
}

func (s *Server) handleAddCheck(w http.ResponseWriter, r *http.Request) {
	var req checkRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	c := &model.PolicyCheck{
		ID: store.NewID(), PolicyID: chi.URLParam(r, "id"),
		Name: req.Name, Type: req.Type, Config: req.Config, ScriptID: req.ScriptID, Severity: req.Severity,
		Frequency: req.Frequency, RemediationScriptID: req.RemediationScriptID,
	}
	if err := s.store.AddCheck(r.Context(), c); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleDeleteCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCheck(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

type taskRequest struct {
	Name            string  `json:"name"`
	ScriptID        *string `json:"script_id"`
	IntervalMinutes int     `json:"interval_minutes"`
	ScheduleType    string  `json:"schedule_type"`
	DailyTime       string  `json:"daily_time"`
	Weekdays        string  `json:"weekdays"`
	Frequency       string  `json:"frequency"`
	CollectFields   bool    `json:"collect_fields"`
}

func (s *Server) handleAddTask(w http.ResponseWriter, r *http.Request) {
	var req taskRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.ScriptID == nil {
		s.writeErr(w, http.StatusBadRequest, "script_id erforderlich")
		return
	}
	t := &model.PolicyTask{
		ID: store.NewID(), PolicyID: chi.URLParam(r, "id"),
		Name: req.Name, ScriptID: req.ScriptID, IntervalMinutes: req.IntervalMinutes,
		ScheduleType: req.ScheduleType, DailyTime: req.DailyTime, Weekdays: req.Weekdays,
		Frequency: req.Frequency, CollectFields: req.CollectFields,
	}
	if err := s.store.AddTask(r.Context(), t); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, t)
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTask(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

type assignRequest struct {
	TargetType string `json:"target_type"` // client | site | device
	TargetID   string `json:"target_id"`
}

func (s *Server) handleAddAssignment(w http.ResponseWriter, r *http.Request) {
	var req assignRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	switch req.TargetType {
	case "client", "site", "device":
	default:
		s.writeErr(w, http.StatusBadRequest, "target_type muss client|site|device sein")
		return
	}
	a := &model.Assignment{
		ID: store.NewID(), PolicyID: chi.URLParam(r, "id"),
		TargetType: req.TargetType, TargetID: req.TargetID,
	}
	if err := s.store.AddAssignment(r.Context(), a); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, a)
}

func (s *Server) handleDeleteAssignment(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAssignment(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "entfernt"})
}

// --- Ad-hoc-Ausführung ---

type runRequest struct {
	ScriptID string `json:"script_id"`
}

// handleRunScript stellt einen Ad-hoc-Skriptbefehl für ein Gerät in die Queue.
func (s *Server) handleRunScript(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	scripts, err := s.store.ListScripts(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	var found *model.Script
	for i := range scripts {
		if scripts[i].ID == req.ScriptID {
			found = &scripts[i]
			break
		}
	}
	if found == nil {
		s.writeErr(w, http.StatusBadRequest, "skript nicht gefunden")
		return
	}
	deviceID := chi.URLParam(r, "id")
	content := found.Content
	if agent, client, site, ferr := s.store.FieldMapsForDevice(r.Context(), deviceID); ferr == nil {
		content = store.SubstituteFields(content, agent, client, site)
	}
	payload := map[string]any{"shell": found.Shell, "script": content, "platforms": found.Platforms}
	id, err := s.queueCommand(r.Context(), deviceID, "run_script", found.Name, payload)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id, "status": "eingereiht"})
}

// handleScanUpdates reiht einen Ad-hoc-Update-Scan für ein Gerät ein.
func (s *Server) handleScanUpdates(w http.ResponseWriter, r *http.Request) {
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "scan_updates", "Update-Scan", nil)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id, "status": "eingereiht"})
}

type installRequest struct {
	Approved bool   `json:"approved"` // true = nur genehmigte Patches installieren
	AptMode  string `json:"apt_mode"` // "safe" = apt upgrade; sonst full-upgrade (Default)
}

// handleInstallUpdates reiht das Installieren ausstehender Updates ein – alle oder
// (approved=true) nur die genehmigten.
func (s *Server) handleInstallUpdates(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // leerer Body = alle installieren

	deviceID := chi.URLParam(r, "id")
	label := "Updates installieren"
	// apt-Strategie: Default full-upgrade; "safe" = konservatives apt upgrade.
	payload := map[string]any{"full": req.AptMode != "safe"}
	if req.Approved {
		names, err := s.store.ApprovedPatches(r.Context(), deviceID)
		if err != nil {
			s.mapStoreErr(w, err)
			return
		}
		if len(names) == 0 {
			s.writeErr(w, http.StatusBadRequest, "keine Patches genehmigt")
			return
		}
		payload["packages"] = names
		label = "Genehmigte Updates installieren"
	}
	id, err := s.queueCommand(r.Context(), deviceID, "install_updates", label, payload)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id, "status": "eingereiht"})
}

type approvePatchRequest struct {
	Name     string `json:"name"`
	Approved bool   `json:"approved"`
}

// handleApprovePatch genehmigt einen Patch bzw. hebt die Genehmigung auf.
func (s *Server) handleApprovePatch(w http.ResponseWriter, r *http.Request) {
	var req approvePatchRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		s.writeErr(w, http.StatusBadRequest, "name erforderlich")
		return
	}
	if err := s.store.ApprovePatch(r.Context(), chi.URLParam(r, "id"), req.Name, req.Approved); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReboot reiht einen Neustart-Befehl für ein Gerät ein.
func (s *Server) handleReboot(w http.ResponseWriter, r *http.Request) {
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "reboot", "Neustart", nil)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id, "status": "eingereiht"})
}
