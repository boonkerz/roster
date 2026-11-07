package api

import (
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func TestFlowTemplatesValid(t *testing.T) {
	for _, tpl := range flowTemplates {
		if err := validateFlowRequest(flowRequest{Name: tpl.Name, Graph: tpl.Graph, Params: tpl.Params}); err != nil {
			t.Errorf("%s: %v", tpl.Key, err)
		}
	}
}

func TestValidateFlowRequest(t *testing.T) {
	g := shared.FlowGraph{Nodes: []shared.FlowNode{{ID: "a", Type: "replace", Config: map[string]any{"search": "(", "regex": true}}}}
	if err := validateFlowRequest(flowRequest{Name: "x", Graph: g}); err == nil || !strings.Contains(err.Error(), "regulärer") {
		t.Fatalf("regex: %v", err)
	}
	g.Nodes[0].Config = map[string]any{"search": "8000", "replace": "80"}
	if err := validateFlowRequest(flowRequest{Name: "x", Graph: g, Params: []shared.FlowParam{{Name: "1bad"}}}); err == nil {
		t.Fatal("ungültiger Parameter akzeptiert")
	}
	if err := validateFlowRequest(flowRequest{Name: "", Graph: g}); err == nil {
		t.Fatal("ohne Namen akzeptiert")
	}
	if err := validateFlowRequest(flowRequest{Name: "ok", Graph: g}); err != nil {
		t.Fatal(err)
	}
}
