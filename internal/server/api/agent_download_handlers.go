package api

import (
	"bytes"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/agentdist"
	"github.com/thomaspeterson/pc-inventory/internal/server/viewerdist"
)

// handleAgentList liefert die verfügbaren Agent-Plattformen (für den "Neuer Computer"-Dialog).
func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"platforms": agentdist.Available()})
}

// handleAgentDownload streamt das Agent-Binary einer Plattform.
// Öffentlich erreichbar, damit Install-Skripte auf frischen Maschinen es ohne Login laden können.
func (s *Server) handleAgentDownload(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")
	data, filename, ok := agentdist.Read(platform)
	if !ok {
		s.writeErr(w, http.StatusNotFound, "agent für plattform nicht verfügbar: "+platform)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
}

// handleViewerDownload streamt das native Fernsteuerungs-Viewer-Binary (pcinv-viewer)
// einer Plattform. Enthält keine Geheimnisse (die Berechtigung steckt im pro-Sitzung
// erzeugten Startcode), daher wie der Agent öffentlich ladbar.
func (s *Server) handleViewerDownload(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")
	data, filename, ok := viewerdist.Read(platform)
	if !ok {
		s.writeErr(w, http.StatusNotFound, "viewer für plattform nicht verfügbar: "+platform)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	http.ServeContent(w, r, filename, time.Time{}, bytes.NewReader(data))
}
