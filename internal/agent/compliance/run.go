package compliance

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/boonkerz/roster/internal/shared"
	cat "github.com/boonkerz/roster/internal/shared/compliance"
)

// evaluator ist die Prüffunktion einer Regel.
type evaluator func(e Env) Result

// evaluators sammelt die Prüffunktionen aller Plattformen (Registrierung in den rules_*.go).
var evaluators = map[string]evaluator{}

// Run prüft alle Regeln, die für Plattform und Profil gelten und nicht abgeschaltet sind.
// Panics einzelner Regeln werden als Status error gemeldet.
func Run(ctx context.Context, spec *shared.ComplianceSpec, env Env) shared.ComplianceReport {
	profile := "server"
	var disabled map[string]bool
	if spec != nil {
		if spec.Profile != "" {
			profile = spec.Profile
		}
		disabled = map[string]bool{}
		for _, id := range spec.Disabled {
			disabled[id] = true
		}
	}
	rep := shared.ComplianceReport{ScannedAt: time.Now().UTC(), Profile: profile}
	for _, r := range cat.Catalog {
		if ctx.Err() != nil {
			break
		}
		if !r.Applies(env.OS(), profile) || disabled[r.ID] {
			continue
		}
		fn, ok := evaluators[r.ID]
		if !ok {
			rep.Results = append(rep.Results, shared.ComplianceResult{RuleID: r.ID, Status: "na", Evidence: "auf dieser Plattform nicht implementiert"})
			continue
		}
		res := safeEval(fn, env)
		rep.Results = append(rep.Results, shared.ComplianceResult{RuleID: r.ID, Status: res.Status, Evidence: trunc(res.Evidence, 400)})
	}
	sort.SliceStable(rep.Results, func(i, j int) bool { return rep.Results[i].RuleID < rep.Results[j].RuleID })
	return rep
}

func safeEval(fn evaluator, env Env) (res Result) {
	defer func() {
		if r := recover(); r != nil {
			res = errRes(fmt.Sprintf("Regel abgestürzt: %v", r))
		}
	}()
	return fn(env)
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

// Score berechnet den Anteil bestandener Regeln in Prozent (na/error zählen nicht).
func Score(results []shared.ComplianceResult) (score, passed, failed int) {
	for _, r := range results {
		switch r.Status {
		case "pass":
			passed++
		case "fail":
			failed++
		}
	}
	if passed+failed == 0 {
		return 100, 0, 0
	}
	return passed * 100 / (passed + failed), passed, failed
}
