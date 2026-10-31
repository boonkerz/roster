//go:build darwin

package authlog

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Collect liest sshd/sudo/su-Meldungen aus dem Unified Log ab dem gemerkten Zeitpunkt
// (best effort; `log show` ist langsam, daher höchstens eine Stunde rückwärts).
func Collect(ctx context.Context, stateDir string) []shared.AuthEvent {
	cur := loadCursor(stateDir)
	start := time.Now().Add(-time.Hour)
	if cur.LastTime != "" {
		if t, err := time.Parse(time.RFC3339Nano, cur.LastTime); err == nil && t.After(start) {
			start = t
		}
	}
	out, err := exec.CommandContext(ctx, "log", "show", "--style", "json", "--start", start.Format("2006-01-02 15:04:05"),
		"--predicate", `process == "sshd" OR process == "sshd-session" OR process == "sudo" OR process == "su" OR process == "login"`).Output()
	if err != nil {
		return nil
	}
	var rows []struct {
		Timestamp string `json:"timestamp"`
		Process   string `json:"process"`
		Message   string `json:"eventMessage"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return nil
	}
	var events []shared.AuthEvent
	last := cur.LastTime
	for _, r := range rows {
		ts, err := time.Parse("2006-01-02 15:04:05.000000-0700", r.Timestamp)
		if err != nil {
			continue
		}
		if !ts.After(start) {
			continue
		}
		if ev, ok := ParseMessage(r.Process, r.Message, ts); ok {
			ev.RecordID = "m:" + shortHash(r.Timestamp+r.Message)
			events = append(events, ev)
		}
		last = ts.Format(time.RFC3339Nano)
		if len(events) >= maxBatch {
			break
		}
	}
	if last != cur.LastTime {
		cur.LastTime = last
		saveCursor(stateDir, cur)
	}
	return events
}
