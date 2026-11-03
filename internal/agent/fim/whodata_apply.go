package fim

import (
	"strings"

	"github.com/boonkerz/roster/internal/shared"
)

// ApplyWho trägt Verursacher aus who in die Ereignisse ein (Pfadvergleich je Plattform).
func ApplyWho(events []shared.FIMEvent, who map[string]Who) int {
	if len(who) == 0 {
		return 0
	}
	n := 0
	for i := range events {
		p := events[i].Path
		if whoFold {
			p = strings.ToLower(p)
		}
		if w, ok := who[p]; ok && events[i].Change != "baseline" && events[i].Change != "limit" {
			events[i].User, events[i].Process = w.User, w.Process
			n++
		}
	}
	return n
}
