package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

// handleClientTree liefert die Client/Site-Hierarchie inkl. Zähler (für den Baum).
func (s *Server) handleClientTree(w http.ResponseWriter, r *http.Request) {
	sites, unrestricted := s.allowedSites(r.Context())
	filter := sites
	if unrestricted {
		filter = nil
	}
	clients, err := s.store.ClientTree(r.Context(), filter)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	// Nicht zugeordnete Geräte sind für eingeschränkte Benutzer nicht sichtbar.
	unassigned := 0
	if unrestricted {
		if unassigned, err = s.store.UnassignedDeviceCount(r.Context()); err != nil {
			s.mapStoreErr(w, err)
			return
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"clients": clients, "unassigned_count": unassigned})
}

type nameRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var req nameRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		s.writeErr(w, http.StatusBadRequest, "name erforderlich")
		return
	}
	c := &model.Client{ID: store.NewID(), Name: req.Name}
	if err := s.store.CreateClient(r.Context(), c); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, c)
}

func (s *Server) handleRenameClient(w http.ResponseWriter, r *http.Request) {
	var req nameRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.RenameClient(r.Context(), chi.URLParam(r, "id"), req.Name); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteClient(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if r.URL.Query().Get("force") != "true" {
		if n, err := s.store.CountDevicesForClient(r.Context(), id); err == nil && n > 0 {
			s.writeJSON(w, http.StatusConflict, map[string]any{"error": "has_devices", "device_count": n})
			return
		}
	}
	if err := s.store.DeleteClient(r.Context(), id); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

type createSiteRequest struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	var req createSiteRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.ClientID == "" || req.Name == "" {
		s.writeErr(w, http.StatusBadRequest, "client_id und name erforderlich")
		return
	}
	site := &model.Site{ID: store.NewID(), ClientID: req.ClientID, Name: req.Name}
	if err := s.store.CreateSite(r.Context(), site); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, site)
}

func (s *Server) handleRenameSite(w http.ResponseWriter, r *http.Request) {
	var req nameRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.RenameSite(r.Context(), chi.URLParam(r, "id"), req.Name); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if r.URL.Query().Get("force") != "true" {
		if n, err := s.store.CountDevicesForSite(r.Context(), id); err == nil && n > 0 {
			s.writeJSON(w, http.StatusConflict, map[string]any{"error": "has_devices", "device_count": n})
			return
		}
	}
	if err := s.store.DeleteSite(r.Context(), id); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

type setSiteRequest struct {
	SiteID *string `json:"site_id"`
}

// handleSetDeviceSite ordnet ein Gerät einer Site zu (site_id=null hebt die Zuordnung auf).
func (s *Server) handleSetDeviceSite(w http.ResponseWriter, r *http.Request) {
	var req setSiteRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetDeviceSite(r.Context(), chi.URLParam(r, "id"), req.SiteID); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "aktualisiert"})
}
