// Package remote stellt die On-Demand-Echtzeitfunktionen des Agents bereit
// (aktuell das interaktive Remote-Terminal). Der Agent hält KEINE Dauerverbindung,
// sondern parkt einen leichten Wake-Long-Poll und „meldet sich" erst, wenn der
// Server eine Session anfordert.
package remote

import (
	"context"
	"log/slog"
	"time"

	"github.com/thomaspeterson/pc-inventory/internal/agent/transport"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// Run betreibt die Wake-Schleife, bis ctx endet. Bei jedem Auftrag wird die
// passende Session-Behandlung asynchron gestartet. onCheckin wird aufgerufen, wenn
// der Server einen sofortigen Checkin anfordert (z.B. nach einem neuen Befehl).
func Run(ctx context.Context, client *transport.Client, agentToken string, log *slog.Logger, onCheckin func()) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		wr, err := client.Wait(ctx, agentToken)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Debug("wake-poll fehlgeschlagen", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		switch wr.Type {
		case "checkin":
			if onCheckin != nil {
				onCheckin()
			}
		case "open_terminal":
			log.Info("terminal angefordert", "session", wr.Session, "shell", wr.Shell, "runas", wr.RunAs)
			go func(s shared.WaitResponse) {
				// Eine Terminal-Session darf den Agent niemals beenden: ein Panic
				// in einer Goroutine würde sonst den gesamten Prozess reißen.
				defer func() {
					if r := recover(); r != nil {
						log.Error("terminal-session abgebrochen (panic abgefangen)", "session", s.Session, "err", r)
					}
				}()
				handleTerminal(ctx, client, agentToken, s.Session, s.Shell, s.RunAs, log)
			}(wr)
		case "open_vnc":
			log.Info("fernsteuerung angefordert", "session", wr.Session, "consent", wr.Consent)
			go func(s shared.WaitResponse) {
				defer func() {
					if r := recover(); r != nil {
						log.Error("vnc-session abgebrochen (panic abgefangen)", "session", s.Session, "err", r)
					}
				}()
				handleVNC(ctx, client, agentToken, s.Session, s.Password, s.Consent, s.Monitor, log)
			}(wr)
		}
	}
}
