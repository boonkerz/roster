package policy

import (
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func secSpec(cfg map[string]any) shared.CheckSpec {
	return shared.CheckSpec{ID: "1", Config: cfg}
}

func TestEvalSmart(t *testing.T) {
	cases := []struct {
		name, js, want string
	}{
		{"alle OK", `{"disks":[{"name":"sda","health":"OK"},{"name":"sdb","health":"OK"}]}`, "passing"},
		{"ein Fehler", `{"disks":[{"name":"sda","health":"OK"},{"name":"sdb","health":"Fehler"}]}`, "failing"},
		{"Warnung", `{"disks":[{"name":"sda","health":"Warnung"}]}`, "failing"},
		{"keine Disks -> unknown", `{"disks":[],"info":"smartctl fehlt"}`, "unknown"},
		{"error -> unknown", `{"error":"kaputt"}`, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if r := evalSmart(secSpec(nil), tc.js); r.Status != tc.want {
				t.Fatalf("Status=%q want %q (%s)", r.Status, tc.want, r.Output)
			}
		})
	}
}

func TestEvalAV(t *testing.T) {
	ok := `{"product":"Defender","enabled":true,"realtime":true,"signature_age_days":2}`
	if r := evalAV(secSpec(nil), ok); r.Status != "passing" {
		t.Fatalf("aktiv sollte passing sein: %+v", r)
	}
	if r := evalAV(secSpec(nil), `{"product":"Defender","enabled":true,"realtime":false,"signature_age_days":2}`); r.Status != "failing" {
		t.Fatalf("kein Echtzeitschutz -> failing: %+v", r)
	}
	if r := evalAV(secSpec(nil), `{"product":"Defender","enabled":true,"realtime":true,"signature_age_days":30}`); r.Status != "failing" {
		t.Fatalf("alte Signaturen -> failing: %+v", r)
	}
	// höhere Schwelle -> passing
	if r := evalAV(secSpec(map[string]any{"max_signature_age": 60.0}), `{"product":"Defender","enabled":true,"realtime":true,"signature_age_days":30}`); r.Status != "passing" {
		t.Fatalf("max_signature_age 60 -> passing: %+v", r)
	}
	// Info/kein AV -> unknown
	if r := evalAV(secSpec(nil), `{"info":"Kein klassischer Virenschutz"}`); r.Status != "unknown" {
		t.Fatalf("info -> unknown: %+v", r)
	}
}

func TestEvalBitlocker(t *testing.T) {
	if r := evalBitlocker(secSpec(nil), `{"volumes":[{"mount_point":"C:","protection":"On"}]}`); r.Status != "passing" {
		t.Fatalf("verschlüsselt -> passing: %+v", r)
	}
	if r := evalBitlocker(secSpec(nil), `{"volumes":[{"mount_point":"C:","protection":"On"},{"mount_point":"D:","protection":"Off"}]}`); r.Status != "failing" {
		t.Fatalf("Off -> failing: %+v", r)
	}
	if r := evalBitlocker(secSpec(nil), `{"volumes":[],"info":"nur Windows"}`); r.Status != "unknown" {
		t.Fatalf("keine Volumes -> unknown: %+v", r)
	}
}
