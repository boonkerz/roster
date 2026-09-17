package policy

import (
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func TestEvalFans(t *testing.T) {
	fans := []shared.Fan{
		{Sensor: "nct6798_fan1", Label: "CPU Fan", RPM: 1180},
		{Sensor: "nct6798_fan2", Label: "Mainboard Lüfter 2", RPM: 640},
		{Sensor: "nct6798_fan3", Label: "Mainboard Lüfter 3", RPM: 0, Min: 300},
	}
	spec := func(cfg map[string]any) shared.CheckSpec { return shared.CheckSpec{ID: "f", Config: cfg} }

	r := evalFans(spec(map[string]any{}), fans)
	if r.Status != "failing" || r.Output != "Mainboard Lüfter 3 steht (0 U/min)" {
		t.Errorf("stehender Lüfter: %s – %s", r.Status, r.Output)
	}
	r = evalFans(spec(map[string]any{"sensor": "cpu"}), fans)
	if r.Status != "passing" || r.Output != "1 Lüfter in Ordnung (1180 U/min)" {
		t.Errorf("Filter cpu: %s – %s", r.Status, r.Output)
	}
	r = evalFans(spec(map[string]any{"sensor": "nct6798_fan1", "min_rpm": float64(1500)}), fans)
	if r.Status != "failing" || !strings.Contains(r.Output, "CPU Fan zu langsam (1180 < 1500 U/min)") {
		t.Errorf("min_rpm: %s – %s", r.Status, r.Output)
	}
	r = evalFans(spec(map[string]any{"sensor": "fan2"}), fans)
	if r.Status != "passing" {
		t.Errorf("einzelner guter Lüfter: %s – %s", r.Status, r.Output)
	}
	if r := evalFans(spec(map[string]any{}), fans[:2]); r.Output != "2 Lüfter in Ordnung (640–1180 U/min)" {
		t.Errorf("Spanne: %s", r.Output)
	}
	if r := evalFans(spec(map[string]any{}), nil); r.Status != "unknown" || !strings.Contains(r.Output, "nct6775") {
		t.Errorf("ohne Lüfter unbekannt mit Hinweis: %s – %s", r.Status, r.Output)
	}
}
