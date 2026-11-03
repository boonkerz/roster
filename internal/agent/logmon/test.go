package logmon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// TestResult ist das Ergebnis eines Probelaufs („Testen“ im Richtlinien-Editor): die
// letzten Zeilen jeder Zieldatei bzw. jedes Containers werden mit dem Format geparst.
type TestResult struct {
	Targets []TargetResult     `json:"targets"`
	Lines   int                `json:"lines"`
	Matched int                `json:"matched"`
	Events  []shared.AuthEvent `json:"events"` // Stichprobe (neueste zuletzt, max. 20)
	Error   string             `json:"error,omitempty"`
	Hints   []string           `json:"hints,omitempty"` // z. B. Namen laufender Container
}

// TargetResult ist das Ergebnis je Datei/Container.
type TargetResult struct {
	Target  string `json:"target"`
	Lines   int    `json:"lines"`
	Matched int    `json:"matched"`
	Error   string `json:"error,omitempty"`
}

const (
	testMaxSample = 20
	testTailBytes = 2 * 1024 * 1024
)

// Test liest die letzten n Zeilen der Quelle und zählt Treffer; Cursor bleiben unberührt.
func Test(ctx context.Context, src shared.LogSource, n int) TestResult {
	if n <= 0 || n > 5000 {
		n = 300
	}
	now := time.Now()
	res := TestResult{Targets: []TargetResult{}, Events: []shared.AuthEvent{}}
	add := func(target string, lines []string, err error) {
		tr := TargetResult{Target: target, Lines: len(lines)}
		if err != nil {
			tr.Error = err.Error()
		}
		for _, line := range lines {
			if ev, ok := Parse(src, Unwrap(line+"\n"), now); ok {
				tr.Matched++
				res.Events = append(res.Events, ev)
			}
		}
		res.Lines += tr.Lines
		res.Matched += tr.Matched
		res.Targets = append(res.Targets, tr)
	}
	switch src.Type {
	case "journal":
		sel, ok := journalArgs(src.Target)
		if !ok {
			res.Error = "ungültiges Journal-Ziel " + src.Target
			break
		}
		args := append([]string{"-o", "json", "--no-pager", "-q", "-n", fmt.Sprint(n)}, sel...)
		out, err := exec.CommandContext(ctx, "journalctl", args...).Output()
		var lines []string
		for _, raw := range strings.Split(string(out), "\n") {
			var jl journalLine
			if json.Unmarshal([]byte(raw), &jl) == nil {
				if t := jl.text(); t != "" {
					lines = append(lines, t)
				}
			}
		}
		if err != nil && len(lines) == 0 {
			err = shortErr(err)
		} else {
			err = nil
		}
		add("journal:"+strings.Join(sel, " "), lines, err)
	case "docker_events":
		out, err := dockerOut(ctx, "events", "--since", "24h", "--until", "0s", "--format", "{{json .}}")
		tr := TargetResult{Target: "docker events (24 h)"}
		if err != nil {
			tr.Error = err.Error()
		}
		for _, raw := range strings.Split(out, "\n") {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			tr.Lines++
			if ev, ok := mapDockerEvent(src, raw, now); ok {
				tr.Matched++
				if len(res.Events) < 20 {
					res.Events = append(res.Events, ev)
				}
			}
		}
		res.Lines, res.Matched = tr.Lines, tr.Matched
		res.Targets = append(res.Targets, tr)
	case "file":
		paths := globPaths(src.Target)
		if len(paths) == 0 {
			res.Error = "keine Datei passt auf " + src.Target
			break
		}
		for _, p := range paths {
			lines, err := tailFile(p, n)
			add(p, lines, err)
		}
	case "docker":
		list, names, err := containers(ctx, src.Target)
		if err != nil {
			res.Error = err.Error()
			break
		}
		if len(list) == 0 {
			res.Error = "kein laufender Container passt auf " + src.Target
			if len(names) > 30 {
				names = names[:30]
			}
			res.Hints = names
			break
		}
		for _, c := range list {
			if c.LogPath != "" {
				if lines, err := tailFile(c.LogPath, n); err == nil {
					add(c.Name, lines, nil)
					continue
				}
			}
			out, err := dockerOut(ctx, "logs", "--timestamps", "--tail", fmt.Sprint(n), c.ID)
			var lines []string
			for _, l := range strings.Split(out, "\n") {
				if l != "" {
					lines = append(lines, l)
				}
			}
			add(c.Name, lines, err)
		}
	default:
		res.Error = "unbekannter Typ " + src.Type
	}
	if len(res.Events) > testMaxSample {
		res.Events = res.Events[len(res.Events)-testMaxSample:]
	}
	return res
}

// tailFile liefert die letzten n Zeilen einer Datei (liest höchstens testTailBytes).
func tailFile(p string, n int) ([]string, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, shortErr(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, shortErr(err)
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("ist ein Ordner")
	}
	start := fi.Size() - testTailBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if start > 0 {
		// angeschnittene erste Zeile verwerfen
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}
