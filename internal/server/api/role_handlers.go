package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
)

// handleListRoles liefert alle Custom-Rollen (mit Benutzer-Zahl) sowie den
// vollständigen Permission-Katalog für die Rollen-Verwaltung.
func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.store.ListCustomRoles(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if roles == nil {
		roles = []model.CustomRole{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"roles":       roles,
		"permissions": model.AllPermissions,
	})
}

type roleRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req roleRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		s.writeErr(w, http.StatusBadRequest, "name erforderlich")
		return
	}
	role, err := s.store.CreateCustomRole(r.Context(), req.Name, req.Permissions)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, role)
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	var req roleRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		s.writeErr(w, http.StatusBadRequest, "name erforderlich")
		return
	}
	if err := s.store.UpdateCustomRole(r.Context(), chi.URLParam(r, "id"), req.Name, req.Permissions); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteCustomRole(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
