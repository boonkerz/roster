package collect

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/boonkerz/roster/internal/shared"
)

// Lüfter über /sys/class/hwmon (fanN_input in U/min, dazu _label/_min/_max/_alarm und
// pwmN). gopsutil bietet dafür nichts. Andere Systeme haben keine allgemeine
// Schnittstelle – dort bleibt die Liste leer (hwmonRoot existiert nicht).

var hwmonRoot = "/sys/class/hwmon"

// fanSeenSpinning merkt sich Lüfter, die seit Agent-Start schon einmal gedreht haben.
// Nur so lässt sich „Lüfter steht" von „am Anschluss hängt nichts" unterscheiden –
// beides meldet der Chip als 0 U/min.
var fanSeen = struct {
	sync.Mutex
	spinning map[string]bool
}{spinning: map[string]bool{}}

// Fans liefert die Lüfter des Systems.
func Fans() []shared.Fan {
	return readFans(hwmonRoot)
}

func readFans(root string) []shared.Fan {
	inputs, _ := filepath.Glob(filepath.Join(root, "hwmon*", "fan*_input"))
	sort.Slice(inputs, func(i, j int) bool { return natLess(inputs[i], inputs[j]) }) // hwmon2 vor hwmon10
	out := make([]shared.Fan, 0, len(inputs))
	seenKeys := map[string]int{}
	fanSeen.Lock()
	defer fanSeen.Unlock()
	for _, in := range inputs {
		dir := filepath.Dir(in)
		base := strings.TrimSuffix(filepath.Base(in), "_input") // fan2
		idx := strings.TrimPrefix(base, "fan")
		rpm, ok := readInt(in)
		if !ok || rpm < 0 || rpm > 30000 {
			continue
		}
		chip := readTrim(filepath.Join(dir, "name"))
		if chip == "" {
			chip = "hwmon"
		}
		label := readTrim(filepath.Join(dir, base+"_label")) // manche Treiber liefern hier EINVAL
		key := chip + "_" + base
		seenKeys[key]++
		if n := seenKeys[key]; n > 1 { // zwei Chips gleichen Namens
			key += "#" + strconv.Itoa(n)
		}

		f := shared.Fan{Sensor: key, Label: fanLabel(chip, idx, label), RPM: rpm}
		if v, ok := readInt(filepath.Join(dir, base+"_min")); ok && v > 0 {
			f.Min = v
		}
		if v, ok := readInt(filepath.Join(dir, base+"_max")); ok && v > 0 && v <= 30000 {
			f.Max = v
		}
		if v, ok := readInt(filepath.Join(dir, base+"_alarm")); ok && v != 0 {
			f.Alarm = true
		}
		if v, ok := readInt(filepath.Join(dir, "pwm"+idx)); ok && v >= 0 && v <= 255 {
			pct := (v*100 + 127) / 255
			f.Percent = &pct
		}

		if rpm > 0 {
			fanSeen.spinning[key] = true
		} else if !fanSeen.spinning[key] && f.Min == 0 && !f.Alarm {
			continue // nie gedreht, keine Mindestdrehzahl, kein Alarm: vermutlich unbelegt
		}
		out = append(out, f)
	}
	return out
}

// fanLabel bevorzugt das Chip-Label ("CPU Fan"), sonst Klasse + Nummer.
func fanLabel(chip, idx, label string) string {
	if label != "" {
		return label
	}
	name, _ := temperatureLabel(chip) // gleiche Chip-Zuordnung wie bei den Temperaturen
	if name == "" || strings.EqualFold(name, chip) {
		name = chip
	}
	return name + " Lüfter " + idx
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readInt(path string) (int, bool) {
	s := readTrim(path)
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	return v, err == nil
}
