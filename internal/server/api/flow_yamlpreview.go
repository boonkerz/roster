package api

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/boonkerz/roster/internal/shared/textdiff"
	"github.com/boonkerz/roster/internal/shared/yamledit"
)

// Vorschau für den Baustein „YAML ändern“ im Editor: wendet die Operationen mit demselben Code
// wie der Agent auf ein eingefügtes YAML an (nichts wird gespeichert oder ausgeführt).

type yamlPreviewRequest struct {
	YAML   string            `json:"yaml"`
	Ops    string            `json:"ops"`
	Params map[string]string `json:"params"`
}

type yamlPreviewResponse struct {
	Result   string              `json:"result"`
	Diff     string              `json:"diff"`
	Results  []yamledit.OpResult `json:"results"`
	Changes  int                 `json:"changes"`
	Error    string              `json:"error,omitempty"`
	Warnings []string            `json:"warnings,omitempty"`
}

var previewParamRe = regexp.MustCompile(`\{\{\s*param\.([A-Za-z0-9_]+)\s*\}\}`)
var previewStepRe = regexp.MustCompile(`\{\{\s*steps\.[^}]*\}\}`)

func (s *Server) handleYAMLPreview(w http.ResponseWriter, r *http.Request) {
	var req yamlPreviewRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	resp := yamlPreviewResponse{Result: req.YAML, Results: []yamledit.OpResult{}}
	ops := previewParamRe.ReplaceAllStringFunc(req.Ops, func(m string) string {
		name := previewParamRe.FindStringSubmatch(m)[1]
		if v, ok := req.Params[name]; ok {
			return v
		}
		resp.Warnings = append(resp.Warnings, "Parameter "+name+" ohne Wert – bitte links eintragen")
		return m
	})
	if steps := previewStepRe.FindAllString(ops, -1); len(steps) > 0 {
		resp.Warnings = append(resp.Warnings, "Werte aus anderen Bausteinen ("+strings.Join(steps, ", ")+") gibt es erst im Lauf – in der Vorschau bleiben sie als Text stehen")
	}
	if _, err := yamledit.Parse([]byte(req.YAML)); err != nil {
		resp.Error = "Eingabe ist kein gültiges YAML: " + err.Error()
		s.writeJSON(w, http.StatusOK, resp)
		return
	}
	parsed, err := yamledit.ParseOps(ops)
	if err != nil {
		resp.Error = err.Error()
		s.writeJSON(w, http.StatusOK, resp)
		return
	}
	out, results, err := yamledit.ApplyAll(req.YAML, parsed)
	resp.Results = results
	for _, r := range results {
		if r.Changed {
			resp.Changes++
		}
	}
	if err != nil {
		resp.Error = err.Error()
	}
	resp.Result = out
	if out != req.YAML {
		resp.Diff = textdiff.Unified("vorher → nachher", req.YAML, out)
	}
	s.writeJSON(w, http.StatusOK, resp)
}
