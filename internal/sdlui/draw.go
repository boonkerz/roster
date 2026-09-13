package sdlui

import (
	"math"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// FillRound zeichnet ein gefülltes Rechteck mit abgerundeten Ecken (zeilenweise).
func FillRound(rn *sdl.Renderer, x, y, w, h, rad float32, cr, cg, cb, ca uint8) {
	sdl.SetRenderDrawColor(rn, cr, cg, cb, ca)
	if rad > h/2 {
		rad = h / 2
	}
	if rad > w/2 {
		rad = w / 2
	}
	rows := int(h)
	for i := 0; i < rows; i++ {
		fi := float32(i) + 0.5
		inset := float32(0)
		var d float32 = -1
		if fi < rad {
			d = rad - fi
		} else if fi > h-rad {
			d = fi - (h - rad)
		}
		if d >= 0 {
			inset = rad - float32(math.Sqrt(float64(rad*rad-d*d)))
		}
		sdl.RenderFillRect(rn, &sdl.FRect{X: x + inset, Y: y + float32(i), W: w - 2*inset, H: 1})
	}
}

// FillDisc zeichnet einen gefüllten Kreis mit Mittelpunkt (cx,cy) – Statuspunkte.
func FillDisc(rn *sdl.Renderer, cx, cy, rad float32, cr, cg, cb, ca uint8) {
	sdl.SetRenderDrawColor(rn, cr, cg, cb, ca)
	for dy := -rad; dy <= rad; dy++ {
		half := float32(math.Sqrt(float64(rad*rad - dy*dy)))
		sdl.RenderFillRect(rn, &sdl.FRect{X: cx - half, Y: cy + dy, W: 2 * half, H: 1})
	}
}

// DetectUIScale ermittelt die HiDPI-Skalierung für Fenster/Font: Pixeldichte des
// Fensters (Wayland: fraktionale Skalierung, z. B. 1,75), Display-Scale und das
// Verhältnis Pixel/Logikgröße – der größte Wert gewinnt.
//
// Achtung: unter dem X11-Treiber (auch XWayland) meldet SDL nichts davon, dort
// bleibt es bei 1,0 – siehe PreferWayland und die manuelle Skalierung der Apps.
func DetectUIScale(window *sdl.Window) float32 {
	s := sdl.GetWindowDisplayScale(window)
	if d := sdl.GetWindowPixelDensity(window); d > s {
		s = d
	}
	if cs := sdl.GetDisplayContentScale(sdl.GetPrimaryDisplay()); cs > s {
		s = cs
	}
	var lw, lh, pw, ph int32
	sdl.GetWindowSize(window, &lw, &lh)
	sdl.GetWindowSizeInPixels(window, &pw, &ph)
	if lw > 0 && pw > 0 {
		if pr := float32(pw) / float32(lw); pr > s {
			s = pr
		}
	}
	if s < 1 { // Auto-Erkennung nie unter 1 skalieren.
		s = 1
	}
	return ClampScale(s)
}

// ClampScale hält die UI-Skalierung in einem sinnvollen Bereich.
func ClampScale(s float32) float32 {
	if s < 0.6 {
		return 0.6
	}
	if s > 4 {
		return 4
	}
	return s
}
