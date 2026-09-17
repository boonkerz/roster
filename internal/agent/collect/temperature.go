package collect

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/sensors"

	"github.com/boonkerz/roster/internal/shared"
)

// Temperatursensoren über gopsutil: Linux liest /sys/class/hwmon (inkl. Schwellen
// der Chips), FreeBSD sysctl, macOS SMC, Windows WMI-Thermalzonen (liefern auf vielen
// Boards nichts). VMs haben meist gar keine Sensoren – dann bleibt die Liste leer.

// Temperatures liefert alle plausiblen Sensorwerte, CPU zuerst. Unter Linux liest der
// Agent /sys/class/hwmon selbst (Chips gleichen Namens – z. B. zwei NVMe-SSDs – lassen
// sich nur so auseinanderhalten), sonst bzw. ohne hwmon-Temperaturen gopsutil.
func Temperatures(ctx context.Context) []shared.Temperature {
	if temps := readHwmonTemps(hwmonRoot); len(temps) > 0 {
		return temps
	}
	stats, err := sensors.TemperaturesWithContext(ctx)
	if err != nil && len(stats) == 0 {
		return nil // gopsutil meldet Teilfehler als Warnung UND liefert die lesbaren Werte
	}
	return normalizeTemperatures(stats)
}

// hwmonChip ist ein hwmon-Verzeichnis mit Treibername und zugehörigem Gerät.
type hwmonChip struct {
	dir    string
	name   string // Treiber, z. B. nvme, k10temp
	device string // Kernel-Gerät, z. B. nvme0, 0000:00:18.3
}

// readHwmonTemps liest alle temp*_input unter root. Gleichnamige Chips bekommen
// eindeutige Namen: NVMe-SSDs ihren Gerätenamen (nvme0 – wie in Proxmox/lsblk; die
// hwmon-Reihenfolge passt dazu nicht zwingend), andere eine laufende Nummer.
func readHwmonTemps(root string) []shared.Temperature {
	dirs, _ := filepath.Glob(filepath.Join(root, "hwmon*"))
	chips := make([]hwmonChip, 0, len(dirs))
	count := map[string]int{}
	for _, d := range dirs {
		name := readTrim(filepath.Join(d, "name"))
		if name == "" {
			continue
		}
		dev := ""
		if target, err := filepath.EvalSymlinks(filepath.Join(d, "device")); err == nil {
			dev = filepath.Base(target)
		}
		chips = append(chips, hwmonChip{dir: d, name: name, device: dev})
		count[name]++
	}
	sort.SliceStable(chips, func(i, j int) bool {
		if chips[i].name != chips[j].name {
			return chips[i].name < chips[j].name
		}
		return natLess(chips[i].device, chips[j].device)
	})

	var out []shared.Temperature
	instance := map[string]int{}
	for _, c := range chips {
		instance[c.name]++
		chipName, class := chipDisplayName(c.name)
		chipKey := c.name
		if count[c.name] > 1 {
			if c.name == "nvme" && strings.HasPrefix(c.device, "nvme") {
				chipKey, chipName = c.device, c.device
			} else {
				n := strconv.Itoa(instance[c.name])
				chipKey, chipName = c.name+n, chipName+" "+n
			}
		}

		inputs, _ := filepath.Glob(filepath.Join(c.dir, "temp*_input"))
		sort.Slice(inputs, func(i, j int) bool { return natLess(inputs[i], inputs[j]) })
		for _, in := range inputs {
			base := strings.TrimSuffix(filepath.Base(in), "_input") // temp3
			milli, ok := readInt(in)
			if !ok {
				continue
			}
			celsius := float64(milli) / 1000
			if celsius <= 0 || celsius > 150 {
				continue
			}
			label := readTrim(filepath.Join(c.dir, base+"_label"))
			t := shared.Temperature{Class: class, Celsius: round1(celsius)}
			switch {
			case label != "":
				t.Sensor = chipKey + "_" + strings.ReplaceAll(strings.ToLower(label), " ", "_")
				t.Label = chipName + " " + label
			case len(inputs) > 1:
				idx := strings.TrimPrefix(base, "temp")
				t.Sensor, t.Label = chipKey+"_"+base, chipName+" "+idx
			default:
				t.Sensor, t.Label = chipKey, chipName
			}
			if v, ok := readInt(filepath.Join(c.dir, base+"_max")); ok {
				t.High = plausibleLimit(float64(v) / 1000)
			}
			if v, ok := readInt(filepath.Join(c.dir, base+"_crit")); ok {
				t.Critical = plausibleLimit(float64(v) / 1000)
			}
			applyKnownLimits(c.name, &t)
			out = append(out, t)
		}
	}
	order := map[string]int{"cpu": 0, "gpu": 1, "disk": 2, "board": 3, "other": 4}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Class] < order[out[j].Class] })
	return out
}

// applyKnownLimits ergänzt Grenzwerte für Chips, die selbst keine melden. AMD-CPUs
// (k10temp/zenpower) liefern nur den Messwert; AMD gibt für aktuelle Ryzen eine
// maximale Betriebstemperatur (Tjmax) von 95 °C an – Warnung 10 °C darunter.
func applyKnownLimits(chip string, t *shared.Temperature) {
	if t.High > 0 || t.Critical > 0 {
		return
	}
	switch chip {
	case "k10temp", "zenpower":
		t.High, t.Critical = 85, 95
	}
}

// chipDisplayName liefert Anzeigename und Klasse eines Treibers.
func chipDisplayName(chip string) (string, string) {
	lower := strings.ToLower(chip)
	for _, c := range tempChips {
		if strings.HasPrefix(lower, c.prefix) {
			return c.name, c.class
		}
	}
	return humanWords(lower), "other"
}

// natLess vergleicht Zeichenketten mit Zahlen natürlich (temp2 < temp10, nvme2 < nvme10).
func natLess(a, b string) bool {
	for a != "" && b != "" {
		da, db := leadingDigits(a), leadingDigits(b)
		if da != "" && db != "" {
			na, _ := strconv.Atoi(da)
			nb, _ := strconv.Atoi(db)
			if na != nb {
				return na < nb
			}
			a, b = a[len(da):], b[len(db):]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func leadingDigits(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}

// normalizeTemperatures filtert Unsinnswerte, vergibt lesbare Namen, macht doppelte
// Schlüssel eindeutig (z. B. meldet dell_smm zehn Sensoren ohne Label) und sortiert.
func normalizeTemperatures(stats []sensors.TemperatureStat) []shared.Temperature {
	out := make([]shared.Temperature, 0, len(stats))
	seen := map[string]int{}
	for _, st := range stats {
		if st.Temperature <= 0 || st.Temperature > 150 {
			continue // unbelegte Kanäle (0, -273) und Messfehler
		}
		key := strings.TrimSpace(st.SensorKey)
		if key == "" {
			key = "sensor"
		}
		seen[key]++
		label, class := temperatureLabel(key)
		if n := seen[key]; n > 1 {
			key += "#" + strconv.Itoa(n)
			label += " " + strconv.Itoa(n)
		}
		out = append(out, shared.Temperature{
			Sensor: key, Label: label, Class: class,
			Celsius:  round1(st.Temperature),
			High:     plausibleLimit(st.High),
			Critical: plausibleLimit(st.Critical),
		})
	}
	order := map[string]int{"cpu": 0, "gpu": 1, "disk": 2, "board": 3, "other": 4}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Class] < order[out[j].Class] })
	return out
}

func plausibleLimit(v float64) float64 {
	if v <= 0 || v > 200 {
		return 0
	}
	return round1(v)
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// tempChips ordnet Treibernamen (Präfix des Schlüssels) einer Klasse und Bezeichnung zu.
var tempChips = []struct{ prefix, name, class string }{
	{"coretemp", "CPU", "cpu"}, {"k10temp", "CPU", "cpu"}, {"zenpower", "CPU", "cpu"},
	{"cpu_thermal", "CPU", "cpu"}, {"cpu", "CPU", "cpu"}, {"x86_pkg_temp", "CPU Package", "cpu"},
	{"amdgpu", "GPU", "gpu"}, {"radeon", "GPU", "gpu"}, {"nouveau", "GPU", "gpu"}, {"i915", "GPU", "gpu"}, {"xe", "GPU", "gpu"},
	{"nvme", "NVMe", "disk"}, {"drivetemp", "Festplatte", "disk"},
	{"acpitz", "ACPI-Zone", "board"}, {"pch_", "Chipsatz", "board"}, {"nct6", "Mainboard", "board"},
	{"it87", "Mainboard", "board"}, {"f71", "Mainboard", "board"}, {"asus", "Mainboard", "board"},
	{"dell_smm", "Dell SMM", "board"}, {"thinkpad", "ThinkPad", "board"}, {"applesmc", "SMC", "board"},
	{"iwlwifi", "WLAN", "other"}, {"mt7921", "WLAN", "other"}, {"ath", "WLAN", "other"},
	{"bnxt", "Netzwerkkarte", "other"}, {"ixgbe", "Netzwerkkarte", "other"}, {"mlx", "Netzwerkkarte", "other"},
}

// temperatureLabel macht aus einem Treiberschlüssel eine lesbare Bezeichnung:
// "coretemp_package_id_0" → "CPU Package 0", "nvme_composite" → "NVMe Composite".
func temperatureLabel(key string) (string, string) {
	lower := strings.ToLower(key)
	// Windows-WMI: "ACPI\ThermalZone\TZ00_0" → "Thermal Zone TZ00"
	if i := strings.LastIndex(lower, `thermalzone\`); i >= 0 {
		zone := key[i+len(`thermalzone\`):]
		if j := strings.LastIndex(zone, "_"); j > 0 {
			zone = zone[:j]
		}
		return "Thermal Zone " + zone, "board"
	}
	for _, c := range tempChips {
		if !strings.HasPrefix(lower, c.prefix) {
			continue
		}
		rest := strings.TrimPrefix(lower, c.prefix)
		if strings.HasSuffix(c.prefix, "_") { // pch_cannonlake: Plattformname weglassen
			rest = ""
		} else if i := strings.Index(rest, "_"); i >= 0 {
			rest = rest[i+1:] // Instanznummer des Treibers (iwlwifi_1) bzw. Trenner
		} else {
			rest = ""
		}
		rest = strings.ReplaceAll(rest, "id_", "")
		if words := humanWords(rest); words != "" && !(c.class == "other" && allDigits(rest)) {
			return c.name + " " + words, c.class
		}
		return c.name, c.class
	}
	return humanWords(lower), "other"
}

// humanWords: "package_id_0" → "Package 0"; Abkürzungen wie "tctl" bleiben lesbar.
func humanWords(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' || r == ' ' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// HeadlineTemperature liefert die Leittemperatur fürs Verlaufsdiagramm: die höchste
// CPU-Temperatur, ohne CPU-Sensoren die höchste überhaupt; nil ohne Sensoren.
func HeadlineTemperature(temps []shared.Temperature) *float64 {
	var best *float64
	for _, class := range []string{"cpu", ""} {
		for _, t := range temps {
			if class != "" && t.Class != class {
				continue
			}
			if best == nil || t.Celsius > *best {
				v := t.Celsius
				best = &v
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}
