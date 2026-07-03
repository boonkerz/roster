package remote

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"log/slog"

	"github.com/coder/websocket"

	"github.com/thomaspeterson/pc-inventory/internal/agent/transport"
)

// vncServerPath liefert den Pfad zum nativen VNC-Server. Gesucht wird neben der
// Agent-EXE (dort kann man das Binary einfach hinlegen) und im PATH. Das
// On-demand-Bündeln (Download vom Server + Cache) folgt separat.
func vncServerPath(name string) (string, error) {
	exe := name
	if runtime.GOOS == "windows" && filepath.Ext(exe) == "" {
		exe = name + ".exe"
	}
	if self, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(self), exe)
		if st, serr := os.Stat(cand); serr == nil && !st.IsDir() {
			return cand, nil
		}
	}
	for _, cand := range wellKnownVNCPaths(exe) {
		if st, serr := os.Stat(cand); serr == nil && !st.IsDir() {
			return cand, nil
		}
	}
	return exec.LookPath(exe)
}

// wellKnownVNCPaths listet übliche Installationsorte des VNC-Servers je Plattform.
func wellKnownVNCPaths(exe string) []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	var out []string
	for _, base := range []string{pf, pf86} {
		if base == "" {
			continue
		}
		out = append(out,
			filepath.Join(base, "uvnc bvba", "UltraVNC", exe),
			filepath.Join(base, "UltraVNC", exe),
			filepath.Join(base, "TightVNC", exe),
		)
	}
	return out
}

// handleVNC bedient eine Fernsteuerungs-Sitzung: es startet on-demand einen
// nativen VNC-Server (loopback), verbindet sich per WebSocket mit dem Server und
// relayed die rohen RFB-Bytes bidirektional zwischen WS und lokalem VNC-Server.
// Der VNC-Server läuft nur während der Sitzung und wird danach beendet.
func handleVNC(ctx context.Context, client *transport.Client, agentToken, session, password string, consent bool, log *slog.Logger) {
	addr, stop, err := startVNCServer(ctx, password, consent, log)
	if err != nil {
		log.Warn("vnc-server start fehlgeschlagen", "err", err)
		return
	}
	defer stop()

	conn, err := client.DialTerminal(ctx, agentToken, session)
	if err != nil {
		log.Warn("vnc-websocket fehlgeschlagen", "err", err)
		return
	}
	defer conn.CloseNow()

	tcp, err := dialVNC(ctx, addr)
	if err != nil {
		log.Warn("verbindung zum vnc-server fehlgeschlagen", "addr", addr, "err", err)
		return
	}
	defer tcp.Close()

	relayWSTCP(ctx, conn, tcp)
	conn.Close(websocket.StatusNormalClosure, "ende")
	log.Info("fernsteuerung beendet", "session", session)
}

// dialVNC verbindet sich mit dem lokalen VNC-Server; der braucht nach dem Start
// einen Moment, bis er lauscht – daher kurzer Retry (~3s).
func dialVNC(ctx context.Context, addr string) (net.Conn, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp", addr)
		if err == nil {
			return c, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return nil, lastErr
}

// relayWSTCP kopiert Bytes bidirektional zwischen der WebSocket (Binär-Frames) und
// der TCP-Verbindung zum VNC-Server. Endet, sobald eine Seite schließt.
func relayWSTCP(ctx context.Context, ws *websocket.Conn, tcp net.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// VNC-Server -> WS (Bildschirmdaten)
	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			n, rerr := tcp.Read(buf)
			if n > 0 {
				if werr := ws.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				return
			}
		}
	}()

	// WS -> VNC-Server (Eingaben)
	for {
		typ, data, rerr := ws.Read(ctx)
		if rerr != nil {
			return
		}
		if typ == websocket.MessageBinary && len(data) > 0 {
			if _, werr := tcp.Write(data); werr != nil {
				return
			}
		}
	}
}
