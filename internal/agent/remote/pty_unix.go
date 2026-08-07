//go:build linux || darwin || freebsd

package remote

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/creack/pty"
)

// unixPTY führt eine interaktive Shell unter einem Pseudo-Terminal aus.
type unixPTY struct {
	cmd *exec.Cmd
	f   *os.File
}

// startPTY startet die Shell. runas=="user" startet sie als der aktuell
// angemeldete Login-Benutzer (via `su - <user>`); sonst im Dienst-Kontext (root).
func startPTY(_shell, runas string) (ptySession, error) {
	cmd := buildShellCmd(runas)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &unixPTY{cmd: cmd, f: f}, nil
}

func buildShellCmd(runas string) *exec.Cmd {
	if runas == "user" {
		if u := activeUser(); u != "" {
			// Login-Shell des Benutzers interaktiv starten.
			return exec.Command("su", "-", u)
		}
	}
	return exec.Command(defaultShell(), "-i")
}

func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	candidates := []string{"/bin/bash", "/bin/sh"}
	if runtime.GOOS == "darwin" {
		candidates = []string{"/bin/zsh", "/bin/bash", "/bin/sh"}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "/bin/sh"
}

// activeUser ermittelt den aktuell angemeldeten interaktiven Benutzer (erste
// Spalte der ersten `who`-Zeile). Leer, wenn niemand angemeldet ist.
func activeUser() string {
	out, err := exec.Command("who").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 && f[0] != "root" {
			return f[0]
		}
	}
	// Fallback: auch root akzeptieren, falls nur root angemeldet ist.
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			return f[0]
		}
	}
	return ""
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *unixPTY) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *unixPTY) Wait() int {
	err := p.cmd.Wait()
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func (p *unixPTY) Close() error {
	_ = p.f.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}
