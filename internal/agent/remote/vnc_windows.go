//go:build windows

package remote

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"unsafe"

	"log/slog"

	"golang.org/x/sys/windows"
)

// startVNCServer startet on-demand einen VNC-Server unter Windows, gebunden an
// 127.0.0.1:5900. Damit der Server den Bildschirm des angemeldeten Nutzers sieht,
// wird er – wenn der Agent als SYSTEM-Dienst läuft – per CreateProcessAsUser in der
// Konsolen-Session gestartet (vgl. pty_windows_user.go). Läuft der Agent bereits in
// der Nutzer-Session, genügt ein normaler Start (Fallback).
//
// Der zu startende Server ist entweder PCINV_VNC_CMD (frei wählbar, z.B. ein
// installiertes UltraVNC/TightVNC) oder das gebündelte winvnc.exe. Die RFB-Auth
// ist optional: Zugriff ist nur über den authentifizierten Tunnel + Loopback
// möglich; die Passwort-Verschlüsselung am Server (UltraVNC-ini) folgt als Härtung.
func startVNCServer(_ context.Context, password string, consent bool, log *slog.Logger) (string, func(), error) {
	cmdLine, cleanup, err := vncCommandLine(password, consent)
	if err != nil {
		return "", nil, err
	}
	log.Info("vnc-server-kommandozeile", "cmd", cmdLine)

	stopProc, err := startInUserSession(cmdLine, log)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, err
	}
	log.Info("vnc-server (windows) gestartet", "consent", consent)

	stop := func() {
		stopProc()
		if cleanup != nil {
			cleanup()
		}
	}
	return "127.0.0.1:5900", stop, nil
}

// vncCommandLine liefert die zu startende Kommandozeile und einen Aufräum-Callback
// für temporäre Dateien.
func vncCommandLine(_ string, consent bool) (string, func(), error) {
	if custom := os.Getenv("PCINV_VNC_CMD"); custom != "" {
		return custom, nil, nil
	}
	bin, err := vncServerPath("winvnc")
	if err != nil {
		return "", nil, fmt.Errorf("winvnc.exe nicht gefunden (siehe VNC-Bundling) oder PCINV_VNC_CMD setzen: %w", err)
	}
	// Minimale ultravnc.ini: nur Loopback, keine Passwort-Abfrage (Tunnel sichert),
	// optional Nachfrage beim Nutzer.
	ini, err := os.CreateTemp("", "pcinv-ultravnc-*.ini")
	if err != nil {
		return "", nil, err
	}
	query := 0
	if consent {
		query = 1
	}
	fmt.Fprintf(ini, "[ultravnc]\nPortNumber=5900\nAllowLoopback=1\nLoopbackOnly=1\nAuthRequired=0\nQuerySetting=%d\nQueryTimeout=30\n", query)
	_ = ini.Close()
	cleanup := func() { _ = os.Remove(ini.Name()) }
	cmd := fmt.Sprintf(`"%s" -run -config "%s"`, bin, ini.Name())
	return cmd, cleanup, nil
}

// startInUserSession startet die Kommandozeile im Kontext des an der Konsole
// angemeldeten Nutzers (CreateProcessAsUser). Fällt darauf zurück, den Prozess im
// aktuellen Kontext zu starten, falls kein Session-Token verfügbar ist (z.B. Agent
// läuft bereits als Nutzer, nicht als SYSTEM-Dienst).
func startInUserSession(cmdLine string, log *slog.Logger) (func(), error) {
	sess := windows.WTSGetActiveConsoleSessionId()
	var token windows.Token
	if sess != 0xFFFFFFFF {
		if err := windows.WTSQueryUserToken(sess, &token); err != nil {
			log.Debug("kein session-token, fallback auf aktuellen kontext", "err", err)
			token = 0
		}
	}
	if token == 0 {
		return startInCurrentSession(cmdLine)
	}
	defer token.Close()

	var envBlock *uint16
	_ = windows.CreateEnvironmentBlock(&envBlock, token, false)

	cmd16, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		if envBlock != nil {
			_ = windows.DestroyEnvironmentBlock(envBlock)
		}
		return nil, err
	}
	desktop, _ := windows.UTF16PtrFromString(`winsta0\default`)
	si := windows.StartupInfo{Desktop: desktop}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NO_WINDOW)
	if err := windows.CreateProcessAsUser(token, nil, cmd16, nil, nil, false, flags, envBlock, nil, &si, &pi); err != nil {
		if envBlock != nil {
			_ = windows.DestroyEnvironmentBlock(envBlock)
		}
		return nil, fmt.Errorf("CreateProcessAsUser: %w", err)
	}
	stop := func() {
		_ = windows.TerminateProcess(pi.Process, 0)
		if pi.Process != 0 {
			windows.CloseHandle(pi.Process)
		}
		if pi.Thread != 0 {
			windows.CloseHandle(pi.Thread)
		}
		if envBlock != nil {
			_ = windows.DestroyEnvironmentBlock(envBlock)
		}
	}
	return stop, nil
}

// startInCurrentSession startet die Kommandozeile im aktuellen Prozesskontext.
func startInCurrentSession(cmdLine string) (func(), error) {
	cmd := exec.Command("cmd", "/c", cmdLine)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}, nil
}
