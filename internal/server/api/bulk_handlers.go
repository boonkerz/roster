package api

import (
	"net/http"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/server/store"
)

type bulkRequest struct {
	TargetType string `json:"target_type"` // device | site | client | group | all
	TargetID   string `json:"target_id"`
	ScriptID   string `json:"script_id"`  // nur bei run-script
	PackageID  string `json:"package_id"` // nur bei install-package
}

// resolveBulkDevices löst die Zielgeräte auf oder schreibt eine Fehlerantwort.
func (s *Server) resolveBulkDevices(w http.ResponseWriter, r *http.Request, req bulkRequest) ([]string, bool) {
	if req.TargetType != "all" && req.TargetID == "" {
		s.writeErr(w, http.StatusBadRequest, "target_id fehlt")
		return nil, false
	}
	ids, err := s.store.DevicesForTarget(r.Context(), req.TargetType, req.TargetID)
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, "ungültiges Ziel")
		return nil, false
	}
	return ids, true
}

// handleBulkRunScript stellt ein Skript für alle Geräte eines Ziels in die Queue.
func (s *Server) handleBulkRunScript(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
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
	ids, ok := s.resolveBulkDevices(w, r, req)
	if !ok {
		return
	}
	queued := 0
	for _, dev := range ids {
		content := found.Content
		if agent, client, site, ferr := s.store.FieldMapsForDevice(r.Context(), dev); ferr == nil {
			content = store.SubstituteFields(content, agent, client, site)
		}
		payload := map[string]any{"shell": found.Shell, "script": content, "platforms": found.Platforms}
		if _, err := s.queueCommand(r.Context(), dev, "run_script", found.Name, payload); err == nil {
			queued++
		}
	}
	s.writeJSON(w, http.StatusCreated, map[string]int{"queued": queued})
}

// handleBulkScanUpdates reiht einen Update-Scan für alle Geräte eines Ziels ein.
func (s *Server) handleBulkScanUpdates(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	ids, ok := s.resolveBulkDevices(w, r, req)
	if !ok {
		return
	}
	queued := 0
	for _, dev := range ids {
		if _, err := s.queueCommand(r.Context(), dev, "scan_updates", "Update-Scan", nil); err == nil {
			queued++
		}
	}
	s.writeJSON(w, http.StatusCreated, map[string]int{"queued": queued})
}

// handleBulkInstallUpdates reiht das Installieren aller Updates für alle Geräte
// eines Ziels ein (apt: full-upgrade).
func (s *Server) handleBulkInstallUpdates(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	ids, ok := s.resolveBulkDevices(w, r, req)
	if !ok {
		return
	}
	queued := 0
	for _, dev := range ids {
		if _, err := s.queueCommand(r.Context(), dev, "install_updates", "Updates installieren", map[string]any{"full": true}); err == nil {
			queued++
		}
	}
	s.writeJSON(w, http.StatusCreated, map[string]int{"queued": queued})
}
