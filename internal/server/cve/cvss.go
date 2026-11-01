package cve

import (
	"math"
	"strconv"
	"strings"
)

// Level ordnet einem CVSS-Basiswert die qualitative Stufe zu (CVSS v3 Rating-Skala).
func Level(score float64) string {
	switch {
	case score >= 9:
		return "CRITICAL"
	case score >= 7:
		return "HIGH"
	case score >= 4:
		return "MEDIUM"
	case score > 0:
		return "LOW"
	}
	return ""
}

// LevelFromWord normalisiert qualitative Angaben der Distributionen (Ubuntu-Priority,
// Debian-Urgency, GHSA) auf die vier Stufen.
func LevelFromWord(w string) string {
	switch strings.ToLower(strings.TrimSpace(w)) {
	case "critical":
		return "CRITICAL"
	case "high", "important":
		return "HIGH"
	case "medium", "moderate":
		return "MEDIUM"
	case "low", "negligible", "minor":
		return "LOW"
	}
	return ""
}

// ScoreFromVector berechnet den Basiswert aus einem CVSS-Vektor (v2, v3.0, v3.1).
// CVSS v4 wird nicht gerechnet (Makrovektor-Tabelle) – dort kommt false zurück.
func ScoreFromVector(vec string) (float64, bool) {
	vec = strings.TrimSpace(vec)
	if f, err := strconv.ParseFloat(vec, 64); err == nil && f >= 0 && f <= 10 {
		return f, true
	}
	m := map[string]string{}
	version := ""
	for i, part := range strings.Split(vec, "/") {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		if i == 0 && strings.EqualFold(kv[0], "CVSS") {
			version = kv[1]
			continue
		}
		m[strings.ToUpper(kv[0])] = strings.ToUpper(kv[1])
	}
	switch {
	case strings.HasPrefix(version, "3."):
		return cvss3(m)
	case version == "" || strings.HasPrefix(version, "2"):
		// v2-Vektoren haben keinen CVSS:-Präfix, z. B. AV:N/AC:L/Au:N/C:P/I:P/A:P.
		return cvss2(m)
	}
	return 0, false
}

func cvss3(m map[string]string) (float64, bool) {
	av := map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2}[m["AV"]]
	ac := map[string]float64{"L": 0.77, "H": 0.44}[m["AC"]]
	ui := map[string]float64{"N": 0.85, "R": 0.62}[m["UI"]]
	cia := map[string]float64{"H": 0.56, "L": 0.22, "N": 0}
	c, i, a := cia[m["C"]], cia[m["I"]], cia[m["A"]]
	changed := m["S"] == "C"
	var pr float64
	if changed {
		pr = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}[m["PR"]]
	} else {
		pr = map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}[m["PR"]]
	}
	if av == 0 || ac == 0 || ui == 0 || pr == 0 {
		return 0, false
	}
	iss := 1 - (1-c)*(1-i)*(1-a)
	var impact float64
	if changed {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}
	expl := 8.22 * av * ac * pr * ui
	if impact <= 0 {
		return 0, true
	}
	if changed {
		return roundUp1(math.Min(1.08*(impact+expl), 10)), true
	}
	return roundUp1(math.Min(impact+expl, 10)), true
}

// roundUp1 ist das Roundup aus der CVSS-v3.1-Spezifikation (eine Nachkommastelle, aufgerundet).
func roundUp1(x float64) float64 {
	n := math.Round(x * 100000)
	if math.Mod(n, 10000) == 0 {
		return n / 100000
	}
	return (math.Floor(n/10000) + 1) / 10
}

func cvss2(m map[string]string) (float64, bool) {
	av := map[string]float64{"L": 0.395, "A": 0.646, "N": 1.0}[m["AV"]]
	ac := map[string]float64{"H": 0.35, "M": 0.61, "L": 0.71}[m["AC"]]
	au := map[string]float64{"M": 0.45, "S": 0.56, "N": 0.704}[m["AU"]]
	cia := map[string]float64{"N": 0, "P": 0.275, "C": 0.660}
	c, i, a := cia[m["C"]], cia[m["I"]], cia[m["A"]]
	if av == 0 || ac == 0 || au == 0 {
		return 0, false
	}
	impact := 10.41 * (1 - (1-c)*(1-i)*(1-a))
	expl := 20 * av * ac * au
	f := 1.176
	if impact == 0 {
		f = 0
	}
	return math.Round(((0.6*impact)+(0.4*expl)-1.5)*f*10) / 10, true
}
