package collect

import (
	"testing"

	"github.com/shirou/gopsutil/v4/sensors"
)

// Echte gopsutil-Ausgabe eines Dell-Laptops (Intel) und typische Server-/AMD-Schlüssel.
var sampleSensors = []sensors.TemperatureStat{
	{SensorKey: "acpitz", Temperature: 25},
	{SensorKey: "nvme_composite", Temperature: 29.9, High: 69.8, Critical: 84.8},
	{SensorKey: "pch_cannonlake", Temperature: 45},
	{SensorKey: "dell_smm", Temperature: 30},
	{SensorKey: "dell_smm", Temperature: 80},
	{SensorKey: "coretemp_package_id_0", Temperature: 96, High: 100, Critical: 100},
	{SensorKey: "coretemp_core_0", Temperature: 96, High: 100, Critical: 100},
	{SensorKey: "iwlwifi_1", Temperature: 41},
	{SensorKey: "k10temp_tctl", Temperature: 55.25},
	{SensorKey: "amdgpu_edge", Temperature: 48, Critical: 100},
	{SensorKey: "nct6798_systin", Temperature: 0},               // unbelegt
	{SensorKey: "bogus", Temperature: -273.2},                   // Messfehler
	{SensorKey: "ACPI\\ThermalZone\\TZ00_0", Temperature: 27.8}, // Windows-WMI
}

func TestNormalizeTemperatures(t *testing.T) {
	got := normalizeTemperatures(sampleSensors)
	want := []struct{ sensor, label, class string }{
		{"coretemp_package_id_0", "CPU Package 0", "cpu"},
		{"coretemp_core_0", "CPU Core 0", "cpu"},
		{"k10temp_tctl", "CPU Tctl", "cpu"},
		{"amdgpu_edge", "GPU Edge", "gpu"},
		{"nvme_composite", "NVMe Composite", "disk"},
		{"acpitz", "ACPI-Zone", "board"},
		{"pch_cannonlake", "Chipsatz", "board"},
		{"dell_smm", "Dell SMM", "board"},
		{"dell_smm#2", "Dell SMM 2", "board"},
		{"ACPI\\ThermalZone\\TZ00_0", "Thermal Zone TZ00", "board"},
		{"iwlwifi_1", "WLAN", "other"},
	}
	if len(got) != len(want) {
		for _, g := range got {
			t.Logf("%s | %s | %s", g.Sensor, g.Label, g.Class)
		}
		t.Fatalf("%d Sensoren, erwartet %d (0 °C und -273 °C müssen raus)", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Sensor != w.sensor || got[i].Label != w.label || got[i].Class != w.class {
			t.Errorf("%d: %s | %s | %s – erwartet %s | %s | %s", i, got[i].Sensor, got[i].Label, got[i].Class, w.sensor, w.label, w.class)
		}
	}
	if got[2].Celsius != 55.3 {
		t.Errorf("auf eine Nachkommastelle runden: %v", got[2].Celsius)
	}
}

func TestTemperatureStatus(t *testing.T) {
	got := normalizeTemperatures(sampleSensors)
	byKey := map[string]string{}
	for _, s := range got {
		byKey[s.Sensor] = s.Status()
	}
	cases := map[string]string{
		"coretemp_package_id_0": "warn",    // 96 °C, max = crit = 100 → Warnung ab 90
		"nvme_composite":        "ok",      // 29.9 °C, max 69.8
		"acpitz":                "unknown", // keine Schwellen
		"amdgpu_edge":           "ok",      // 48 °C, crit 100 → Warnung ab 90
	}
	for key, want := range cases {
		if byKey[key] != want {
			t.Errorf("%s: %s, erwartet %s", key, byKey[key], want)
		}
	}
}

func TestHeadlineTemperature(t *testing.T) {
	got := normalizeTemperatures(sampleSensors)
	if h := HeadlineTemperature(got); h == nil || *h != 96 {
		t.Errorf("Leittemperatur soll die heißeste CPU sein (96, nicht Dell SMM 80): %v", h)
	}
	noCPU := normalizeTemperatures([]sensors.TemperatureStat{
		{SensorKey: "acpitz", Temperature: 40}, {SensorKey: "nvme_composite", Temperature: 52},
	})
	if h := HeadlineTemperature(noCPU); h == nil || *h != 52 {
		t.Errorf("ohne CPU die höchste überhaupt: %v", h)
	}
	if HeadlineTemperature(nil) != nil {
		t.Error("ohne Sensoren nil")
	}
}
