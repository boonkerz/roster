package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
)

// Log-Quellen einer Richtlinie: zusätzliche Protokolle (Dateien, Docker-Container), die der
// Agent auf Anmeldeversuche und Angriffsmuster liest.

var (
	logSourceTypes   = map[string]bool{"file": true, "docker": true}
	logSourceFormats = map[string]bool{"nginx_access": true, "nginx_error": true, "sshd": true, "ftp": true, "regex": true}
	logSourceKinds   = map[string]bool{"login_failed": true, "probe": true}
	reServiceName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)
)

type logSourceRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Target  string `json:"target"`
	Format  string `json:"format"`
	Pattern string `json:"pattern"`
	Kind    string `json:"kind"`
	Enabled *bool  `json:"enabled"`
}

func (req logSourceRequest) apply(l *model.PolicyLogSource) error {
	name := strings.TrimSpace(req.Name)
	if !reServiceName.MatchString(name) {
		return errors.New("Name: 1–40 Zeichen aus Buchstaben, Ziffern, Punkt, Bindestrich, Unterstrich")
	}
	if !logSourceTypes[req.Type] {
		return errors.New("type muss file|docker sein")
	}
	if !logSourceFormats[req.Format] {
		return errors.New("format muss nginx_access|nginx_error|sshd|ftp|regex sein")
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return errors.New("Ziel fehlt (Dateipfad bzw. Container-Name)")
	}
	if req.Type == "file" && !strings.HasPrefix(target, "/") && !winAbsPath.MatchString(target) {
		return errors.New("Dateipfad muss absolut sein")
	}
	l.Pattern, l.Kind = "", ""
	if req.Format == "regex" {
		pat := strings.TrimSpace(req.Pattern)
		re, err := regexp.Compile(pat)
		if err != nil {
			return errors.New("ungültiges Muster: " + err.Error())
		}
		if re.SubexpIndex("ip") < 0 {
			return errors.New("das Muster braucht eine benannte Gruppe (?P<ip>…)")
		}
		if !logSourceKinds[req.Kind] {
			return errors.New("kind muss login_failed|probe sein")
		}
		l.Pattern, l.Kind = pat, req.Kind
	}
	l.Name, l.Type, l.Target, l.Format = name, req.Type, target, req.Format
	l.Enabled = true
	if req.Enabled != nil {
		l.Enabled = *req.Enabled
	}
	return nil
}

func (s *Server) handleAddLogSource(w http.ResponseWriter, r *http.Request) {
	var req logSourceRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	l := model.PolicyLogSource{PolicyID: chi.URLParam(r, "id")}
	if err := req.apply(&l); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveLogSource(r.Context(), &l); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, l)
}

func (s *Server) handleUpdateLogSource(w http.ResponseWriter, r *http.Request) {
	var req logSourceRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	l, err := s.store.GetLogSource(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if err := req.apply(l); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveLogSource(r.Context(), l); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, l)
}

func (s *Server) handleDeleteLogSource(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteLogSource(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
