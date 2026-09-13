package policy

import (
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

var sensorsFixture = []shared.Temperature{
	{Sensor: "coretemp_package_id_0", Label: "CPU Package 0", Class: "cpu", Celsius: 93, High: 100, Critical: 100},
	{Sensor: "coretemp_core_0", Label: "CPU Core 0", Class: "cpu", Celsius: 71, High: 100, Critical: 100},
	{Sensor: "nvme_composite", Label: "NVMe Composite", Class: "disk", Celsius: 86, High: 69.8, Critical: 84.8},
	{Sensor: "acpitz", Label: "ACPI-Zone", Class: "board", Celsius: 40},
}

func TestEvalTemperatureChipLimits(t *testing.T) {
	r := evalTemperature(shared.CheckSpec{ID: "c", Config: map[string]any{}}, sensorsFixture)
	if r.Status != "failing" || r.Value != 86 {
		t.Fatalf("NVMe über crit muss failing sein: %s %v – %s", r.Status, r.Value, r.Output)
	}
	if !strings.HasPrefix(r.Output, "NVMe Composite 86 °C (kritisch ab 85 °C), CPU Package 0 93 °C (Warnung ab 90 °C)") {
		t.Errorf("Ausgabe (kritisch zuerst): %s", r.Output)
	}
	if strings.Contains(r.Output, "ACPI") {
		t.Error("Sensor ohne Schwellen darf nicht bewertet werden")
	}
}

func TestEvalTemperatureCustomAndFilter(t *testing.T) {
	// Nur CPU, eigene Grenzen → Warnung (93 ≥ 90, < 95).
	r := evalTemperature(shared.CheckSpec{ID: "c", Config: map[string]any{"sensor": "cpu", "warn": float64(90), "crit": float64(95)}}, sensorsFixture)
	if r.Status != "warning" || !strings.Contains(r.Output, "CPU Package 0 93 °C (Warnung ab 90 °C)") {
		t.Errorf("CPU eigene Grenzen: %s – %s", r.Status, r.Output)
	}
	// Großzügige Grenze → grün mit höchstem Wert.
	r = evalTemperature(shared.CheckSpec{ID: "c", Config: map[string]any{"crit": float64(100)}}, sensorsFixture)
	if r.Status != "passing" || r.Output != "höchste: CPU Package 0 93 °C (4 Sensoren bewertet)" {
		t.Errorf("grün: %s – %s", r.Status, r.Output)
	}
	// Filter trifft nichts / nur Sensoren ohne Schwellen / gar keine Sensoren → unbekannt.
	for name, res := range map[string]shared.CheckResult{
		"filter leer":    evalTemperature(shared.CheckSpec{ID: "c", Config: map[string]any{"sensor": "gpu"}}, sensorsFixture),
		"ohne Schwellen": evalTemperature(shared.CheckSpec{ID: "c", Config: map[string]any{"sensor": "acpi"}}, sensorsFixture),
		"keine Sensoren": evalTemperature(shared.CheckSpec{ID: "c", Config: map[string]any{}}, nil),
	} {
		if res.Status != "unknown" {
			t.Errorf("%s: %s – %s", name, res.Status, res.Output)
		}
	}
}
