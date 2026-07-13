package main

import (
	"strings"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// connectDialog zeigt ein kleines Fenster zum Verbinden ohne Kommandozeile: der
// Startcode (aus dem Browser-Button „Kopieren") wird per Strg+V eingefügt – die
// Zwischenablage wird beim Öffnen automatisch geprüft – und mit Enter/Klick auf den
// grünen Button verbunden. Bewusst font-frei: alle Texte stehen in der Titelleiste
// (vom Fenstermanager gerendert), der Body gibt nur Status-Feedback per Farbe.
// Rückgabe (nil, nil) = vom Nutzer abgebrochen.
func connectDialog() (*launchConfig, error) {
	const w, h = 520, 230
	window := sdl.CreateWindow("PC-Inventory Fernsteuerung", w, h, 0)
	if window == nil {
		return nil, errDialog("fenster")
	}
	defer sdl.DestroyWindow(window)
	renderer := sdl.CreateRenderer(window, "")
	if renderer == nil {
		return nil, errDialog("renderer")
	}
	defer sdl.DestroyRenderer(renderer)

	var cfg *launchConfig
	buf := ""
	setTitle := func() {
		switch {
		case cfg != nil:
			sdl.SetWindowTitle(window, "Bereit: "+cfg.Device+" — Enter/Klick zum Verbinden   (Esc = Abbruch)")
		case buf != "":
			sdl.SetWindowTitle(window, "Ungültiger Startcode — Strg+V zum Einfügen   (Esc = Abbruch)")
		default:
			sdl.SetWindowTitle(window, "Startcode einfügen: Strg+V, dann Enter/Klick zum Verbinden   (Esc = Abbruch)")
		}
	}
	tryDecode := func(s string) {
		buf = strings.TrimSpace(s)
		if buf == "" {
			cfg = nil
		} else if c, derr := decodeLaunchCode(buf); derr == nil {
			cfg = c
		} else {
			cfg = nil
		}
		setTitle()
	}
	tryDecode(sdl.GetClipboardText()) // Browser-„Kopieren" direkt davor → sofort „Bereit"

	btn := sdl.FRect{X: (w - 220) / 2, Y: 150, W: 220, H: 56}
	var ev sdl.Event
	for {
		for sdl.PollEvent(&ev) {
			switch ev.Type() {
			case sdl.EventQuit, sdl.EventWindowCloseRequested:
				return nil, nil
			case sdl.EventKeyDown:
				ke := ev.Key()
				switch {
				case ke.Key == sdl.KeycodeEscape:
					return nil, nil
				case ke.Key == sdl.KeycodeReturn || ke.Key == sdl.KeycodeKpEnter:
					if cfg != nil {
						return cfg, nil
					}
				case ke.Key == sdl.KeycodeV && ke.Mod&sdl.KeymodCtrl != 0:
					tryDecode(sdl.GetClipboardText())
				}
			case sdl.EventMouseButtonDown:
				b := ev.Button()
				if b.Button == uint8(sdl.ButtonLeft) && cfg != nil &&
					b.X >= btn.X && b.X < btn.X+btn.W && b.Y >= btn.Y && b.Y < btn.Y+btn.H {
					return cfg, nil
				}
			}
		}

		sdl.SetRenderDrawColor(renderer, 0x14, 0x18, 0x20, 0xff)
		sdl.RenderClear(renderer)
		// „Eingabefeld" – füllt sich grün, wenn ein gültiger Code geladen ist.
		field := sdl.FRect{X: 40, Y: 64, W: w - 80, H: 40}
		sdl.SetRenderDrawColor(renderer, 0x0b, 0x0e, 0x14, 0xff)
		sdl.RenderFillRect(renderer, &field)
		sdl.SetRenderDrawColor(renderer, 0x33, 0x3a, 0x46, 0xff)
		sdl.RenderRect(renderer, &field)
		if len(buf) > 0 {
			barW := float32(len(buf))
			if barW > field.W-8 {
				barW = field.W - 8
			}
			if cfg != nil {
				sdl.SetRenderDrawColor(renderer, 0x2e, 0x7d, 0x32, 0xff)
			} else {
				sdl.SetRenderDrawColor(renderer, 0x8a, 0x3b, 0x3b, 0xff)
			}
			sdl.RenderFillRect(renderer, &sdl.FRect{X: field.X + 4, Y: field.Y + 12, W: barW, H: 16})
		}
		// Connect-Button (grün = bereit) mit Play-Dreieck.
		if cfg != nil {
			sdl.SetRenderDrawColor(renderer, 0x2e, 0x7d, 0x32, 0xff)
		} else {
			sdl.SetRenderDrawColor(renderer, 0x30, 0x36, 0x40, 0xff)
		}
		sdl.RenderFillRect(renderer, &btn)
		drawPlay(renderer, btn, cfg != nil)
		sdl.RenderPresent(renderer)
		time.Sleep(16 * time.Millisecond)
	}
}

// drawPlay zeichnet ein nach rechts zeigendes Play-Dreieck mittig in r.
func drawPlay(renderer *sdl.Renderer, r sdl.FRect, active bool) {
	if active {
		sdl.SetRenderDrawColor(renderer, 0xff, 0xff, 0xff, 0xff)
	} else {
		sdl.SetRenderDrawColor(renderer, 0x66, 0x6c, 0x78, 0xff)
	}
	const size = 16
	cx, cy := r.X+r.W/2-size/2, r.Y+r.H/2
	for dy := float32(-size); dy <= size; dy++ {
		wln := size - abs32(dy)
		sdl.RenderLine(renderer, cx, cy+dy, cx+wln, cy+dy)
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func errDialog(what string) error {
	return &dialogError{what + ": " + sdl.GetError()}
}

type dialogError struct{ msg string }

func (e *dialogError) Error() string { return e.msg }
