package logmon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Weitere Quelltypen: journal (systemd-Journal, z. B. Kernel-Log für Firewall-Drops auf
// Hosts ohne rsyslog) und docker_events (Container-Ereignisse: exec, start, stop …).

var reJournalIdent = regexp.MustCompile(`^[A-Za-z0-9_.@:-]{1,64}$`)

// journalArgs baut die Auswahl: Target "kernel" → -k, sonst SYSLOG_IDENTIFIER=<target>.
func journalArgs(target string) ([]string, bool) {
	t := strings.TrimSpace(target)
	if t == "" || t == "kernel" || t == "kern" || t == "dmesg" {
		return []string{"-k"}, true
	}
	if !reJournalIdent.MatchString(t) {
		return nil, false
	}
	return []string{"SYSLOG_IDENTIFIER=" + t}, true
}

type journalLine struct {
	Message  json.RawMessage `json:"MESSAGE"`
	Cursor   string          `json:"__CURSOR"`
	Realtime string          `json:"__REALTIME_TIMESTAMP"`
}

// journalText liefert MESSAGE als Text (Byte-Arrays werden ausgelassen).
func (l journalLine) text() string {
	var s string
	if json.Unmarshal(l.Message, &s) == nil {
		return s
	}
	return ""
}

func (l journalLine) time(now time.Time) time.Time {
	if us, err := strconv.ParseInt(l.Realtime, 10, 64); err == nil && us > 0 {
		return time.UnixMicro(us)
	}
	return now
}

// collectJournal liest neue Journal-Einträge ab dem gemerkten Cursor (erster Lauf: 10 min).
func collectJournal(ctx context.Context, src shared.LogSource, cur cursors, now time.Time, budget int) ([]shared.AuthEvent, int, string) {
	sel, ok := journalArgs(src.Target)
	if !ok {
		return nil, 0, "ungültiges Journal-Ziel " + src.Target
	}
	key := "j:" + src.ID
	cs := cur[key]
	if cs == nil {
		cs = &fileCursor{}
		cur[key] = cs
	}
	cs.Seen = now.Format(time.RFC3339)
	args := []string{"-o", "json", "--no-pager", "-q"}
	if cs.Since != "" {
		args = append(args, "--after-cursor="+cs.Since)
	} else {
		args = append(args, "--since=-10min")
	}
	args = append(args, sel...)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "journalctl", args...).Output()
	if err != nil && len(out) == 0 {
		return nil, 0, shortErr(err).Error()
	}
	var events []shared.AuthEvent
	for _, raw := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var jl journalLine
		if json.Unmarshal([]byte(raw), &jl) != nil {
			continue
		}
		if jl.Cursor != "" {
			cs.Since = jl.Cursor
		}
		text := jl.text()
		if text == "" {
			continue
		}
		if ev, ok := Parse(src, Line{Text: text, Time: jl.time(now)}, now); ok {
			ev.RecordID = "jl:" + short(key) + ":" + short(jl.Cursor)
			events = append(events, ev)
			if len(events) >= budget {
				break
			}
		}
	}
	return events, 1, ""
}

// dockerEvent ist eine Zeile von `docker events --format '{{json .}}'`.
type dockerEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	TimeNano int64 `json:"timeNano"`
}

var dockerLifecycle = map[string]bool{"create": true, "start": true, "die": true, "kill": true, "oom": true, "destroy": true, "restart": true, "stop": true}

// mapDockerEvent macht aus einem Container-Ereignis ein AuthEvent: exec_create →
// docker_exec (Befehl im Detail), Lebenszyklus → docker_event. Name muss zum Target passen.
func mapDockerEvent(src shared.LogSource, raw string, now time.Time) (shared.AuthEvent, bool) {
	var e dockerEvent
	if json.Unmarshal([]byte(raw), &e) != nil || e.Type != "container" {
		return shared.AuthEvent{}, false
	}
	name := e.Actor.Attributes["name"]
	if t := strings.TrimSpace(src.Target); t != "" && t != "*" && !matchName(t, name) {
		return shared.AuthEvent{}, false
	}
	ts := now
	if e.TimeNano > 0 {
		ts = time.Unix(0, e.TimeNano)
	}
	ev := shared.AuthEvent{Time: ts, Service: src.Name, RecordID: "de:" + short(src.ID) + ":" + strconv.FormatInt(e.TimeNano, 10)}
	action, arg, _ := strings.Cut(e.Action, ": ")
	switch {
	case action == "exec_create":
		ev.Kind = "docker_exec"
		ev.Detail = name + ": " + arg
		return ev, true
	case dockerLifecycle[action]:
		ev.Kind = "docker_event"
		ev.Detail = action + " " + name
		if img := e.Actor.Attributes["image"]; img != "" {
			ev.Detail += " (" + img
			if ec := e.Actor.Attributes["exitCode"]; ec != "" {
				ev.Detail += ", exit=" + ec
			}
			ev.Detail += ")"
		}
		return ev, true
	}
	return shared.AuthEvent{}, false
}

// collectDockerEvents liest Container-Ereignisse seit dem gemerkten Zeitpunkt. --until
// beendet den Befehl (ohne läuft `docker events` endlos). Erster Lauf setzt nur den Cursor.
func collectDockerEvents(ctx context.Context, src shared.LogSource, cur cursors, now time.Time, budget int) ([]shared.AuthEvent, int, string) {
	key := "de:" + src.ID
	cs := cur[key]
	if cs == nil {
		cur[key] = &fileCursor{Since: strconv.FormatInt(now.UnixNano(), 10), Seen: now.Format(time.RFC3339)}
		return nil, 1, ""
	}
	cs.Seen = now.Format(time.RFC3339)
	sinceNano, _ := strconv.ParseInt(cs.Since, 10, 64)
	if sinceNano <= 0 {
		sinceNano = now.Add(-10 * time.Minute).UnixNano()
	}
	// docker events nimmt Sekunden-Zeitstempel mit Nachkommastellen.
	since := fmt.Sprintf("%d.%09d", sinceNano/1e9, sinceNano%1e9)
	until := fmt.Sprintf("%d.%09d", now.UnixNano()/1e9, now.UnixNano()%1e9)
	out, err := dockerOut(ctx, "events", "--since", since, "--until", until, "--format", "{{json .}}")
	if err != nil {
		return nil, 0, err.Error()
	}
	var events []shared.AuthEvent
	last := sinceNano
	for _, raw := range strings.Split(out, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var e dockerEvent
		if json.Unmarshal([]byte(raw), &e) == nil && e.TimeNano > last {
			last = e.TimeNano
		}
		if ev, ok := mapDockerEvent(src, raw, now); ok && len(events) < budget {
			events = append(events, ev)
		}
	}
	// Nächster Lauf ab dem letzten gesehenen Ereignis (+1 ns), spätestens ab jetzt.
	if last < now.UnixNano() {
		cs.Since = strconv.FormatInt(last+1, 10)
	} else {
		cs.Since = strconv.FormatInt(now.UnixNano(), 10)
	}
	return events, 1, ""
}
