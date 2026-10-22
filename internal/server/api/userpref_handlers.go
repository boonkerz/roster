package api

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
)

// Benutzereinstellungen der Web-UI (Spalten, gespeicherte Filter, Export-Spalten …):
// frei wählbare Schlüssel mit JSON-Wert, je angemeldetem Benutzer. Der Server
// interpretiert die Werte nicht, begrenzt nur Schlüsselform und Größe.
var prefKeyRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,64}$`)

const maxPrefBytes = 64 << 10

func (s *Server) handleGetUserPrefs(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.store.UserPrefs(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, prefs)
}

func (s *Server) handleSetUserPref(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if !prefKeyRe.MatchString(key) {
		s.writeErr(w, http.StatusBadRequest, "ungültiger Schlüssel")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPrefBytes+1))
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, "Body nicht lesbar")
		return
	}
	if len(body) > maxPrefBytes {
		s.writeErr(w, http.StatusRequestEntityTooLarge, "Einstellung zu groß (max. 64 KB)")
		return
	}
	if !json.Valid(body) {
		s.writeErr(w, http.StatusBadRequest, "Body muss gültiges JSON sein")
		return
	}
	if err := s.store.SetUserPref(r.Context(), userFrom(r.Context()).ID, key, json.RawMessage(body)); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteUserPref(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if !prefKeyRe.MatchString(key) {
		s.writeErr(w, http.StatusBadRequest, "ungültiger Schlüssel")
		return
	}
	if err := s.store.DeleteUserPref(r.Context(), userFrom(r.Context()).ID, key); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
