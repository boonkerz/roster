package clientinstall

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Shortcut beschreibt eine .lnk-Verknüpfung.
type Shortcut struct {
	Path        string // Ziel-.lnk
	Target      string // Programm
	Args        string
	Icon        string // .ico oder .exe (leer = Symbol des Programms)
	Description string
}

// CreateShortcut legt eine Verknüpfung über die WScript.Shell-COM-Schnittstelle an
// (PowerShell ist auf jedem Windows vorhanden; kein cgo, keine COM-Bindings nötig).
func CreateShortcut(s Shortcut) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	q := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
	script := fmt.Sprintf(
		"$s=(New-Object -ComObject WScript.Shell).CreateShortcut(%s);$s.TargetPath=%s;$s.Arguments=%s;$s.WorkingDirectory=%s;$s.Description=%s;",
		q(s.Path), q(s.Target), q(s.Args), q(filepath.Dir(s.Target)), q(s.Description))
	if s.Icon != "" {
		script += fmt.Sprintf("$s.IconLocation=%s;", q(s.Icon+",0"))
	}
	script += "$s.Save()"
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("verknüpfung %s: %v: %s", s.Path, err, strings.TrimSpace(string(out)))
	}
	return nil
}
