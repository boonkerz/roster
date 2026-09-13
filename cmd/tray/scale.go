package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
)

// Basisgröße des Fensters in Oberflächen-Einheiten (vor der Skalierung).
const (
	baseWinW = 1040
	baseWinH = 620
)

// autoScale ermittelt die Skalierung: feste Vorgabe (tray.json ui_scale bzw.
// ROSTER_TRAY_SCALE) hat Vorrang, sonst fragt es SDL nach der Pixeldichte.
func (a *app) autoScale() float32 {
	if v := strings.TrimSpace(os.Getenv("ROSTER_TRAY_SCALE")); v != "" {
		if f, err := strconv.ParseFloat(v, 32); err == nil && f > 0 {
			return sdlui.ClampScale(float32(f))
		}
	}
	if a.cfg.UIScale > 0 {
		return sdlui.ClampScale(float32(a.cfg.UIScale))
	}
	return sdlui.DetectUIScale(a.win)
}

// updatePixelRatio merkt sich, wie viele Pixel eine logische Einheit sind. Gezeichnet
// wird in Pixeln (scharf auf HiDPI), Mausereignisse liefert SDL aber logisch.
func (a *app) updatePixelRatio() {
	var lw, lh, pw, ph int32
	sdl.GetWindowSize(a.win, &lw, &lh)
	sdl.GetWindowSizeInPixels(a.win, &pw, &ph)
	a.pxRatio = 1
	if lw > 0 && pw > 0 {
		a.pxRatio = float32(pw) / float32(lw)
	}
}

// applyScale setzt die Skalierung samt Schriftgröße neu.
func (a *app) applyScale(s float32) {
	s = sdlui.ClampScale(s)
	a.scale = s
	if err := a.txt.SetSize(float64(baseFontPx * s)); err != nil {
		a.setStatus("Schriftgröße: "+err.Error(), true)
	}
	a.markDirty()
}

// resizeToScale bringt das Fenster auf die Größe, die der aktuellen Skalierung
// entspricht. Nötig, wenn die Skalierung nicht vom Compositor kommt (X11): dort
// zeichnen wir größer, ohne dass das Fenster mitwächst.
func (a *app) resizeToScale() {
	w := int32(baseWinW * a.scale / a.pxRatio)
	h := int32(baseWinH * a.scale / a.pxRatio)
	var cw, ch int32
	sdl.GetWindowSize(a.win, &cw, &ch)
	if cw < w || ch < h {
		sdl.SetWindowSize(a.win, w, h)
	}
}

// rescale reagiert auf Anzeige-/Fensterwechsel (anderer Monitor, andere Skalierung).
func (a *app) rescale() {
	a.updatePixelRatio()
	if s := a.autoScale(); s != a.scale {
		a.applyScale(s)
	}
	a.markDirty()
}

// zoom verstellt die Skalierung von Hand (Strg +/−, Strg+0 = wieder automatisch)
// und merkt sich den Wert in der Konfiguration.
func (a *app) zoom(delta float32, reset bool) {
	a.mu.Lock()
	if reset {
		a.cfg.UIScale = 0
	} else {
		a.cfg.UIScale = float64(sdlui.ClampScale(a.scale + delta))
	}
	err := a.cfg.save()
	scale := a.cfg.UIScale
	a.mu.Unlock()

	a.updatePixelRatio()
	if reset {
		a.applyScale(sdlui.DetectUIScale(a.win))
		a.setStatus("Skalierung wieder automatisch ("+strconv.FormatFloat(float64(a.scale), 'f', 2, 32)+"×).", false)
	} else {
		a.applyScale(float32(scale))
		a.setStatus("Skalierung "+strconv.FormatFloat(float64(a.scale), 'f', 2, 32)+"× (Strg+0 = automatisch).", false)
	}
	if err != nil {
		a.setStatus("Skalierung nicht gespeichert: "+err.Error(), true)
	}
	a.resizeToScale()
}
