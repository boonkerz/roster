package collect

import (
	"strings"
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

// TestReadHwmonTempsPVE nutzt die echten sysfs-Werte eines Proxmox-Mini-PCs (AMD Ryzen 5
// 7430U, zwei NVMe). Wichtig: hwmon0 gehört dort zu nvme1, hwmon1 zu nvme0.
func TestReadHwmonTempsPVE(t *testing.T) {
	root := fakeHwmon(t, map[string]string{
		"hwmon0/name": "nvme", "hwmon0/device": "<link>devices/nvme1",
		"hwmon0/temp1_input": "58850", "hwmon0/temp1_label": "Composite", "hwmon0/temp1_max": "82850", "hwmon0/temp1_crit": "89850", "hwmon0/temp1_min": "-150",
		"hwmon0/temp3_input": "81850", "hwmon0/temp3_label": "Sensor 2", "hwmon0/temp3_max": "",
		"hwmon1/name": "nvme", "hwmon1/device": "<link>devices/nvme0",
		"hwmon1/temp1_input": "55850", "hwmon1/temp1_label": "Composite", "hwmon1/temp1_max": "82850", "hwmon1/temp1_crit": "89850",
		"hwmon1/temp3_input": "82850", "hwmon1/temp3_label": "Sensor 2",
		"hwmon2/name": "k10temp", "hwmon2/device": "<link>devices/0000:00:18.3",
		"hwmon2/temp1_input": "68125", "hwmon2/temp1_label": "Tctl",
		"hwmon3/name": "amdgpu", "hwmon3/device": "<link>devices/0000:05:00.0",
		"hwmon3/temp1_input": "60000", "hwmon3/temp1_label": "edge",
	})
	got := readHwmonTemps(root)
	want := []struct {
		sensor, label, class string
		celsius, high, crit  float64
		status               string
	}{
		{"k10temp_tctl", "CPU Tctl", "cpu", 68.1, 85, 95, "ok"},
		{"amdgpu_edge", "GPU edge", "gpu", 60, 0, 0, "unknown"},
		{"nvme0_composite", "nvme0 Composite", "disk", 55.9, 82.9, 89.9, "ok"},
		{"nvme0_sensor_2", "nvme0 Sensor 2", "disk", 82.9, 0, 0, "unknown"},
		{"nvme1_composite", "nvme1 Composite", "disk", 58.9, 82.9, 89.9, "ok"},
		{"nvme1_sensor_2", "nvme1 Sensor 2", "disk", 81.9, 0, 0, "unknown"},
	}
	if len(got) != len(want) {
		for _, g := range got {
			t.Logf("%+v", g)
		}
		t.Fatalf("%d Sensoren, erwartet %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Sensor != w.sensor || g.Label != w.label || g.Class != w.class || g.Celsius != w.celsius ||
			g.High != w.high || g.Critical != w.crit || g.Status() != w.status {
			t.Errorf("%d: %+v (%s) – erwartet %+v", i, g, g.Status(), w)
		}
	}
	if h := HeadlineTemperature(got); h == nil || *h != 68.1 {
		t.Errorf("Leittemperatur soll die CPU sein: %v", h)
	}
}

func TestReadHwmonTempsSingleChipAndUnlabeled(t *testing.T) {
	root := fakeHwmon(t, map[string]string{
		"hwmon0/name": "nvme", "hwmon0/temp1_input": "40000", "hwmon0/temp1_label": "Composite",
		"hwmon1/name": "dell_smm", "hwmon1/temp1_input": "30000", "hwmon1/temp2_input": "80000", "hwmon1/temp10_input": "0",
		"hwmon2/name": "acpitz", "hwmon2/temp1_input": "25000",
	})
	got := readHwmonTemps(root)
	labels := make([]string, len(got))
	for i, g := range got {
		labels[i] = g.Label
	}
	if strings.Join(labels, "|") != "NVMe Composite|ACPI-Zone|Dell SMM 1|Dell SMM 2" {
		t.Errorf("Bezeichnungen: %v", labels)
	}
}
