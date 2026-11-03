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
	logSourceTypes   = map[string]bool{"file": true, "docker": true, "journal": true, "docker_events": true}
	logSourceFormats = map[string]bool{"nginx_access": true, "apache_access": true, "nginx_error": true, "sshd": true, "ftp": true, "mail": true, "haproxy": true, "traefik": true, "mysql": true, "firewall": true, "docker_events": true, "regex": true}
	reJournalTarget  = regexp.MustCompile(`^[A-Za-z0-9_.@:-]{1,64}$`)
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
		return errors.New("type muss file|docker|journal|docker_events sein")
	}
	if req.Type == "docker_events" {
		req.Format = "docker_events" // der Typ bestimmt das Format
		if strings.TrimSpace(req.Target) == "" {
			req.Target = "*"
		}
	}
	if !logSourceFormats[req.Format] {
		return errors.New("format muss nginx_access|apache_access|nginx_error|sshd|ftp|mail|haproxy|traefik|mysql|firewall|docker_events|regex sein")
	}
	if (req.Format == "docker_events") != (req.Type == "docker_events") {
		return errors.New("Format docker_events gehört zum Typ docker_events")
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return errors.New("Ziel fehlt (Dateipfad, Container-Name bzw. Journal-Kennung)")
	}
	if req.Type == "journal" && !reJournalTarget.MatchString(target) {
		return errors.New("Journal-Ziel: „kernel“ oder eine SYSLOG_IDENTIFIER-Kennung (Buchstaben, Ziffern, ._@:-)")
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

// handleDeviceSecurityMonitoring zeigt, welche Sicherheitsüberwachung auf dem Gerät wirkt
// (Anmeldungen, Datei-Integrität, Compliance, Log-Quellen) – Log-Quellen mit dem vom
// Agent gemeldeten Lesestatus.
func (s *Server) handleDeviceSecurityMonitoring(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	deviceID := chi.URLParam(r, "id")
	if _, err := s.store.GetDevice(ctx, deviceID); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	bundle, err := s.store.EffectivePolicy(ctx, deviceID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	out := model.SecurityMonitoring{LogSources: []model.DeviceLogSource{}}
	if since := s.store.AuthMonitorSince(ctx, deviceID); !since.IsZero() {
		out.AuthMonitorSince = &since
	}
	if bundle != nil && bundle.Security != nil {
		sec := bundle.Security
		out.AuthMonitor = sec.AuthMonitor
		if sec.FIM != nil {
			out.FIMRules = len(sec.FIM.Rules)
		}
		out.Compliance = sec.Compliance != nil && sec.Compliance.Enabled
		out.FIMWhoData = sec.FIM != nil && sec.FIM.WhoData
		out.StateMonitor, out.Rootcheck = sec.StateMonitor, sec.Rootcheck
		status, err := s.store.LogSourceStatusFor(ctx, deviceID)
		if err != nil {
			s.mapStoreErr(w, err)
			return
		}
		policyName := map[string]string{}
		if policies, err := s.store.ListPolicies(ctx); err == nil {
			for _, p := range policies {
				policyName[p.ID] = p.Name
			}
		}
		for _, src := range sec.LogSources {
			d := status[src.ID]
			d.ID, d.Name, d.Type, d.Target, d.Format, d.Pattern, d.Kind = src.ID, src.Name, src.Type, src.Target, src.Format, src.Pattern, src.Kind
			if pl, err := s.store.GetLogSource(ctx, src.ID); err == nil {
				d.PolicyID, d.PolicyName = pl.PolicyID, policyName[pl.PolicyID]
			}
			out.LogSources = append(out.LogSources, d)
		}
	}
	s.writeJSON(w, http.StatusOK, out)
}

type testLogSourceRequest struct {
	logSourceRequest
	Lines int `json:"lines"`
}

// handleTestLogSource lässt den Agent eines Geräts eine (auch ungespeicherte) Log-Quelle
// probeweise lesen: letzte Zeilen parsen, Treffer zählen. Ergebnis über GET /commands/{id}.
func (s *Server) handleTestLogSource(w http.ResponseWriter, r *http.Request) {
	var req testLogSourceRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	var l model.PolicyLogSource
	if err := req.logSourceRequest.apply(&l); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	src := map[string]any{"id": "test", "name": l.Name, "type": l.Type, "target": l.Target, "format": l.Format, "pattern": l.Pattern, "kind": l.Kind}
	lines := req.Lines
	if lines <= 0 || lines > 5000 {
		lines = 300
	}
	s.queueDeviceCmd(w, r, "test_log_source", "Log-Quelle testen: "+l.Name, map[string]any{"source": src, "lines": lines})
}
