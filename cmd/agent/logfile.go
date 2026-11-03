package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kardianos/service"
)

// logWriter liefert das Ziel des Agent-Protokolls: stderr – und als Windows-Dienst
// zusätzlich die Datei agent.log neben der Konfiguration (C:\ProgramData\Roster), weil
// der Service Control Manager stderr verwirft und ein Hänger sonst nicht nachvollziehbar
// ist. Ab 5 MB wird die Datei zu agent.log.1 gedreht.
func logWriter(configPath string) io.Writer {
	if runtime.GOOS != "windows" || service.Interactive() {
		return os.Stderr
	}
	path := filepath.Join(filepath.Dir(configPath), "agent.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, f)
}
