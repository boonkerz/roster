package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/server/store"
)

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListGroups(r.Context(), time.Now().Add(-s.cfg.OfflineAfter))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, groups)
}

type groupRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ParentID    *string `json:"parent_id"`
	Rule        string  `json:"rule"`
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		s.writeErr(w, http.StatusBadRequest, "name erforderlich")
		return
	}
	g := &model.Group{ID: store.NewID(), Name: req.Name, Description: req.Description, ParentID: req.ParentID, Rule: req.Rule}
	if err := s.store.CreateGroup(r.Context(), g); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, g)
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	g := &model.Group{ID: chi.URLParam(r, "id"), Name: req.Name, Description: req.Description, ParentID: req.ParentID, Rule: req.Rule}
	if err := s.store.UpdateGroup(r.Context(), g); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteGroup(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}
