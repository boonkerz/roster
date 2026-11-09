package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestFlowSchemaCoversTemplates prüft, dass das veröffentlichte JSON-Schema alle Bausteine und
// alle Konfigurationsschlüssel der Vorlagen kennt, und schreibt die Vorlagen als
// Austauschdokumente nach $FLOW_DOCS_OUT (für eine externe Schema-Prüfung).
func TestFlowSchemaCoversTemplates(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "schemas", "roster-flow-1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			Node struct {
				Properties struct {
					Type struct {
						Enum []string `json:"enum"`
					} `json:"type"`
				} `json:"properties"`
				AllOf []struct {
					If struct {
						Properties struct {
							Type struct {
								Const string `json:"const"`
							} `json:"type"`
						} `json:"properties"`
					} `json:"if"`
					Then struct {
						Properties struct {
							Config struct {
								Properties map[string]any `json:"properties"`
							} `json:"config"`
						} `json:"properties"`
					} `json:"then"`
				} `json:"allOf"`
			} `json:"node"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	keys := map[string]map[string]any{}
	for _, v := range schema.Defs.Node.AllOf {
		keys[v.If.Properties.Type.Const] = v.Then.Properties.Config.Properties
	}
	for typ := range map[string]bool{"find_files": true, "replace": true, "compose": true, "command": true, "http": true, "check": true, "rollback": true, "wait": true} {
		if keys[typ] == nil {
			t.Errorf("Schema kennt Baustein %s nicht", typ)
		}
	}
	out := os.Getenv("FLOW_DOCS_OUT")
	for _, tpl := range flowTemplates {
		for _, n := range tpl.Graph.Nodes {
			for k := range n.Config {
				if _, ok := keys[n.Type][k]; !ok {
					t.Errorf("Vorlage %s, Baustein %s: Schlüssel %q fehlt im Schema", tpl.Key, n.ID, k)
				}
			}
		}
		if out != "" {
			doc := flowDocument{Schema: flowSchemaURL, Format: flowFormat, Version: flowFormatVersion, Name: tpl.Name, Description: tpl.Description, Params: tpl.Params, Nodes: tpl.Graph.Nodes, Edges: tpl.Graph.Edges}
			b, _ := json.MarshalIndent(doc, "", "  ")
			_ = os.WriteFile(filepath.Join(out, tpl.Key+".roster-flow.json"), b, 0o644)
		}
	}
}
