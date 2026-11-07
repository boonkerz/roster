package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
	"github.com/boonkerz/roster/internal/shared"
)

// Abläufe (Flow-Designer): Definitionen verwalten (page.scripts) und ausführen
// (devices.operate) – Vorschau auf einem Server, Test auf diesem Server, Rollout auf den Rest.

type flowRequest struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Graph       shared.FlowGraph   `json:"graph"`
	Params      []shared.FlowParam `json:"params"`
}

// validateFlowRequest prüft Namen, Parameter und den Graphen inkl. typabhängiger Pflichtfelder.
func validateFlowRequest(req flowRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("Name fehlt")
	}
	seen := map[string]bool{}
	for _, p := range req.Params {
		if !shared.ValidParamName(p.Name) {
			return fmt.Errorf("ungültiger Parametername %q (Buchstabe, dann Buchstaben/Ziffern/_)", p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("Parameter %q doppelt", p.Name)
		}
		seen[p.Name] = true
	}
	if err := shared.ValidateFlow(req.Graph); err != nil {
		return err
	}
	for _, n := range req.Graph.Nodes {
		str := func(k string) string { v, _ := n.Config[k].(string); return strings.TrimSpace(v) }
		name := n.Label
		if name == "" {
			name = n.ID
		}
		switch n.Type {
		case "find_files":
			if str("mode") == "compose_image" && str("image") == "" {
				return fmt.Errorf("%s: Image fehlt", name)
			}
			if str("mode") != "compose_image" && str("paths") == "" {
				return fmt.Errorf("%s: Pfade fehlen", name)
			}
		case "replace":
			if str("search") == "" {
				return fmt.Errorf("%s: Suchtext fehlt", name)
			}
			if b, _ := n.Config["regex"].(bool); b && !strings.Contains(str("search"), "{{") {
				if _, err := regexp.Compile(str("search")); err != nil {
					return fmt.Errorf("%s: ungültiger regulärer Ausdruck: %v", name, err)
				}
			}
		case "compose":
			switch str("action") {
			case "pull", "up", "restart", "down":
			default:
				return fmt.Errorf("%s: Aktion pull, up, restart oder down wählen", name)
			}
		case "command":
			if str("script") == "" && str("script_id") == "" {
				return fmt.Errorf("%s: Befehl oder Bibliotheks-Skript fehlt", name)
			}
		case "http":
			if str("url") == "" {
				return fmt.Errorf("%s: URL fehlt", name)
			}
		case "check":
			switch str("kind") {
			case "http", "tcp", "container", "file_contains", "expr":
			default:
				return fmt.Errorf("%s: Art der Prüfung wählen", name)
			}
		}
	}
	return nil
}

func (s *Server) handleListFlows(w http.ResponseWriter, r *http.Request) {
	flows, err := s.store.ListFlows(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, flows)
}

func (s *Server) handleGetFlow(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFlow(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleSaveFlow(w http.ResponseWriter, r *http.Request) {
	var req flowRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := validateFlowRequest(req); err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	f := &model.Flow{ID: chi.URLParam(r, "id"), Name: strings.TrimSpace(req.Name), Description: req.Description, Graph: req.Graph, Params: req.Params}
	if u := userFrom(r.Context()); u != nil {
		f.CreatedBy = u.Username
	}
	if err := s.store.SaveFlow(r.Context(), f); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFlow(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFlow(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFlowTemplates(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, flowTemplates)
}

type flowRunRequest struct {
	Params   map[string]string `json:"params"`
	DeviceID string            `json:"device_id"` // Vorschau-/Testserver
}

// handleStartFlowRun legt einen Lauf an und startet die Vorschau auf dem gewählten Server.
func (s *Server) handleStartFlowRun(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFlow(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	var req flowRunRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	params := map[string]string{}
	for _, p := range f.Params {
		v := strings.TrimSpace(req.Params[p.Name])
		if v == "" {
			v = p.Default
		}
		if v == "" && p.Required {
			s.writeErr(w, http.StatusBadRequest, "Parameter "+orName(p)+" fehlt")
			return
		}
		params[p.Name] = v
	}
	ids, ok := s.resolveBulkDevices(w, r, bulkRequest{TargetType: "devices", DeviceIDs: []string{req.DeviceID}})
	if !ok {
		return
	}
	if len(ids) != 1 {
		s.writeErr(w, http.StatusBadRequest, "Vorschau-Server nicht gefunden oder nicht verwaltet")
		return
	}
	run := &model.FlowRun{FlowID: f.ID, FlowName: f.Name, Graph: f.Graph, Params: params, TestDeviceID: ids[0]}
	if u := userFrom(r.Context()); u != nil {
		run.StartedBy = u.Username
	}
	if err := s.store.CreateFlowRun(r.Context(), run); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if err := s.queueFlow(r.Context(), run, ids[0], "preview"); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"id": run.ID})
}

func orName(p shared.FlowParam) string {
	if p.Label != "" {
		return p.Label
	}
	return p.Name
}

// handleFlowRunTest führt den Lauf echt auf dem Vorschau-Server aus.
func (s *Server) handleFlowRunTest(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetFlowRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if err := s.queueFlow(r.Context(), run, run.TestDeviceID, "test"); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"queued": 1})
}

// handleFlowRunRollout führt den Lauf auf allen Zielen aus (ohne den Testserver).
func (s *Server) handleFlowRunRollout(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetFlowRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	var req bulkRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	ids, ok := s.resolveBulkDevices(w, r, req)
	if !ok {
		return
	}
	queued := 0
	for _, id := range ids {
		if id == run.TestDeviceID {
			continue
		}
		if err := s.queueFlow(r.Context(), run, id, "rollout"); err == nil {
			queued++
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"queued": queued})
}

func (s *Server) handleGetFlowRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetFlowRun(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListFlowRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListFlowRuns(r.Context(), chi.URLParam(r, "id"), 30)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, runs)
}

// queueFlow bereitet den Graphen für ein Gerät vor (Feld-Platzhalter, Bibliotheks-Skripte)
// und reiht den Befehl run_flow ein.
func (s *Server) queueFlow(ctx context.Context, run *model.FlowRun, deviceID, stage string) error {
	g, err := s.prepareFlowGraph(ctx, run.Graph, deviceID)
	if err != nil {
		return err
	}
	label := run.FlowName
	switch stage {
	case "preview":
		label = "Ablauf (Vorschau): " + run.FlowName
	default:
		label = "Ablauf: " + run.FlowName
	}
	payload := map[string]any{"graph": g, "params": run.Params, "dry_run": stage == "preview", "run_id": run.ID[:12]}
	cmdID, err := s.queueCommand(ctx, deviceID, "run_flow", label, payload)
	if err != nil {
		return err
	}
	return s.store.AddFlowRunDevice(ctx, run.ID, deviceID, stage, cmdID)
}

func (s *Server) prepareFlowGraph(ctx context.Context, g shared.FlowGraph, deviceID string) (shared.FlowGraph, error) {
	agent, client, site, _ := s.store.FieldMapsForDevice(ctx, deviceID)
	var scripts []model.Script
	out := shared.FlowGraph{Edges: g.Edges}
	for _, n := range g.Nodes {
		cfg := map[string]any{}
		for k, v := range n.Config {
			if sv, ok := v.(string); ok {
				cfg[k] = store.SubstituteFields(sv, agent, client, site)
			} else {
				cfg[k] = v
			}
		}
		if n.Type == "command" {
			if id, _ := cfg["script_id"].(string); id != "" {
				if scripts == nil {
					scripts, _ = s.store.ListScripts(ctx)
				}
				found := false
				for _, sc := range scripts {
					if sc.ID == id {
						cfg["script"] = store.SubstituteFields(sc.Content, agent, client, site)
						cfg["shell"] = sc.Shell
						found = true
					}
				}
				if !found {
					return out, fmt.Errorf("Bibliotheks-Skript %s nicht gefunden", id)
				}
			}
		}
		n.Config = cfg
		out.Nodes = append(out.Nodes, n)
	}
	return out, nil
}

// completeFlowRuns übernimmt die Ergebnisse von run_flow-Befehlen in die Läufe.
func (s *Server) completeFlowRuns(ctx context.Context, results []shared.CommandResult) {
	for _, res := range results {
		status := "fail"
		var fr shared.FlowResult
		if json.Unmarshal([]byte(res.Output), &fr) == nil && fr.Status != "" {
			status = fr.Status
		} else {
			fr = shared.FlowResult{Status: "fail", Error: strings.TrimSpace(res.Output)}
		}
		b, _ := json.Marshal(fr)
		if _, err := s.store.CompleteFlowCommand(ctx, res.CommandID, status, string(b)); err != nil {
			s.log.Error("ablauf-ergebnis speichern", "err", err)
		}
	}
}
