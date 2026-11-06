package policy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/shared"
)

// dockerCheck wertet einen Docker-Zustand aus. Modi (Config "mode"):
//
//	running   – Anzahl laufender Container >= min (Standard 1)
//	stopped   – Anzahl NICHT laufender Container <= max (Standard 0)
//	container – ein Container (Name enthält "name") läuft
//	unhealthy – kein Container mit Health=unhealthy
//	databases – Datenbanken in DB-Containern entsprechen der Erwartungsliste ("expect",
//	            optional "container"-Teilstring); ohne Liste nur Abfragbarkeit
//	image_age – kein laufender Container nutzt ein Image älter als "max_days" (Standard 365)
func dockerCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	if !collect.DockerAvailable() {
		return shared.CheckResult{CheckID: c.ID, Status: "unknown", Output: "Docker nicht verfügbar"}
	}
	containers := collect.DockerContainers(ctx)
	switch strConfig(c, "mode") {
	case "databases":
		return evalDatabases(c, collect.DockerDatabases(ctx, containers))
	case "image_age":
		return evalImageAge(c, containers, collect.DockerImages(ctx), time.Now())
	}
	return evalDockerMode(c, containers)
}

// evalDatabases vergleicht den Datenbank-Bestand mit config.expect (kommagetrennt).
func evalDatabases(c shared.CheckSpec, dbs []shared.DockerDatabase) shared.CheckResult {
	filter := strings.ToLower(strConfig(c, "container"))
	expect := map[string]bool{}
	for _, n := range strings.Split(strConfig(c, "expect"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			expect[n] = true
		}
	}
	var errs, unexpected []string
	found := map[string]bool{}
	containers := map[string]bool{}
	for _, d := range dbs {
		if filter != "" && !strings.Contains(strings.ToLower(d.Container), filter) {
			continue
		}
		containers[d.Container] = true
		if d.Error != "" {
			errs = append(errs, d.Container+": "+d.Error)
			continue
		}
		found[d.Name] = true
		if len(expect) > 0 && !expect[d.Name] {
			unexpected = append(unexpected, d.Container+"/"+d.Name)
		}
	}
	if len(containers) == 0 {
		return dockerRes(c, "unknown", 0, "kein laufender Datenbank-Container gefunden")
	}
	var missing []string
	for n := range expect {
		if !found[n] {
			missing = append(missing, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	switch {
	case len(unexpected) > 0 || len(missing) > 0:
		var parts []string
		if len(unexpected) > 0 {
			parts = append(parts, "unerwartet: "+strings.Join(unexpected, ", "))
		}
		if len(missing) > 0 {
			parts = append(parts, "fehlt: "+strings.Join(missing, ", "))
		}
		return dockerRes(c, "failing", float64(len(unexpected)+len(missing)), strings.Join(parts, "; "))
	case len(errs) > 0 && len(found) == 0:
		return dockerRes(c, "unknown", 0, "nicht abfragbar: "+strings.Join(errs, "; "))
	}
	out := fmt.Sprintf("%d Datenbank(en) in %d Container(n)", len(found), len(containers))
	if len(errs) > 0 {
		out += "; nicht abfragbar: " + strings.Join(errs, "; ")
	}
	return dockerRes(c, "passing", float64(len(found)), out)
}

// imageCreated parst das docker-Rohformat von CreatedAt.
func imageCreated(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05 -0700 MST", "2006-01-02T15:04:05.999999999Z07:00"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// imageFor ordnet die Image-Referenz eines Containers einem lokalen Image zu.
func imageFor(ref string, images []shared.DockerImage) (shared.DockerImage, bool) {
	for _, im := range images {
		full := im.Repository + ":" + im.Tag
		if ref == full || (im.Tag == "latest" && ref == im.Repository) || (im.ImageID != "" && strings.HasPrefix(strings.TrimPrefix(ref, "sha256:"), im.ImageID)) {
			return im, true
		}
	}
	return shared.DockerImage{}, false
}

// evalImageAge meldet laufende Container, deren Image älter als max_days ist.
func evalImageAge(c shared.CheckSpec, containers []shared.DockerContainer, images []shared.DockerImage, now time.Time) shared.CheckResult {
	maxDays := 365.0
	if v, ok := numConfig(c, "max_days"); ok && v > 0 {
		maxDays = v
	}
	var old []string
	oldest := 0.0
	for _, ct := range containers {
		if ct.State != "running" {
			continue
		}
		im, ok := imageFor(ct.Image, images)
		if !ok {
			continue
		}
		created, ok := imageCreated(im.CreatedAt)
		if !ok {
			continue
		}
		days := now.Sub(created).Hours() / 24
		if days > oldest {
			oldest = days
		}
		if days > maxDays {
			old = append(old, fmt.Sprintf("%s (%s, %.0f Tage)", ct.Name, ct.Image, days))
		}
	}
	if len(old) > 0 {
		return dockerRes(c, "failing", oldest, fmt.Sprintf("%d Container mit Image älter als %.0f Tage: %s", len(old), maxDays, strings.Join(old, ", ")))
	}
	return dockerRes(c, "passing", oldest, fmt.Sprintf("ältestes Image %.0f Tage (max %.0f)", oldest, maxDays))
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
