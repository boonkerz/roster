package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, users)
}

type createUserRequest struct {
	Username     string `json:"username"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Role         string `json:"role"`
	CustomRoleID string `json:"custom_role_id"`
}

// handleCreateUser legt einen lokalen Benutzer an (nur Admin).
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		s.writeErr(w, http.StatusBadRequest, "username und password erforderlich")
		return
	}
	role := model.Role(req.Role)
	if role != model.RoleAdmin && role != model.RoleTech && role != model.RoleViewer {
		role = model.RoleViewer
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.writeErr(w, http.StatusInternalServerError, "hashing fehlgeschlagen")
		return
	}
	u := &model.User{
		ID:           store.NewID(),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hash,
		Role:         role,
		CustomRoleID: req.CustomRoleID,
		AuthSource:   model.AuthLocal,
	}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, u)
}

// handleUpdateUser ändert Rolle und optionale Custom-Rolle eines Benutzers (Admin).
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Role         string `json:"role"`
		CustomRoleID string `json:"custom_role_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	role := model.Role(req.Role)
	if role != model.RoleAdmin && role != model.RoleTech && role != model.RoleViewer {
		role = model.RoleViewer
	}
	// Selbst-Aussperrung vermeiden: der eigene Account darf nicht die Admin-Rolle verlieren.
	if u := userFrom(r.Context()); u != nil && u.ID == id && role != model.RoleAdmin {
		s.writeErr(w, http.StatusBadRequest, "die eigene Admin-Rolle kann nicht entzogen werden")
		return
	}
	if err := s.store.UpdateUserRole(r.Context(), id, role, req.CustomRoleID); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleGetUserScope liefert den Daten-Scope eines Benutzers (zugeordnete Kunden/Standorte).
func (s *Server) handleGetUserScope(w http.ResponseWriter, r *http.Request) {
	clientIDs, siteIDs, err := s.store.GetUserScope(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"clients": clientIDs, "sites": siteIDs})
}

// handleSetUserScope setzt den Daten-Scope eines Benutzers (leere Listen = unbeschränkt).
func (s *Server) handleSetUserScope(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Clients []string `json:"clients"`
		Sites   []string `json:"sites"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetUserScope(r.Context(), chi.URLParam(r, "id"), req.Clients, req.Sites); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAdminReset2FA deaktiviert die Zwei-Faktor-Authentifizierung eines Benutzers
// (Admin, für ausgesperrte Nutzer). Bei 2FA-Pflicht muss der Nutzer beim nächsten
// Login neu einrichten.
func (s *Server) handleAdminReset2FA(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.store.GetUserByID(r.Context(), id); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if err := s.store.ClearUserTOTP(r.Context(), id); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
