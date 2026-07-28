package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/shared"
)

// dockerCheck wertet einen Docker-Zustand aus. Modi (Config "mode"):
//
//	running   – Anzahl laufender Container >= min (Standard 1)
//	stopped   – Anzahl NICHT laufender Container <= max (Standard 0)
//	container – ein Container (Name enthält "name") läuft
//	unhealthy – kein Container mit Health=unhealthy
func dockerCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	if !collect.DockerAvailable() {
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Output: "Docker nicht verfügbar"}
	}
	return evalDockerMode(c, collect.DockerContainers(ctx))
}

// evalDockerMode wertet die Modi gegen eine Containerliste aus (rein, testbar).
func evalDockerMode(c shared.CheckSpec, containers []shared.DockerContainer) shared.CheckResult {
	mode := strConfig(c, "mode")
	if mode == "" {
		mode = "running"
	}
	switch mode {
	case "running":
		min := 1.0
		if v, ok := numConfig(c, "min"); ok {
			min = v
		}
		n := countState(containers, "running")
		st := "passing"
		if float64(n) < min {
			st = "failing"
		}
		return dockerRes(c, st, float64(n), fmt.Sprintf("%d Container laufen (min %.0f)", n, min))

	case "stopped":
		max := 0.0
		if v, ok := numConfig(c, "max"); ok {
			max = v
		}
		n := len(containers) - countState(containers, "running")
		st := "passing"
		if float64(n) > max {
			st = "failing"
		}
		out := fmt.Sprintf("%d Container gestoppt (max %.0f)", n, max)
		if n > 0 {
			out += ": " + strings.Join(namesByNotState(containers, "running"), ", ")
		}
		return dockerRes(c, st, float64(n), out)

	case "container":
		name := strConfig(c, "name")
		if name == "" {
			return unknownCheck(c, "Container-Name (config.name) erforderlich")
		}
		running, found := false, false
		for _, ct := range containers {
			if strings.Contains(ct.Name, name) {
				found = true
				if ct.State == "running" {
					running = true
				}
			}
		}
		switch {
		case running:
			return dockerRes(c, "passing", 1, fmt.Sprintf("Container %q läuft", name))
		case found:
			return dockerRes(c, "failing", 0, fmt.Sprintf("Container %q ist nicht gestartet", name))
		default:
			return dockerRes(c, "failing", 0, fmt.Sprintf("Container %q nicht gefunden", name))
		}

	case "unhealthy":
		var bad []string
		for _, ct := range containers {
			if ct.Health == "unhealthy" {
				bad = append(bad, ct.Name)
			}
		}
		if len(bad) > 0 {
			return dockerRes(c, "failing", float64(len(bad)), fmt.Sprintf("%d unhealthy: %s", len(bad), strings.Join(bad, ", ")))
		}
		return dockerRes(c, "passing", 0, "keine unhealthy Container")

	default:
		return unknownCheck(c, "unbekannter Docker-Modus: "+mode)
	}
}

func countState(cs []shared.DockerContainer, state string) int {
	n := 0
	for _, c := range cs {
		if c.State == state {
			n++
		}
	}
	return n
}

func namesByNotState(cs []shared.DockerContainer, state string) []string {
	var out []string
	for _, c := range cs {
		if c.State != state {
			out = append(out, c.Name)
		}
	}
	return out
}

func dockerRes(c shared.CheckSpec, status string, val float64, out string) shared.CheckResult {
	return shared.CheckResult{CheckID: c.ID, Status: status, Value: val, Output: out}
}
