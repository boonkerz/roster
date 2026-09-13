package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// config liegt in ~/.config/roster/tray.json (bzw. dem OS-Äquivalent) und hält die
// Serveradresse samt langlebigem API-Token. Datei mit 0600, Verzeichnis mit 0700 –
// das Token ist ein vollwertiger Zugang zum Server.
type config struct {
	URL        string  `json:"url"`
	User       string  `json:"user,omitempty"`
	Token      string  `json:"token,omitempty"`
	TokenID    string  `json:"token_id,omitempty"`
	Insecure   bool    `json:"insecure,omitempty"`    // TLS-Zertifikat nicht prüfen (Selfsigned-Testserver)
	RefreshSec int     `json:"refresh_sec,omitempty"` // Poll-Intervall der Geräteliste (Default 30)
	Terminal   string  `json:"terminal,omitempty"`    // Terminal-Emulator erzwingen (sonst Auto-Erkennung)
	ViewerPath string  `json:"viewer_path,omitempty"` // Pfad zu roster-viewer (sonst Auto-Erkennung)
	Shell      string  `json:"shell,omitempty"`       // Default-Shell fürs Remote-Terminal
	UIScale    float64 `json:"ui_scale,omitempty"`    // feste Oberflächen-Skalierung (0 = automatisch)

	// SFTP-Knopf: Benutzername für die Verbindung und optional ein eigenes Programm
	// statt des Desktop-Handlers. Platzhalter: {{host}}, {{user}}, {{url}}, {{name}}.
	SFTPUser    string `json:"sftp_user,omitempty"`
	SFTPCommand string `json:"sftp_command,omitempty"`

	// Aufgeklappte Proxmox-Hosts (Geräte-IDs) – bleibt über Neustarts erhalten.
	ExpandedHosts []string `json:"expanded_hosts,omitempty"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "roster", "tray.json"), nil
}

func loadConfig() (*config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &config{}, nil
		}
		return nil, err
	}
	var c config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *config) save() error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

func (c *config) refreshInterval() int {
	if c.RefreshSec <= 0 {
		return 30
	}
	if c.RefreshSec < 5 {
		return 5
	}
	return c.RefreshSec
}
