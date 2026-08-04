package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/auth"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

// handleListAPITokens listet die (nicht widerrufenen) API-Tokens des angemeldeten
// Benutzers – ohne Klartext/Hash.
func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	tokens, err := s.store.ListUserAPITokens(r.Context(), u.ID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, tokens)
}

type createAPITokenRequest struct {
	Label      string `json:"label"`
	ExpiresInH int    `json:"expires_in_hours"` // 0 = nie
}

// handleCreateAPIToken erzeugt ein langlebiges User-API-Token und gibt es EINMALIG im
// Klartext zurück. Erfordert eine gültige, voll authentifizierte Sitzung (Passwort +
// ggf. TOTP) – siehe requireEnrolled auf der Route.
func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	var req createAPITokenRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	u := userFrom(r.Context())
	plain := auth.GenerateToken()
	tok := &model.UserAPIToken{
		ID:     store.NewID(),
		UserID: u.ID,
		Label:  req.Label,
	}
	if req.ExpiresInH > 0 {
		exp := time.Now().Add(time.Duration(req.ExpiresInH) * time.Hour).UTC()
		tok.ExpiresAt = &exp
	}
	if err := s.store.CreateUserAPIToken(r.Context(), tok, auth.HashToken(plain)); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	tok.Token = plain // nur in dieser Antwort
	s.writeJSON(w, http.StatusCreated, tok)
}

// handleRevokeAPIToken widerruft ein eigenes API-Token.
func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if err := s.store.RevokeUserAPIToken(r.Context(), chi.URLParam(r, "id"), u.ID); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "widerrufen"})
}
