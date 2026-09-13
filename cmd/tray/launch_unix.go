//go:build linux || freebsd

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// termCandidate beschreibt, wie ein Terminal-Emulator Titel und Kommando erwartet.
// %T = Titel, danach folgt das Kommando mit seinen Argumenten.
type termCandidate struct {
	bin  string
	args []string // Platzhalter %T für den Titel; das Kommando wird angehängt
}

var termCandidates = []termCandidate{
	{"kitty", []string{"--title", "%T"}},
	{"ghostty", []string{"--title=%T", "-e"}},
	{"foot", []string{"--title", "%T"}},
	{"alacritty", []string{"--title", "%T", "-e"}},
	{"wezterm", []string{"start", "--"}},
	{"gnome-terminal", []string{"--title=%T", "--"}},
	{"konsole", []string{"-p", "tabtitle=%T", "-e"}},
	{"xfce4-terminal", []string{"--title=%T", "-x"}},
	{"mate-terminal", []string{"--title=%T", "--"}},
	{"tilix", []string{"-t", "%T", "-e"}},
	{"x-terminal-emulator", []string{"-e"}},
	{"xterm", []string{"-title", "%T", "-e"}},
}

// spawnTerminal öffnet ein Terminal-Fenster, in dem cmd mit args läuft.
// Vorrang haben tray.json (terminal) bzw. $ROSTER_TERMINAL/$TERMINAL.
func spawnTerminal(cfg *config, title, cmd string, args []string) error {
	if forced := firstNonEmpty(cfg.Terminal, os.Getenv("ROSTER_TERMINAL"), os.Getenv("TERMINAL")); forced != "" {
		fields := strings.Fields(forced)
		bin, err := exec.LookPath(fields[0])
		if err != nil {
			return fmt.Errorf("terminal %q nicht gefunden: %w", fields[0], err)
		}
		// Mit eigenen Argumenten ($TERMINAL="foot --title X") übernehmen wir diese
		// unverändert; ohne welche raten wir das übliche -e.
		full := append([]string{}, fields[1:]...)
		if len(fields) == 1 {
			full = append(full, "-e")
		}
		full = append(full, cmd)
		return spawn(bin, append(full, args...)...)
	}
	for _, c := range termCandidates {
		bin, err := exec.LookPath(c.bin)
		if err != nil {
			continue
		}
		full := make([]string, 0, len(c.args)+len(args)+1)
		for _, a := range c.args {
			full = append(full, strings.ReplaceAll(a, "%T", title))
		}
		full = append(full, cmd)
		full = append(full, args...)
		return spawn(bin, full...)
	}
	return fmt.Errorf("kein Terminal-Emulator gefunden – in tray.json unter \"terminal\" eintragen")
}

// openURL überlässt das Öffnen dem Desktop (Browser bzw. roster://-Handler).
func openURL(u string) error { return spawn("xdg-open", u) }

// sftpClients sind bekannte Programme, die eine sftp://-Adresse direkt öffnen –
// Notnagel, falls der Desktop kein Programm für das Schema registriert hat.
var sftpClients = []string{"filezilla", "dolphin", "nautilus", "nemo", "thunar", "krusader", "konqueror", "gftp"}

// spawnSFTP öffnet die sftp://-Adresse: bevorzugt beim registrierten Programm des
// Desktops (GVFS/KIO mounten die Verbindung dann im Dateimanager), sonst beim
// ersten gefundenen bekannten Client.
func spawnSFTP(target string) (string, error) {
	if prog := sftpHandler(); prog != "" {
		return prog, openURL(target)
	}
	for _, bin := range sftpClients {
		if p, err := exec.LookPath(bin); err == nil {
			return bin, spawn(p, target)
		}
	}
	return "", fmt.Errorf("kein Programm für sftp://-Adressen gefunden – in tray.json \"sftp_command\" setzen (z. B. \"filezilla {{url}}\")")
}

// sftpHandler liefert den Desktop-Eintrag, der für sftp:// registriert ist ("" = keiner).
func sftpHandler() string {
	out, err := exec.Command("xdg-mime", "query", "default", "x-scheme-handler/sftp").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), ".desktop")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
