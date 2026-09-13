package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// spawnTerminal öffnet unter macOS Terminal.app mit einem kleinen Wegwerf-Skript –
// „open" nimmt kein Kommando mit Argumenten entgegen.
func spawnTerminal(cfg *config, title, cmd string, args []string) error {
	quoted := make([]string, 0, len(args)+1)
	for _, a := range append([]string{cmd}, args...) {
		quoted = append(quoted, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
	}
	script := filepath.Join(os.TempDir(),
		"roster-term-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".command")
	body := "#!/bin/sh\n" +
		"printf '\\033]0;" + strings.ReplaceAll(title, "'", "") + "\\007'\n" +
		"exec " + strings.Join(quoted, " ") + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		return fmt.Errorf("startskript: %w", err)
	}
	app := cfg.Terminal
	if app == "" {
		app = "Terminal"
	}
	return spawn("/usr/bin/open", "-a", app, script)
}

// openURL überlässt das Öffnen dem Desktop (Browser bzw. roster://-Handler).
func openURL(u string) error { return spawn("/usr/bin/open", u) }

// spawnSFTP überlässt die sftp://-Adresse dem registrierten Programm (z. B.
// Cyberduck/ForkLift); der Finder selbst kann kein SFTP.
func spawnSFTP(target string) (string, error) { return "Standardprogramm", openURL(target) }
