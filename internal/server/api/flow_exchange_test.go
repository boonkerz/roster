package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func TestFlowDocumentRoundtrip(t *testing.T) {
	tpl := flowTemplates[0]
	doc := flowDocument{Format: flowFormat, Version: flowFormatVersion, Name: tpl.Name, Params: tpl.Params, Nodes: tpl.Graph.Nodes, Edges: tpl.Graph.Edges}
	b, _ := json.Marshal(doc)
	var back flowDocument
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	req, err := documentToRequest(back)
	if err != nil {
		t.Fatal(err)
	}
	if req.Name != tpl.Name || len(req.Graph.Nodes) != len(tpl.Graph.Nodes) || len(req.Params) != len(tpl.Params) {
		t.Fatalf("%+v", req)
	}
}

func TestFlowDocumentRejects(t *testing.T) {
	ok := flowDocument{Format: flowFormat, Version: 1, Name: "x", Nodes: []shared.FlowNode{{ID: "w", Type: "wait"}}}
	if _, err := documentToRequest(ok); err != nil {
		t.Fatal(err)
	}
	cases := map[string]flowDocument{
		"Format":    {Format: "n8n", Version: 1, Name: "x", Nodes: ok.Nodes},
		"Version":   {Format: flowFormat, Version: 2, Name: "x", Nodes: ok.Nodes},
		"script_id": {Format: flowFormat, Version: 1, Name: "x", Nodes: []shared.FlowNode{{ID: "c", Type: "command", Config: map[string]any{"script_id": "abc"}}}},
		"Baustein":  {Format: flowFormat, Version: 1, Name: "x", Nodes: []shared.FlowNode{{ID: "c", Type: "rm_rf"}}},
	}
	for want, d := range cases {
		if _, err := documentToRequest(d); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
	}
}
