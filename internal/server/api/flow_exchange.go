package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/shared"
)

// Austauschformat für Abläufe („roster-flow“, Version 1): eine JSON-Datei mit Name,
// Beschreibung, Parametern, Bausteinen und Verbindungen. Bibliotheks-Skripte werden beim
// Export eingebettet (ihre IDs gibt es auf anderen Servern nicht). Schema:
// https://roster-rmm.dev/schemas/roster-flow-1.schema.json

const (
	flowFormat        = "roster-flow"
	flowFormatVersion = 1
	flowSchemaURL     = "https://roster-rmm.dev/schemas/roster-flow-1.schema.json"
)

type flowDocument struct {
	Schema      string             `json:"$schema,omitempty"`
	Format      string             `json:"format"`
	Version     int                `json:"version"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Params      []shared.FlowParam `json:"params"`
	Nodes       []shared.FlowNode  `json:"nodes"`
	Edges       []shared.FlowEdge  `json:"edges"`
	ExportedAt  string             `json:"exported_at,omitempty"`
	ExportedBy  string             `json:"exported_by,omitempty"` // Roster-Version
}

// flowToDocument erzeugt das Austauschdokument; Bibliotheks-Skripte werden eingebettet.
func (s *Server) flowToDocument(ctx context.Context, f *model.Flow) (flowDocument, error) {
	doc := flowDocument{Schema: flowSchemaURL, Format: flowFormat, Version: flowFormatVersion, Name: f.Name, Description: f.Description,
		Params: f.Params, Edges: f.Graph.Edges, ExportedAt: time.Now().UTC().Format(time.RFC3339), ExportedBy: "Roster " + s.version}
	var scripts []model.Script
	for _, n := range f.Graph.Nodes {
		cfg := map[string]any{}
		for k, v := range n.Config {
			cfg[k] = v
		}
		if id, _ := cfg["script_id"].(string); id != "" && n.Type == "command" {
			if scripts == nil {
				var err error
				if scripts, err = s.store.ListScripts(ctx); err != nil {
					return doc, err
				}
			}
			found := false
			for _, sc := range scripts {
				if sc.ID == id {
					cfg["script"], cfg["shell"] = sc.Content, sc.Shell
					found = true
				}
			}
			if !found {
				return doc, fmt.Errorf("Bibliotheks-Skript %s im Baustein %s nicht gefunden", id, n.ID)
			}
			delete(cfg, "script_id")
		}
		n.Config = cfg
		doc.Nodes = append(doc.Nodes, n)
	}
	if doc.Params == nil {
		doc.Params = []shared.FlowParam{}
	}
	if doc.Edges == nil {
		doc.Edges = []shared.FlowEdge{}
	}
	return doc, nil
}

// documentToRequest prüft Format/Version und liefert den Ablauf (ohne Speichern).
func documentToRequest(doc flowDocument) (flowRequest, error) {
	if doc.Format != flowFormat {
		return flowRequest{}, fmt.Errorf("unbekanntes Format %q (erwartet %q)", doc.Format, flowFormat)
	}
	if doc.Version != flowFormatVersion {
		return flowRequest{}, fmt.Errorf("Version %d wird nicht unterstützt (diese Roster-Version liest Version %d)", doc.Version, flowFormatVersion)
	}
	for _, n := range doc.Nodes {
		if id, _ := n.Config["script_id"].(string); id != "" {
			return flowRequest{}, fmt.Errorf("Baustein %s verweist auf ein Bibliotheks-Skript (script_id) – im Austauschformat muss der Befehl eingebettet sein", n.ID)
		}
	}
	req := flowRequest{Name: strings.TrimSpace(doc.Name), Description: doc.Description, Params: doc.Params,
		Graph: shared.FlowGraph{Nodes: doc.Nodes, Edges: doc.Edges}}
	if req.Params == nil {
		req.Params = []shared.FlowParam{}
	}
	if req.Graph.Edges == nil {
		req.Graph.Edges = []shared.FlowEdge{}
	}
	if err := validateFlowRequest(req); err != nil {
		return flowRequest{}, err
	}
	return req, nil
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// handleExportFlow liefert den Ablauf als Datei <name>.roster-flow.json.
func (s *Server) handleExportFlow(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFlow(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	doc, err := s.flowToDocument(r.Context(), f)
	if err != nil {
		s.writeErr(w, http.StatusConflict, err.Error())
		return
	}
	name := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "Ae", "Ö", "Oe", "Ü", "Ue", "ß", "ss").Replace(f.Name)
	name = strings.Trim(unsafeFileChars.ReplaceAllString(name, "-"), "-")
	if name == "" {
		name = "ablauf"
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.roster-flow.json"`)
	s.writeJSON(w, http.StatusOK, doc)
}

// handleImportFlow legt einen Ablauf aus einem Austauschdokument an (Name bei Bedarf mit
// Zusatz „(Import)“, damit nichts überschrieben wird). ?dry_run=1 prüft nur.
func (s *Server) handleImportFlow(w http.ResponseWriter, r *http.Request) {
	var doc flowDocument
	if !s.decodeJSON(w, r, &doc) {
		return
	}
	req, err := documentToRequest(doc)
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.URL.Query().Get("dry_run") == "1" {
		s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": req.Name, "nodes": len(req.Graph.Nodes), "params": len(req.Params)})
		return
	}
	existing, err := s.store.ListFlows(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	taken := map[string]bool{}
	for _, f := range existing {
		taken[f.Name] = true
	}
	name := req.Name
	for i := 1; taken[name]; i++ {
		name = fmt.Sprintf("%s (Import %d)", req.Name, i)
		if i == 1 {
			name = req.Name + " (Import)"
		}
	}
	f := &model.Flow{Name: name, Description: req.Description, Graph: req.Graph, Params: req.Params}
	if u := userFrom(r.Context()); u != nil {
		f.CreatedBy = u.Username
	}
	if err := s.store.SaveFlow(r.Context(), f); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, f)
}
