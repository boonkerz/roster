package policy

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/shared"
)

// temperatureCheck bewertet die Temperatursensoren. Konfiguration:
//
//	sensor – nur Sensoren, deren Name/Bezeichnung dies enthält (z. B. "cpu", "nvme"); leer = alle
//	warn   – Warnung ab dieser Temperatur in °C (optional)
//	crit   – Failing ab dieser Temperatur in °C (optional)
//
// Ohne warn/crit gelten die Schwellen, die der Chip selbst meldet (Sensor.Status):
// kritisch ab „crit", Warnung ab „max" bzw. 10 °C unter „crit". Sensoren ganz ohne
// Schwellen werden dann nicht bewertet.
func temperatureCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	return evalTemperature(c, collect.Temperatures(ctx))
}

// evalTemperature wertet die Sensoren gegen die Konfiguration aus (rein, testbar).
func evalTemperature(c shared.CheckSpec, temps []shared.Temperature) shared.CheckResult {
	if len(temps) == 0 {
		return unknownCheck(c, "keine Temperatursensoren gefunden (VM oder vom System nicht unterstützt)")
	}
	filter := strings.ToLower(strConfig(c, "sensor"))
	warn, hasWarn := numConfig(c, "warn")
	crit, hasCrit := numConfig(c, "crit")
	custom := hasWarn || hasCrit

	type hit struct {
		t     shared.Temperature
		level int // 2 kritisch, 1 Warnung
		limit float64
	}
	var hits []hit
	var hottest *shared.Temperature
	rated := 0
	for i := range temps {
		t := temps[i]
		if filter != "" && !strings.Contains(strings.ToLower(t.Sensor+" "+t.Label), filter) {
			continue
		}
		if hottest == nil || t.Celsius > hottest.Celsius {
			hottest = &temps[i]
		}
		switch {
		case custom:
			rated++
			if hasCrit && t.Celsius >= crit {
				hits = append(hits, hit{t, 2, crit})
			} else if hasWarn && t.Celsius >= warn {
				hits = append(hits, hit{t, 1, warn})
			}
		default:
			switch t.Status() {
			case "unknown":
				continue
			case "critical":
				hits = append(hits, hit{t, 2, t.Critical})
			case "warn":
				hits = append(hits, hit{t, 1, t.WarnAt()})
			}
			rated++
		}
	}

	if hottest == nil {
		return unknownCheck(c, fmt.Sprintf("kein Sensor passt zu %q", strConfig(c, "sensor")))
	}
	if rated == 0 {
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Value: hottest.Celsius,
			Output: fmt.Sprintf("Sensoren melden keine Schwellen – warn/crit in °C setzen (höchste: %s %.0f °C)", hottest.Label, hottest.Celsius)}
	}
	if len(hits) == 0 {
		return shared.CheckResult{CheckID: c.ID, Status: "passing", Value: hottest.Celsius,
			Output: fmt.Sprintf("höchste: %s %.0f °C (%d Sensoren bewertet)", hottest.Label, hottest.Celsius, rated)}
	}

	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].level != hits[j].level {
			return hits[i].level > hits[j].level
		}
		return hits[i].t.Celsius > hits[j].t.Celsius
	})
	parts := make([]string, 0, len(hits))
	for _, h := range hits {
		kind := "Warnung"
		if h.level == 2 {
			kind = "kritisch"
		}
		parts = append(parts, fmt.Sprintf("%s %.0f °C (%s ab %.0f °C)", h.t.Label, h.t.Celsius, kind, h.limit))
	}
	status := "warning"
	if hits[0].level == 2 {
		status = "failing"
	}
	return shared.CheckResult{CheckID: c.ID, Status: status, Value: hits[0].t.Celsius, Output: trunc(strings.Join(parts, ", "))}
}
