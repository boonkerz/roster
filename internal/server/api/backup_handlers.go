package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
)

// HTTP-Schnittstelle für den Backup-Bereich der Richtlinien.

type backupRequest struct {
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Enabled        *bool          `json:"enabled"`
	Config         map[string]any `json:"config"`
	ScriptID       *string        `json:"script_id"`
	Weekdays       string         `json:"weekdays"`
	AtTime         string         `json:"at_time"`
	CatchUpMinutes int            `json:"catch_up_minutes"`
	WaitDeviceID   *string        `json:"wait_device_id"`
	WaitMinutes    int            `json:"wait_minutes"`
	TimeoutMinutes int            `json:"timeout_minutes"`
	AfterDeviceID  *string        `json:"after_device_id"`
	AfterScriptID  *string        `json:"after_script_id"`
	AfterWhen      string         `json:"after_when"`
}

// apply überträgt die Anfrage auf einen Eintrag und prüft die Pflichtangaben.
func (req backupRequest) apply(b *model.PolicyBackup) error {
	b.Name = strings.TrimSpace(req.Name)
	if b.Name == "" {
		return errBadRequest("name erforderlich")
	}
	switch req.Type {
	case "proxmox", "script":
		b.Type = req.Type
	default:
		return errBadRequest("type muss proxmox oder script sein")
	}
	if _, _, ok := parseClock(req.AtTime); !ok {
		return errBadRequest("uhrzeit muss HH:MM sein")
	}
	switch req.AfterWhen {
	case "", "always", "success", "failure":
	default:
		return errBadRequest("after_when muss always, success oder failure sein")
	}
	if b.Type == "script" && (req.ScriptID == nil || *req.ScriptID == "") {
		return errBadRequest("für Typ script ist ein Skript erforderlich")
	}
	if req.AfterDeviceID != nil && *req.AfterDeviceID != "" && (req.AfterScriptID == nil || *req.AfterScriptID == "") {
		return errBadRequest("Folgeaktion braucht ein Skript")
	}
	b.Enabled = req.Enabled == nil || *req.Enabled
	b.Config = req.Config
	if b.Config == nil {
		b.Config = map[string]any{}
	}
	b.ScriptID = emptyToNil(req.ScriptID)
	b.Weekdays = strings.TrimSpace(req.Weekdays)
	b.AtTime = strings.TrimSpace(req.AtTime)
	b.CatchUpMinutes = orDefault(req.CatchUpMinutes, 360)
	b.WaitDeviceID = emptyToNil(req.WaitDeviceID)
	b.WaitMinutes = req.WaitMinutes
	b.TimeoutMinutes = orDefault(req.TimeoutMinutes, 720)
	b.AfterDeviceID = emptyToNil(req.AfterDeviceID)
	b.AfterScriptID = emptyToNil(req.AfterScriptID)
	b.AfterWhen = req.AfterWhen
	if b.AfterWhen == "" {
		b.AfterWhen = "always"
	}
	return nil
}

func emptyToNil(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	s := strings.TrimSpace(*v)
	return &s
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// errBadRequest markiert Eingabefehler (400 statt 500).
type errBadRequest string

func (e errBadRequest) Error() string { return string(e) }

// handleAddBackup legt einen Backup-Eintrag in einer Richtlinie an.
func (s *Server) handleAddBackup(w http.ResponseWriter, r *http.Request) {
	var req backupRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	b := model.PolicyBackup{PolicyID: chi.URLParam(r, "id")}
	if err := req.apply(&b); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveBackup(r.Context(), &b); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, b)
}

// handleUpdateBackup ändert einen Eintrag.
func (s *Server) handleUpdateBackup(w http.ResponseWriter, r *http.Request) {
	var req backupRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	b, err := s.store.GetBackup(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if err := req.apply(b); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveBackup(r.Context(), b); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, b)
}

// handleDeleteBackup entfernt einen Eintrag samt Lauf-Historie.
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteBackup(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

// handleRunBackup startet einen Eintrag sofort auf einem Gerät ("Jetzt starten").
func (s *Server) handleRunBackup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	b, err := s.store.GetBackup(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	device, err := s.store.GetDevice(r.Context(), req.DeviceID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if b.Type == "proxmox" && device.ProxmoxVersion == "" {
		s.writeErr(w, http.StatusBadRequest, "das Gerät ist kein Proxmox-Host")
		return
	}
	now := time.Now()
	runID := s.startOrQueueRun(r.Context(), *b, device, now, "manual", now)
	if runID == "" {
		s.writeErr(w, http.StatusConflict, "Lauf konnte nicht gestartet werden (läuft bereits?)")
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"run_id": runID, "status": "gestartet"})
}

// handleDeviceBackupRuns liefert die Lauf-Historie eines Geräts.
func (s *Server) handleDeviceBackupRuns(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	runs, err := s.store.BackupRunsForDevice(r.Context(), chi.URLParam(r, "id"), limit)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if runs == nil {
		runs = []model.BackupRun{}
	}
	s.writeJSON(w, http.StatusOK, runs)
}

// handleGetBackupRun liefert einen Lauf inklusive vollständiger Ausgabe.
func (s *Server) handleGetBackupRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.BackupRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, run)
}
