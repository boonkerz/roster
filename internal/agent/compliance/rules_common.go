package compliance

import "strings"

// Plattformübergreifende Regeln (nutzen die vorhandenen Collectoren).
func init() {
	evaluators["auto_updates_enabled"] = func(e Env) Result {
		switch s := e.AutoUpdates(); s {
		case "enabled", "security":
			return pass("Status: " + s)
		case "download", "notify", "disabled":
			return fail("Status: " + s)
		default:
			return na("kein bekannter Auto-Update-Mechanismus")
		}
	}
	evaluators["reboot_not_pending"] = func(e Env) Result {
		pending, known := e.RebootPending()
		if !known {
			return na("nicht ermittelbar")
		}
		if pending {
			return fail("Neustart ausstehend")
		}
		return pass("kein Neustart ausstehend")
	}
}

// lines liefert die getrimmten, nicht-leeren, nicht-kommentierten Zeilen.
func lines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, l)
	}
	return out
}
