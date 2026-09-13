//go:build !windows

package main

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
)

// Einzelinstanz: Aus dem Anwendungsmenü wird schnell ein zweites Mal gestartet.
// Statt eines zweiten Tray-Symbols soll dann das vorhandene Fenster nach vorn
// kommen. Dafür lauscht die laufende Instanz auf einem Unix-Socket im
// Laufzeitverzeichnis; ein zweiter Start schickt „show" und beendet sich.

func socketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "roster-tray.sock")
}

// notifyRunning meldet einer laufenden Instanz, dass sie ihr Fenster zeigen soll.
// Rückgabe true = es läuft bereits eine, dieser Start kann sich beenden.
func notifyRunning() bool {
	c, err := net.Dial("unix", socketPath())
	if err != nil {
		return false // niemand da (oder verwaiste Socket-Datei)
	}
	defer c.Close()
	_, _ = c.Write([]byte("show\n"))
	return true
}

// listenSingleton nimmt die Rolle der laufenden Instanz ein und liefert die
// Aufräumfunktion. Klappt das nicht, läuft die App ohne Einzelinstanz-Erkennung.
func (a *app) listenSingleton() func() {
	path := socketPath()
	ln, err := net.Listen("unix", path)
	if err != nil {
		// Verwaiste Socket-Datei eines abgestürzten Vorgängers: einmal aufräumen.
		// (Eine echte laufende Instanz hätte notifyRunning vorher erkannt.)
		if rerr := os.Remove(path); rerr != nil {
			return func() {}
		}
		if ln, err = net.Listen("unix", path); err != nil {
			return func() {}
		}
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadString('\n')
			c.Close()
			if line == "show\n" {
				a.wantShow.Store(true)
			}
		}
	}()
	return func() { ln.Close(); os.Remove(path) }
}
