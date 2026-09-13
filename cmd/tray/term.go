package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// runTerminal ist der Terminal-Modus (roster-tray --term <gerät>): er verbindet
// stdin/stdout im Rohmodus mit der Terminal-WebSocket des Servers. Damit übernimmt
// das echte Terminal-Fenster (kitty, gnome-terminal, wt.exe …) Scrollback,
// Copy & Paste und Farben – die App muss keinen VT-Emulator mitbringen.
//
// Protokoll (wie im Web-UI): Binärframes = rohe Terminal-Ein-/Ausgabe,
// Textframes = JSON-Steuerung ({"type":"resize",…} bzw. {"type":"exit","code":n}).
func runTerminal(cfg *config, deviceID, shell, runas string, hold bool) error {
	if cfg.URL == "" || cfg.Token == "" {
		return fmt.Errorf("nicht angemeldet – zuerst die Taskleisten-App starten und anmelden")
	}
	if runas != "user" {
		runas = "system"
	}
	if shell == "" {
		shell = "shell"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc := &http.Client{}
	if cfg.Insecure {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	endpoint := fmt.Sprintf("%s/api/v1/devices/%s/terminal?shell=%s&runas=%s",
		wsBase(strings.TrimRight(cfg.URL, "/")), deviceID, shell, runas)

	dialCtx, dialCancel := context.WithTimeout(ctx, 30*time.Second)
	conn, _, err := websocket.Dial(dialCtx, endpoint, &websocket.DialOptions{
		HTTPClient: hc,
		HTTPHeader: http.Header{"Authorization": {"Bearer " + cfg.Token}},
	})
	dialCancel()
	if err != nil {
		return finish(hold, fmt.Errorf("verbindung fehlgeschlagen: %w", err))
	}
	defer conn.CloseNow()
	conn.SetReadLimit(4 << 20)

	restore, rawErr := makeRaw()
	restored := rawErr != nil // ohne Rohmodus gibt es nichts zurückzusetzen
	resetTerm := func() {
		if !restored {
			restore()
			restored = true
		}
	}
	defer resetTerm()

	sendResize := func() {
		cols, rows, err := termSize()
		if err != nil || cols <= 0 || rows <= 0 {
			return
		}
		msg, _ := json.Marshal(map[string]any{"type": "resize", "cols": cols, "rows": rows})
		_ = conn.Write(ctx, websocket.MessageText, msg)
	}
	sendResize()

	resized := make(chan struct{}, 1)
	stopWatch := watchResize(resized)
	defer stopWatch()
	go func() {
		for range resized {
			sendResize()
		}
	}()

	// stdin → Gerät.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Gerät → stdout, bis die Gegenseite schließt.
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		switch typ {
		case websocket.MessageBinary:
			_, _ = os.Stdout.Write(data)
		case websocket.MessageText:
			var ctl struct {
				Type string `json:"type"`
				Code int    `json:"code"`
			}
			if json.Unmarshal(data, &ctl) == nil && ctl.Type == "exit" {
				resetTerm()
				if ctl.Code == -1 {
					return finish(hold, fmt.Errorf("agent nicht erreichbar (Gerät offline?)"))
				}
				return finish(hold, nil)
			}
		}
	}
	resetTerm()
	return finish(hold, nil)
}

// finish hält das Fenster offen, wenn die App das Terminal selbst geöffnet hat –
// sonst verschwände eine Fehlermeldung sofort mit dem Fenster.
func finish(hold bool, err error) error {
	if !hold {
		return err
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "\r\nFehler: %v\r\n", err)
	} else {
		fmt.Fprint(os.Stderr, "\r\nSitzung beendet.\r\n")
	}
	fmt.Fprint(os.Stderr, "Zum Schließen Enter drücken …")
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
	return nil
}
