package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
)

// Einstellungen-Ansicht: Erscheinungsbild (Dunkel/Hell) und der SFTP-Knopf
// (Benutzer, SSH-Schlüssel, optionales Programm). Alles landet in tray.json –
// dieselben Schlüssel, die man auch von Hand setzen kann.

// openSettings füllt das Formular aus der Konfiguration und wechselt die Ansicht.
func (a *app) openSettings() {
	a.mu.Lock()
	a.sform = settingsForm{
		fields: [2]string{a.cfg.SFTPUser, a.cfg.SFTPCommand},
		theme:  themeName(a.cfg.Theme),
		key:    a.cfg.SFTPKey,
		keys:   sshKeys(),
	}
	// Ein konfigurierter Schlüssel außerhalb von ~/.ssh bleibt wählbar.
	if a.sform.key != "" && !containsStr(a.sform.keys, a.sform.key) {
		a.sform.keys = append(a.sform.keys, a.sform.key)
	}
	a.view = viewSettings
	a.mu.Unlock()
	a.markDirty()
}

func themeName(s string) string {
	if s == "light" {
		return "light"
	}
	return "dark"
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// clickSettings verarbeitet die Knöpfe der Einstellungen (Hit-IDs "set:…").
func (a *app) clickSettings(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case id == "set:save":
		a.cfg.SFTPUser = strings.TrimSpace(a.sform.fields[0])
		a.cfg.SFTPCommand = strings.TrimSpace(a.sform.fields[1])
		a.cfg.SFTPKey = a.sform.key
		a.cfg.Theme = a.sform.theme
		if a.cfg.Theme == "dark" {
			a.cfg.Theme = "" // Vorgabe nicht in die Datei schreiben
		}
		if err := a.cfg.save(); err != nil {
			a.status, a.statusBad = "Einstellungen nicht gespeichert: "+err.Error(), true
		} else {
			a.status, a.statusBad = "Einstellungen gespeichert.", false
		}
		a.view = viewList
	case id == "set:cancel":
		applyTheme(a.cfg.Theme) // Vorschau zurücknehmen
		a.view = viewList
	case strings.HasPrefix(id, "set:theme:"):
		a.sform.theme = themeName(strings.TrimPrefix(id, "set:theme:"))
		applyTheme(a.sform.theme)
	case id == "set:key:":
		a.sform.key = ""
	case strings.HasPrefix(id, "set:key:"):
		if i, err := strconv.Atoi(strings.TrimPrefix(id, "set:key:")); err == nil && i >= 0 && i < len(a.sform.keys) {
			a.sform.key = a.sform.keys[i]
		}
	case strings.HasPrefix(id, "set:f"):
		if n := int(id[len("set:f")] - '0'); n >= 0 && n < len(a.sform.fields) {
			a.sform.focus = n
		}
	}
	a.markDirty()
}

// drawSettings zeichnet die Einstellungen-Ansicht.
func (a *app) drawSettings(w, h float32) {
	s := a.s()
	lineH := a.txt.LineH()
	a.mu.Lock()
	f := a.sform
	a.mu.Unlock()

	pw := minF(640*s, w-40*s)
	px := (w - pw) / 2
	py := 20 * s
	ph := h - py - footerH*s - 12*s
	sdlui.FillRound(a.rn, px, py, pw, ph, 10*s, colPanel[0], colPanel[1], colPanel[2], 0xff)
	a.addHit("settings:bg", px, py, pw, ph)

	x := px + 18*s
	y := py + 16*s
	a.txt.Draw("Einstellungen", x, y, colStrong[0], colStrong[1], colStrong[2])
	y += lineH + 16*s
	bh := 26 * s

	// --- Erscheinungsbild ---
	a.txt.Draw("Erscheinungsbild", x, y, colMuted[0], colMuted[1], colMuted[2])
	y += lineH + 6*s
	bx := x
	for _, opt := range [][2]string{{"dark", "Dunkel"}, {"light", "Hell"}} {
		accent := 0
		if f.theme == opt[0] {
			accent = 1
		}
		bx += a.button("set:theme:"+opt[0], opt[1], bx, y, bh, accent) + 6*s
	}
	y += bh + 16*s

	// --- SFTP-Benutzer ---
	a.txt.Draw("SFTP-Benutzer (leer = kein Benutzername in der Adresse)", x, y, colMuted[0], colMuted[1], colMuted[2])
	y += lineH + 4*s
	y = a.textField("set:f0", x, y, pw-36*s, f.fields[0], f.focus == 0) + 14*s

	// --- SSH-Schlüssel ---
	a.txt.Draw("SSH-Schlüssel für SFTP", x, y, colMuted[0], colMuted[1], colMuted[2])
	y += lineH + 6*s
	bx = x
	accent := 0
	if f.key == "" {
		accent = 1
	}
	bx += a.button("set:key:", "keiner (Agent/Standard)", bx, y, bh, accent) + 6*s
	for i, k := range f.keys {
		label := filepath.Base(k)
		bw := a.txt.Width(label) + 2*padX*s
		if bx+bw > px+pw-18*s { // Zeilenumbruch bei vielen Schlüsseln
			bx = x
			y += bh + 6*s
		}
		accent = 0
		if f.key == k {
			accent = 1
		}
		bx += a.button("set:key:"+strconv.Itoa(i), label, bx, y, bh, accent) + 6*s
	}
	y += bh + 6*s
	hint := "Dateimanager nehmen über sftp:// keinen Schlüssel an (dort hilft nur ~/.ssh/config). Mit Schlüssel öffnet der Knopf „sftp -i“ im Terminal bzw. WinSCP."
	for _, ln := range a.wrap(hint, pw-36*s) {
		a.txt.Draw(ln, x, y, colMuted[0], colMuted[1], colMuted[2])
		y += lineH + 2*s
	}
	y += 10 * s

	// --- SFTP-Programm ---
	a.txt.Draw("SFTP-Programm (optional, statt Desktop/Terminal)", x, y, colMuted[0], colMuted[1], colMuted[2])
	y += lineH + 4*s
	y = a.textField("set:f1", x, y, pw-36*s, f.fields[1], f.focus == 1) + 4*s
	a.txt.Draw(a.txt.Ellipsize("Platzhalter: {{host}} {{user}} {{url}} {{name}} {{key}} – z. B. filezilla {{url}}", pw-36*s),
		x, y, colMuted[0], colMuted[1], colMuted[2])
	y += lineH + 18*s

	// --- Knöpfe ---
	bx = x
	bx += a.button("set:save", "Speichern", bx, y, 32*s, 1) + 8*s
	a.button("set:cancel", "Abbrechen", bx, y, 32*s, 0)
	y += 32*s + 10*s
	a.txt.Draw(a.txt.Clip(fmt.Sprintf("Gespeichert in %s", configPathHint()), pw-36*s), x, y, colMuted[0], colMuted[1], colMuted[2])
}

// textField zeichnet ein einzeiliges Eingabefeld mit Schreibmarke und liefert die
// Unterkante. Die Tastatureingabe läuft über typeText/keyDown nach Fokus.
func (a *app) textField(id string, x, y, w float32, val string, focused bool) float32 {
	s := a.s()
	lineH := a.txt.LineH()
	fieldH := 30 * s
	bg := colRow
	if focused {
		bg = colFieldFocus
	}
	sdlui.FillRound(a.rn, x, y, w, fieldH, 6*s, bg[0], bg[1], bg[2], 0xff)
	sdl.SetRenderDrawColor(a.rn, colLine[0], colLine[1], colLine[2], 0xff)
	if focused {
		sdl.SetRenderDrawColor(a.rn, colAccent[0], colAccent[1], colAccent[2], 0xff)
	}
	sdl.RenderRect(a.rn, &sdl.FRect{X: x, Y: y, W: w, H: fieldH})
	shown := a.txt.Clip(val, w-16*s)
	tw := a.txt.Draw(shown, x+9*s, y+(fieldH-lineH)/2, colText[0], colText[1], colText[2])
	if focused {
		sdl.SetRenderDrawColor(a.rn, colText[0], colText[1], colText[2], 0xff)
		sdl.RenderFillRect(a.rn, &sdl.FRect{X: x + 10*s + tw, Y: y + 6*s, W: 2 * s, H: fieldH - 12*s})
	}
	a.addHit(id, x, y, w, fieldH)
	return y + fieldH
}

func configPathHint() string {
	if p, err := configPath(); err == nil {
		return p
	}
	return "tray.json"
}
