// Package compliance wertet den Regelkatalog (internal/shared/compliance) auf dem Gerät
// aus. Jede Regel ist eine kleine Funktion über einer Env-Schnittstelle (Dateien, Befehle,
// sysctl), damit die Auswertung mit einer Fake-Umgebung unit-testbar bleibt.
package compliance

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/agent/collect"
)

// Env kapselt den Systemzugriff der Regeln.
type Env interface {
	OS() string
	ReadFile(path string) (string, error)
	Exists(path string) bool
	Glob(pattern string) []string
	Stat(path string) (mode fs.FileMode, uid, gid int, ok bool)
	Run(name string, args ...string) (out string, exit int, err error)
	HasBin(name string) bool
	AutoUpdates() string
	RebootPending() (pending, known bool)
}

// Result ist das Ergebnis einer Regel.
type Result struct {
	Status   string // pass | fail | na | error
	Evidence string
}

func pass(ev string) Result   { return Result{Status: "pass", Evidence: ev} }
func fail(ev string) Result   { return Result{Status: "fail", Evidence: ev} }
func na(ev string) Result     { return Result{Status: "na", Evidence: ev} }
func errRes(ev string) Result { return Result{Status: "error", Evidence: ev} }

// systemEnv ist die echte Umgebung (mit Timeout je Befehl).
type systemEnv struct {
	ctx context.Context
}

// NewEnv liefert die Systemumgebung.
func NewEnv(ctx context.Context) Env { return &systemEnv{ctx: ctx} }

func (e *systemEnv) OS() string { return runtime.GOOS }

func (e *systemEnv) ReadFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func (e *systemEnv) Exists(path string) bool { _, err := os.Stat(path); return err == nil }

func (e *systemEnv) Glob(pattern string) []string { m, _ := filepath.Glob(pattern); return m }

func (e *systemEnv) Stat(path string) (fs.FileMode, int, int, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, 0, false
	}
	uid, gid := ownerIDs(fi)
	return fi.Mode(), uid, gid, true
}

func (e *systemEnv) Run(name string, args ...string) (string, int, error) {
	ctx, cancel := context.WithTimeout(e.ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	exit := 0
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
		err = nil
	}
	return strings.TrimSpace(string(out)), exit, err
}

func (e *systemEnv) HasBin(name string) bool { _, err := exec.LookPath(name); return err == nil }

func (e *systemEnv) AutoUpdates() string { return collect.AutoUpdates(e.ctx) }

func (e *systemEnv) RebootPending() (bool, bool) { return collect.RebootPending() }
