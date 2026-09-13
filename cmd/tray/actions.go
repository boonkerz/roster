package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

// submitLogin meldet im Hintergrund an und legt dabei das API-Token an. Die Maske
// bleibt währenddessen sichtbar (busy), Fehler landen direkt darunter.
func (a *app) submitLogin() {
	a.mu.Lock()
	if a.form.busy {
		a.mu.Unlock()
		return
	}
	base := normalizeBase(a.form.fields[0])
	user := strings.TrimSpace(a.form.fields[1])
	pass := a.form.fields[2]
	code := strings.TrimSpace(a.form.fields[3])
	insecure := a.cfg.Insecure
	switch {
	case base == "":
		a.form.err = "Serveradresse fehlt."
		a.mu.Unlock()
		a.markDirty()
		return
	case user == "" || pass == "":
		a.form.err = "Benutzer und Passwort ausfüllen."
		a.mu.Unlock()
		a.markDirty()
		return
	}
	a.form.busy, a.form.err = true, ""
	a.mu.Unlock()
	a.markDirty()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		token, tokenID, err := login(ctx, base, user, pass, code, insecure)

		a.mu.Lock()
		a.form.busy = false
		switch {
		case errors.Is(err, errTOTPRequired):
			a.form.needsCode = true
			a.form.focus = 3
			a.form.err = "Zwei-Faktor-Code eingeben."
		case err != nil:
			a.form.err = loginHint(err)
		default:
			a.cfg.URL, a.cfg.User = base, user
			a.cfg.Token, a.cfg.TokenID = token, tokenID
			if serr := a.cfg.save(); serr != nil {
				a.form.err = "Konfiguration konnte nicht gespeichert werden: " + serr.Error()
			}
			a.cli = newClient(base, token, insecure)
			a.view = viewList
			a.form.fields[2], a.form.fields[3] = "", ""
			a.form.needsCode = false
			a.status, a.statusBad = "Angemeldet als "+user, false
		}
		loggedIn := a.cli != nil
		a.mu.Unlock()
		a.markDirty()
		if loggedIn {
			a.requestRefresh()
		}
	}()
}

// loginHint übersetzt typische Fehler in einen brauchbaren Hinweis.
func loginHint(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") {
		return "TLS-Zertifikat nicht vertrauenswürdig – für Testserver \"insecure\": true in tray.json setzen."
	}
	return msg
}

// logout widerruft das Token serverseitig und kehrt zur Anmeldemaske zurück.
func (a *app) logout() {
	a.mu.Lock()
	cli, tokenID := a.cli, a.cfg.TokenID
	a.cli = nil
	a.devices = nil
	a.view = viewLogin
	a.form.fields[0], a.form.fields[1] = a.cfg.URL, a.cfg.User
	a.form.fields[2], a.form.fields[3] = "", ""
	a.form.needsCode, a.form.busy, a.form.err = false, false, ""
	a.form.focus = 2
	a.status, a.statusBad = "Abgemeldet.", false
	a.cfg.Token, a.cfg.TokenID = "", ""
	_ = a.cfg.save()
	a.username = ""
	a.mu.Unlock()
	a.markDirty()
	a.trayDirty.Store(true)

	if cli != nil && tokenID != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = cli.revokeToken(ctx, tokenID)
		}()
	}
}

// actTerminal öffnet das Remote-Terminal im System-Terminal.
func (a *app) actTerminal(d device) {
	if err := openTerminal(a.cfg, d); err != nil {
		a.setStatus("Terminal: "+err.Error(), true)
		return
	}
	a.setStatus("Terminal zu "+d.Hostname+" gestartet.", false)
}

// actWeb öffnet die Geräteseite im Browser.
func (a *app) actWeb(d device) {
	if err := openWeb(a.cfg, d); err != nil {
		a.setStatus("Browser: "+err.Error(), true)
	}
}

// actSFTP öffnet die Dateiübertragung zum Gerät im SFTP-Programm des Desktops.
func (a *app) actSFTP(d device) {
	prog, err := openSFTP(a.cfg, d)
	if err != nil {
		a.setStatus("SFTP: "+err.Error(), true)
		return
	}
	a.setStatus(prog+" für "+d.Hostname+" ("+deviceAddress(d)+") gestartet.", false)
}

// actViewer fordert die Fernsteuerungs-Sitzung an und startet den nativen Viewer.
func (a *app) actViewer(d device) {
	a.mu.Lock()
	cli := a.cli
	a.mu.Unlock()
	if cli == nil {
		return
	}
	a.setStatus("Fernsteuerung zu "+d.Hostname+" wird gestartet …", false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := openViewer(ctx, cli, a.cfg, d); err != nil {
			a.setStatus("Fernsteuerung: "+err.Error(), true)
			return
		}
		a.setStatus("Viewer für "+d.Hostname+" gestartet.", false)
	}()
}
