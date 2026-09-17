package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHwmon baut einen sysfs-Ausschnitt nach: Datei -> Inhalt.
func fakeHwmon(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if target, ok := strings.CutPrefix(content, "<link>"); ok { // device-Symlink wie in sysfs
			dst := filepath.Join(root, target)
			if err := os.MkdirAll(dst, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(dst, p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if content == "<dir>" { // Lesefehler simulieren (wie EINVAL beim dell_smm-Label)
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestReadFans(t *testing.T) {
	fanSeen.spinning = map[string]bool{}
	root := fakeHwmon(t, map[string]string{
		"hwmon0/name":        "nct6798",
		"hwmon0/fan1_input":  "1180",
		"hwmon0/fan1_label":  "CPU Fan",
		"hwmon0/pwm1":        "128",
		"hwmon0/fan2_input":  "0", // unbelegter Anschluss
		"hwmon0/fan3_input":  "0", // steht, aber Mindestdrehzahl gesetzt → melden
		"hwmon0/fan3_min":    "300",
		"hwmon0/fan4_input":  "650",
		"hwmon0/fan4_alarm":  "1",
		"hwmon1/name":        "dell_smm",
		"hwmon1/fan1_input":  "4514",
		"hwmon1/fan1_label":  "<dir>",
		"hwmon1/fan1_max":    "5100",
		"hwmon1/fan1_min":    "0",
		"hwmon1/pwm1":        "255",
		"hwmon2/name":        "coretemp",
		"hwmon2/temp1_input": "45000",
	})

	fans := readFans(root)
	byKey := map[string]int{}
	for i, f := range fans {
		byKey[f.Sensor] = i
	}
	if _, ok := byKey["nct6798_fan2"]; ok {
		t.Error("unbelegter Anschluss (nie gedreht) darf nicht erscheinen")
	}
	if len(fans) != 4 {
		t.Fatalf("%d Lüfter, erwartet 4: %+v", len(fans), fans)
	}

	cpu := fans[byKey["nct6798_fan1"]]
	if cpu.Label != "CPU Fan" || cpu.RPM != 1180 || cpu.Percent == nil || *cpu.Percent != 50 || cpu.Problem() != "" {
		t.Errorf("CPU-Lüfter: %+v", cpu)
	}
	if f := fans[byKey["nct6798_fan3"]]; f.Label != "Mainboard Lüfter 3" || f.Min != 300 || f.Problem() != "steht" {
		t.Errorf("stehender Lüfter mit Mindestdrehzahl: %+v (%s)", f, f.Problem())
	}
	if f := fans[byKey["nct6798_fan4"]]; !f.Alarm || f.Problem() != "Alarm" {
		t.Errorf("Alarm: %+v", f)
	}
	if f := fans[byKey["dell_smm_fan1"]]; f.Label != "Dell SMM Lüfter 1" || f.Max != 5100 || f.Min != 0 || *f.Percent != 100 {
		t.Errorf("dell_smm (Label unlesbar): %+v", f)
	}

	// CPU-Lüfter bleibt stehen → wird jetzt als „steht" gemeldet, statt zu verschwinden.
	if err := os.WriteFile(filepath.Join(root, "hwmon0/fan1_input"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stopped *int
	for i, f := range readFans(root) {
		if f.Sensor == "nct6798_fan1" {
			i := i
			stopped = &i
			if f.Problem() != "steht" {
				t.Errorf("stehengebliebener Lüfter: %+v", f)
			}
		}
	}
	if stopped == nil {
		t.Error("Lüfter, der gedreht hat und jetzt steht, muss gemeldet werden")
	}
}

func TestReadFansNoHwmon(t *testing.T) {
	if fans := readFans(filepath.Join(t.TempDir(), "gibtsnicht")); len(fans) != 0 {
		t.Errorf("ohne hwmon keine Lüfter: %+v", fans)
	}
}
