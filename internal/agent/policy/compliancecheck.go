package policy

import (
	"fmt"
	"strings"

	"github.com/boonkerz/roster/internal/shared"
	cat "github.com/boonkerz/roster/internal/shared/compliance"
)

// ComplianceReport liefert den zuletzt erstellten Compliance-Bericht (setzt das
// Hauptprogramm); nil = noch keine Prüfung gelaufen.
var ComplianceReport func() *shared.ComplianceReport

// complianceCheck: failing, wenn der Score unter min_score liegt (Standard 80) oder
// (fail_on_high) eine Regel mit Schweregrad high fehlschlägt; unknown ohne Bericht.
func complianceCheck(c shared.CheckSpec) shared.CheckResult {
	if ComplianceReport == nil {
		return unknownCheck(c, "Compliance-Prüfung nicht verfügbar")
	}
	rep := ComplianceReport()
	if rep == nil || len(rep.Results) == 0 {
		return unknownCheck(c, "noch keine Compliance-Prüfung (in der Richtlinie aktivieren)")
	}
	minScore := 80.0
	if v, ok := numConfig(c, "min_score"); ok {
		minScore = v
	}
	failOnHigh := boolConfig(c, "fail_on_high", false)
	pass, fail := 0, 0
	var highFails []string
	for _, r := range rep.Results {
		switch r.Status {
		case "pass":
			pass++
		case "fail":
			fail++
			if rule, ok := cat.ByID(r.RuleID); ok && rule.Severity == "high" {
				highFails = append(highFails, rule.Title)
			}
		}
	}
	score := 100
	if pass+fail > 0 {
		score = pass * 100 / (pass + fail)
	}
	out := fmt.Sprintf("Score %d %% (%d bestanden, %d fehlgeschlagen)", score, pass, fail)
	if float64(score) < minScore {
		return failCheck(c, float64(score), out+fmt.Sprintf(" – unter %.0f %%", minScore))
	}
	if failOnHigh && len(highFails) > 0 {
		return failCheck(c, float64(score), out+" – kritische Regeln: "+strings.Join(highFails, ", "))
	}
	return passCheck(c, float64(score), out)
}
