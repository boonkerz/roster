package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// openTerminal startet das Remote-Terminal in einem echten Terminal-Fenster des
// Systems: es ruft dieses Binary erneut mit --term auf (siehe term.go).
func openTerminal(cfg *config, d device) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("eigenen pfad ermitteln: %w", err)
	}
	shell := cfg.Shell
	if shell == "" {
		shell = defaultShell(d.OS)
	}
	args := []string{"--term", d.ID, "--shell", shell, "--hold"}
	title := "Roster – " + d.Hostname
	return spawnTerminal(cfg, title, self, args)
}

// defaultShell wählt die Standard-Shell wie das Web-UI: cmd auf Windows, sonst die
// Login-Shell des Geräts.
func defaultShell(osName string) string {
	if strings.Contains(strings.ToLower(osName), "win") {
		return "cmd"
	}
	return "shell"
}

// openViewer fordert eine Fernsteuerungs-Sitzung an und startet den nativen Viewer
// mit dem base64-Startcode – identisch zum Browser-Button „Im Viewer öffnen".
func openViewer(ctx context.Context, c *client, cfg *config, d device) error {
	s, err := c.startRemote(ctx, d.ID)
	if err != nil {
		return err
	}
	code, err := viewerLaunchCode(cfg, d, s)
	if err != nil {
		return err
	}

	if bin := findViewer(cfg); bin != "" {
		return spawn(bin, code)
	}
	// Kein Viewer-Binary gefunden: über den roster://-Handler versuchen.
	if err := openURL("roster://" + code); err != nil {
		return fmt.Errorf("roster-viewer nicht gefunden – Pfad in tray.json (viewer_path) setzen oder Viewer installieren")
	}
	return nil
}

// viewerLaunchCode baut den base64url-Startcode, den roster-viewer erwartet –
// dasselbe Format wie der Browser-Button „Im Viewer öffnen".
func viewerLaunchCode(cfg *config, d device, s *remoteStart) (string, error) {
	blob := map[string]any{
		"url":     cfg.URL,
		"device":  d.ID,
		"session": s.Session,
		"token":   s.Token,
		"title":   "Fernsteuerung " + d.Hostname,
	}
	if cfg.Insecure {
		blob["insecure"] = true
	}
	raw, err := json.Marshal(blob)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// openWeb öffnet die Geräteseite im Standardbrowser.
func openWeb(cfg *config, d device) error {
	return openURL(strings.TrimRight(cfg.URL, "/") + "/devices/" + d.ID)
}

// findViewer sucht roster-viewer: konfigurierter Pfad → neben diesem Binary → PATH.
func findViewer(cfg *config) string {
	name := "roster-viewer"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if cfg.ViewerPath != "" {
		if fi, err := os.Stat(cfg.ViewerPath); err == nil && !fi.IsDir() {
			return cfg.ViewerPath
		}
	}
	if self, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(self), name)
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// spawn startet ein Programm losgelöst; der Zombie wird im Hintergrund abgeräumt.
func spawn(bin string, args ...string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
