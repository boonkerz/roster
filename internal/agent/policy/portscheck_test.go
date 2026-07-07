package policy

import (
	"strings"
	"testing"

	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

func TestParsePortSet(t *testing.T) {
	set := parsePortSet("22, 80 443;8080")
	for _, p := range []int{22, 80, 443, 8080} {
		if !set[p] {
			t.Fatalf("Port %d sollte enthalten sein: %v", p, set)
		}
	}
	if set[25] {
		t.Fatalf("25 sollte nicht enthalten sein")
	}
}

func TestEvalPorts(t *testing.T) {
	ports := []shared.ListenPort{
		{Proto: "tcp", Port: 22, Public: true},
		{Proto: "tcp", Port: 80, Public: true},
		{Proto: "tcp6", Port: 80, Public: true},
		{Proto: "tcp", Port: 8443, Public: true},  // nicht erlaubt -> failing
		{Proto: "tcp", Port: 3307, Public: false}, // lokal -> ignoriert
	}
	allowed := parsePortSet("22,80,443")

	res := evalPorts("chk", allowed, ports)
	if res.Status != "failing" {
		t.Fatalf("erwartete failing, bekam %s (%s)", res.Status, res.Output)
	}
	if !strings.Contains(res.Output, "8443") {
		t.Fatalf("Ausgabe sollte 8443 nennen: %q", res.Output)
	}
	if strings.Contains(res.Output, "3307") {
		t.Fatalf("lokaler Port 3307 darf nicht auslösen: %q", res.Output)
	}

	// Alles erlaubt -> passing.
	res2 := evalPorts("chk", parsePortSet("22,80,443,8443"), ports)
	if res2.Status != "passing" {
		t.Fatalf("erwartete passing, bekam %s (%s)", res2.Status, res2.Output)
	}
}
