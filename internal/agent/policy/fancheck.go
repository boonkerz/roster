package policy

import (
	"fmt"
	"strings"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/shared"
)

// fanCheck prüft die Lüfter. Konfiguration:
//
//	sensor  – nur Lüfter, deren Name/Bezeichnung dies enthält (z. B. "cpu"); leer = alle
//	min_rpm – Failing unter dieser Drehzahl (optional; sonst nur Stillstand/Alarm/Chip-Minimum)
//
// Failing, wenn ein Lüfter steht, der Chip Alarm meldet oder die Drehzahl unter dem
// Minimum liegt. Unbelegte Anschlüsse erfasst der Collector gar nicht erst.
func fanCheck(c shared.CheckSpec) shared.CheckResult {
	return evalFans(c, collect.Fans())
}

func evalFans(c shared.CheckSpec, fans []shared.Fan) shared.CheckResult {
	if len(fans) == 0 {
		return unknownCheck(c, "keine Lüfter-Sensoren gefunden (unter Linux ggf. Treiber für den Mainboard-Chip laden, z. B. nct6775/it87; Server mit BMC melden Lüfter oft nur über IPMI)")
	}
	filter := strings.ToLower(strConfig(c, "sensor"))
	minRPM, hasMin := numConfig(c, "min_rpm")

	var problems []string
	checked, slowest, fastest := 0, -1, 0
	for _, f := range fans {
		if filter != "" && !strings.Contains(strings.ToLower(f.Sensor+" "+f.Label), filter) {
			continue
		}
		checked++
		if slowest < 0 || f.RPM < slowest {
			slowest = f.RPM
		}
		if f.RPM > fastest {
			fastest = f.RPM
		}
		switch p := f.Problem(); {
		case p != "":
			problems = append(problems, fmt.Sprintf("%s %s (%d U/min)", f.Label, p, f.RPM))
		case hasMin && float64(f.RPM) < minRPM:
			problems = append(problems, fmt.Sprintf("%s zu langsam (%d < %.0f U/min)", f.Label, f.RPM, minRPM))
		}
	}
	if checked == 0 {
		return unknownCheck(c, fmt.Sprintf("kein Lüfter passt zu %q", strConfig(c, "sensor")))
	}
	if len(problems) > 0 {
		return shared.CheckResult{CheckID: c.ID, Status: "failing", Value: float64(len(problems)), Output: trunc(strings.Join(problems, ", "))}
	}
	out := fmt.Sprintf("%d Lüfter in Ordnung (%d U/min)", checked, fastest)
	if slowest != fastest {
		out = fmt.Sprintf("%d Lüfter in Ordnung (%d–%d U/min)", checked, slowest, fastest)
	}
	return shared.CheckResult{CheckID: c.ID, Status: "passing", Value: 0, Output: out}
}
