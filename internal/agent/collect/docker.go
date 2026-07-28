package collect

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Docker-Inventar über die docker-CLI (vorhanden, wo Docker läuft). Plattformneutral
// (exec.LookPath deckt Linux/macOS/Windows ab). Fehlt Docker oder schlägt ein Aufruf
// fehl, wird nil geliefert (das Feld verschwindet aus dem Inventar).

// DockerAvailable meldet, ob die docker-CLI im PATH ist.
func DockerAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

// dockerRun führt ein docker-Kommando mit kurzem Timeout aus (leer bei Fehler), damit
// ein hängender Daemon das Inventar nicht blockiert.
func dockerRun(ctx context.Context, args ...string) string {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "docker", args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// dockerPSLine spiegelt die Felder von `docker ps --format '{{json .}}'`.
type dockerPSLine struct {
	ID           string `json:"ID"`
	Names        string `json:"Names"`
	Image        string `json:"Image"`
	State        string `json:"State"`
	Status       string `json:"Status"`
	Ports        string `json:"Ports"`
	Labels       string `json:"Labels"`
	CreatedAt    string `json:"CreatedAt"`
	HealthStatus string `json:"HealthStatus"`
}

// dockerImageLine spiegelt die Felder von `docker images --format '{{json .}}'`.
type dockerImageLine struct {
	Repository   string `json:"Repository"`
	Tag          string `json:"Tag"`
	ID           string `json:"ID"`
	Size         string `json:"Size"`
	CreatedSince string `json:"CreatedSince"`
}

// shortID kürzt eine (evtl. sha256:-präfixierte) ID auf 12 Zeichen.
func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// composeProject liest com.docker.compose.project aus dem Labels-String (k=v,k=v).
func composeProject(labels string) string {
	for _, kv := range strings.Split(labels, ",") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(kv), "com.docker.compose.project="); ok {
			return p
		}
	}
	return ""
}

// parseDockerContainers wandelt die JSON-Zeilen von `docker ps` in Container um.
func parseDockerContainers(out string) []shared.DockerContainer {
	var list []shared.DockerContainer
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p dockerPSLine
		if json.Unmarshal([]byte(line), &p) != nil {
			continue
		}
		state := p.State
		if state == "" { // ältere Docker liefern kein State-Feld -> aus Status ableiten
			if strings.HasPrefix(p.Status, "Up") {
				state = "running"
			} else {
				state = "exited"
			}
		}
		list = append(list, shared.DockerContainer{
			ContainerID: shortID(p.ID),
			Name:        p.Names,
			Image:       p.Image,
			State:       state,
			Status:      p.Status,
			Ports:       p.Ports,
			Compose:     composeProject(p.Labels),
			Created:     p.CreatedAt,
			Health:      p.HealthStatus,
		})
	}
	return list
}

// parseDockerImages wandelt die JSON-Zeilen von `docker images` in Images um.
func parseDockerImages(out string) []shared.DockerImage {
	var list []shared.DockerImage
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p dockerImageLine
		if json.Unmarshal([]byte(line), &p) != nil {
			continue
		}
		list = append(list, shared.DockerImage{
			Repository: p.Repository,
			Tag:        p.Tag,
			ImageID:    shortID(p.ID),
			Size:       p.Size,
			Created:    p.CreatedSince,
		})
	}
	return list
}

// DockerContainers listet alle Container (laufend + gestoppt), sofern Docker vorhanden.
func DockerContainers(ctx context.Context) []shared.DockerContainer {
	if !DockerAvailable() {
		return nil
	}
	return parseDockerContainers(dockerRun(ctx, "ps", "--all", "--no-trunc", "--format", "{{json .}}"))
}

// DockerImages listet lokale Images, sofern Docker vorhanden.
func DockerImages(ctx context.Context) []shared.DockerImage {
	if !DockerAvailable() {
		return nil
	}
	return parseDockerImages(dockerRun(ctx, "images", "--format", "{{json .}}"))
}
