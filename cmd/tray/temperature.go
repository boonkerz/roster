package main

import (
	"fmt"
	"sort"

	"github.com/boonkerz/roster/internal/shared"
)

// tempBadge beschreibt die Temperatur-Plakette einer Serverzeile. Gezeigt wird der
// auffälligste Sensor: ist einer im Warn-/Kritischbereich, dieser (mit Kurzname, wenn
// es nicht die CPU ist); sonst die heißeste CPU. Normalwerte bleiben neutral grau –
// eine Temperatur ist kein „bestanden", nur Grenzüberschreitungen bekommen Farbe.
func tempBadge(temps []shared.Temperature) (badgeInfo, bool) {
	if len(temps) == 0 {
		return badgeInfo{}, false
	}
	rank := map[string]int{"critical": 3, "warn": 2, "ok": 1, "unknown": 0}
	sorted := append([]shared.Temperature(nil), temps...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, rj := rank[sorted[i].Status()], rank[sorted[j].Status()]
		if ri != rj {
			return ri > rj
		}
		return sorted[i].Celsius > sorted[j].Celsius
	})
	worst := sorted[0]
	switch worst.Status() {
	case "critical":
		return badgeInfo{tempText(worst), colRed}, true
	case "warn":
		return badgeInfo{tempText(worst), colAmber}, true
	}
	var hottest *shared.Temperature
	for i := range sorted {
		if sorted[i].Class == "cpu" && (hottest == nil || sorted[i].Celsius > hottest.Celsius) {
			hottest = &sorted[i]
		}
	}
	if hottest == nil {
		hottest = &sorted[0] // keine CPU-Sensoren: heißester überhaupt (bereits vorne)
	}
	return badgeInfo{fmt.Sprintf("%.0f °C", hottest.Celsius), colLine}, true
}

// tempText nennt bei Auffälligkeiten den Sensor, außer es ist die CPU (der Normalfall).
func tempText(t shared.Temperature) string {
	if t.Class == "cpu" {
		return fmt.Sprintf("CPU %.0f °C", t.Celsius)
	}
	return fmt.Sprintf("%s %.0f °C", t.Label, t.Celsius)
}
