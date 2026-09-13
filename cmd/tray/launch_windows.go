package main

import (
	"os/exec"
)

// spawnTerminal bevorzugt Windows Terminal (wt.exe) und fällt sonst auf die
// klassische Konsole zurück.
func spawnTerminal(cfg *config, title, cmd string, args []string) error {
	if cfg.Terminal != "" {
		return spawn(cfg.Terminal, append([]string{cmd}, args...)...)
	}
	if wt, err := exec.LookPath("wt.exe"); err == nil {
		full := append([]string{"--title", title, cmd}, args...)
		return spawn(wt, full...)
	}
	// cmd.exe /c start "<Titel>" "<Programm>" <Argumente>
	full := append([]string{"/c", "start", title, cmd}, args...)
	return spawn("cmd.exe", full...)
}

// openURL überlässt das Öffnen dem Desktop (Browser bzw. roster://-Handler).
func openURL(u string) error { return spawn("rundll32.exe", "url.dll,FileProtocolHandler", u) }

// spawnSFTP nutzt WinSCP, wenn es installiert ist, sonst das für sftp://
// registrierte Programm.
func spawnSFTP(target string) (string, error) {
	for _, cand := range []string{
		"WinSCP.exe",
		`C:\Program Files (x86)\WinSCP\WinSCP.exe`,
		`C:\Program Files\WinSCP\WinSCP.exe`,
	} {
		if p, err := exec.LookPath(cand); err == nil {
			return "WinSCP", spawn(p, target)
		}
	}
	return "Standardprogramm", openURL(target)
}
