package remote

import (
	"context"
	"fmt"

	"log/slog"

	"github.com/coder/websocket"

	"github.com/thomaspeterson/pc-inventory/internal/agent/transport"
)

// handleVNC bedient eine Fernsteuerungs-Sitzung: der Agent ist selbst der VNC-Server.
// Er verbindet sich per WebSocket mit dem Server und fährt darauf den eingebauten
// RFB-Server (rfb.go), der den Bildschirm aufnimmt und Eingaben umsetzt. Keine
// Fremdsoftware.
func handleVNC(ctx context.Context, client *transport.Client, agentToken, session, _ string, consent bool, monitor int, log *slog.Logger) {
	conn, err := client.DialTerminal(ctx, agentToken, session)
	if err != nil {
		log.Warn("vnc-websocket fehlgeschlagen", "err", err)
		return
	}
	defer conn.CloseNow()

	// Zielgruppenabhängige Zustimmung: der angemeldete Nutzer muss bestätigen.
	if consent && !confirmRemote(log) {
		log.Info("fernsteuerung am gerät abgelehnt/keine antwort")
		conn.Close(websocket.StatusPolicyViolation, "am gerät abgelehnt")
		return
	}

	src, err := newResilientSource(log, monitor)
	if err != nil {
		log.Warn("bildschirmaufnahme nicht verfügbar", "err", err)
		conn.Close(websocket.StatusInternalError, "keine aufnahme")
		return
	}
	defer src.Close()

	w, h := src.Bounds()
	log.Info("fernsteuerung: rfb-server startet", "size", fmt.Sprintf("%dx%d", w, h))
	nc := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	if err := rfbServe(ctx, nc, src, log); err != nil && ctx.Err() == nil {
		log.Debug("rfb-server beendet", "err", err)
	}
	conn.Close(websocket.StatusNormalClosure, "ende")
	log.Info("fernsteuerung beendet", "session", session)
}
