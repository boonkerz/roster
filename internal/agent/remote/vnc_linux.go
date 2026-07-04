//go:build linux

package remote

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"log/slog"

	"github.com/thomaspeterson/pc-inventory/internal/agent/transport"
)

// startVNCServer startet on-demand x11vnc gegen die aktive X-Session, gebunden an
// 127.0.0.1:5900, mit Einmalpasswort. -once beendet den Server nach dem Trennen des
// (einzigen) Clients; stop() räumt zusätzlich auf. Bei consent zeigt x11vnc dem
// angemeldeten Nutzer einen Bestätigungsdialog.
func startVNCServer(ctx context.Context, client *transport.Client, agentToken, password string, consent bool, log *slog.Logger) (string, func(), error) {
	pw, err := os.CreateTemp("", "pcinv-vnc-*.pw")
	if err != nil {
		return "", nil, err
	}
	_, _ = pw.WriteString(password + "\n")
	_ = pw.Close()

	// PCINV_VNC_CMD erlaubt es, statt x11vnc einen eigenen VNC-Server zu starten
	// (z.B. wayvnc unter Wayland). Der Befehl muss auf 127.0.0.1:5900 lauschen; das
	// Passwort steht als $VNC_PASSWORD/{pwfile} bereit.
	var cmd *exec.Cmd
	if custom := os.Getenv("PCINV_VNC_CMD"); custom != "" {
		cmd = exec.Command("sh", "-c", custom)
		cmd.Env = append(os.Environ(), "VNC_PASSWORD="+password, "VNC_PASSWORD_FILE="+pw.Name())
	} else {
		// Bevorzugt das mitgelieferte Bundle (on-demand geladen); Fallback: PATH.
		bin, berr := ensureVNCServer(ctx, client, agentToken, "linux-amd64", "x11vnc", log)
		if berr != nil {
			bin, err = vncServerPath("x11vnc")
			if err != nil {
				_ = os.Remove(pw.Name())
				return "", nil, fmt.Errorf("x11vnc weder im Bundle noch installiert: %w", berr)
			}
		}
		args := []string{
			"-localhost", "-rfbport", "5900",
			"-passwdfile", "plain:" + pw.Name(),
			"-display", ":0", "-auth", "guess",
			"-once", "-noxdamage", "-quiet",
		}
		if consent {
			args = append(args, "-accept", "popup:t=30") // Nachfrage am Gerät, 30s Timeout
		}
		cmd = exec.Command(bin, args...)
	}
	if err := cmd.Start(); err != nil {
		_ = os.Remove(pw.Name())
		return "", nil, err
	}
	log.Info("x11vnc gestartet", "pid", cmd.Process.Pid, "consent", consent)

	stop := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		_ = os.Remove(pw.Name())
	}
	return "127.0.0.1:5900", stop, nil
}
