//go:build windows

package remote

import (
	"context"
	"crypto/des"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"unsafe"

	"log/slog"

	"golang.org/x/sys/windows"

	"github.com/thomaspeterson/pc-inventory/internal/agent/transport"
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
func startVNCServer(ctx context.Context, client *transport.Client, agentToken, password string, consent bool, log *slog.Logger) (string, func(), error) {
	cmdLine, cleanup, err := vncCommandLine(ctx, client, agentToken, password, consent, log)
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
func vncCommandLine(ctx context.Context, client *transport.Client, agentToken, password string, consent bool, log *slog.Logger) (string, func(), error) {
	if custom := os.Getenv("PCINV_VNC_CMD"); custom != "" {
		return custom, nil, nil
	}
	// Bevorzugt das mitgelieferte Bundle (on-demand geladen); Fallback: ein
	// installiertes UltraVNC/TightVNC (Standardpfade / PATH).
	bin, err := ensureVNCServer(ctx, client, agentToken, "windows-amd64", "winvnc.exe", log)
	if err != nil {
		if p, lerr := vncServerPath("winvnc"); lerr == nil {
			bin = p
		} else {
			return "", nil, fmt.Errorf("winvnc.exe weder im Bundle noch installiert gefunden: %w", err)
		}
	}
	// ultravnc.ini: nur Loopback, Einmalpasswort (RFB-Auth wie von noVNC erwartet),
	// optional Nachfrage beim angemeldeten Nutzer.
	ini, err := os.CreateTemp("", "pcinv-ultravnc-*.ini")
	if err != nil {
		return "", nil, err
	}
	query := 0
	if consent {
		query = 1
	}
	fmt.Fprintf(ini, "[ultravnc]\nPortNumber=5900\nAllowLoopback=1\nLoopbackOnly=1\npasswd=%s\nQuerySetting=%d\nQueryTimeout=30\n",
		ultraVNCPasswd(password), query)
	_ = ini.Close()
	cleanup := func() { _ = os.Remove(ini.Name()) }
	cmd := fmt.Sprintf(`"%s" -run -config "%s"`, bin, ini.Name())
	return cmd, cleanup, nil
}

// ultraVNCPasswd verschlüsselt das Klartext-Passwort im klassischen VNC-Format
// (DES-ECB mit festem, bit-gespiegeltem Schlüssel; max. 8 Zeichen) und liefert es
// als Hex – so speichert UltraVNC das Passwort in der ini (passwd=).
func ultraVNCPasswd(plain string) string {
	fixed := []byte{23, 82, 107, 6, 35, 78, 88, 7}
	key := make([]byte, 8)
	for i, b := range fixed {
		var r byte
		for j := 0; j < 8; j++ {
			r = (r << 1) | (b & 1)
			b >>= 1
		}
		key[i] = r
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return ""
	}
	pw := make([]byte, 8) // mit Nullen aufgefüllt / abgeschnitten
	copy(pw, []byte(plain))
	out := make([]byte, 8)
	block.Encrypt(out, pw)
	return hex.EncodeToString(out)
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
