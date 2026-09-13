package main

import (
	"strings"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// handleEvent verarbeitet ein SDL-Ereignis. Alle Treffer laufen über die beim
// Zeichnen gesammelten Hit-Bereiche (a.hits).
func (a *app) handleEvent(ev *sdl.Event) {
	switch ev.Type() {
	case sdl.EventQuit:
		a.quitting = true

	case sdl.EventWindowCloseRequested:
		a.hideWindow()

	case sdl.EventWindowResized, sdl.EventWindowExposed, sdl.EventWindowFocusGained:
		a.markDirty()

	case sdl.EventWindowPixelSizeChanged, sdl.EventWindowDisplayScaleChanged, sdl.EventWindowDisplayChanged:
		a.rescale() // anderer Monitor / andere Skalierung

	case sdl.EventMouseMotion:
		m := ev.Motion()
		if id := a.hitAt(m.X*a.pxRatio, m.Y*a.pxRatio); id != a.hover {
			a.hover = id
			a.markDirty()
		}

	case sdl.EventMouseButtonDown:
		b := ev.Button()
		if b.Button != uint8(sdl.ButtonLeft) {
			return
		}
		a.hover = a.hitAt(b.X*a.pxRatio, b.Y*a.pxRatio)
		a.click(a.hover, b.Clicks)

	case sdl.EventMouseWheel:
		w := ev.Wheel()
		a.scroll -= w.Y * float32(rowHeight) * a.s() / 2
		if a.scroll < 0 {
			a.scroll = 0
		}
		a.markDirty()

	case sdl.EventTextInput:
		te := ev.Text()
		a.typeText(te.Text())

	case sdl.EventKeyDown:
		a.keyDown(ev.Key())
	}
}

// hitAt liefert die ID des Bereichs unter (x,y) – der zuletzt gezeichnete gewinnt,
// damit Knöpfe über ihrer Zeile liegen.
func (a *app) hitAt(x, y float32) string {
	for i := len(a.hits) - 1; i >= 0; i-- {
		h := a.hits[i]
		if x >= h.x && x < h.x+h.w && y >= h.y && y < h.y+h.h {
			return h.id
		}
	}
	return ""
}

func (a *app) click(id string, clicks uint8) {
	switch {
	case id == "":
		return
	case id == "refresh":
		a.requestRefresh()
	case id == "logout":
		a.logout()
	case id == "login":
		a.submitLogin()
	case strings.HasPrefix(id, "f"):
		if n := int(id[1] - '0'); n >= 0 && n <= 3 {
			a.mu.Lock()
			a.form.focus = n
			a.mu.Unlock()
			a.markDirty()
		}
	case strings.HasPrefix(id, "pvetoggle:"):
		a.toggleHost(strings.TrimPrefix(id, "pvetoggle:"))
	case strings.HasPrefix(id, "pve:"):
		a.clickGuest(id)
	case strings.HasPrefix(id, "sftp:"):
		if d, ok := a.deviceByID(strings.TrimPrefix(id, "sftp:")); ok {
			a.actSFTP(d)
		}
	case strings.HasPrefix(id, "term:"):
		if d, ok := a.deviceByID(strings.TrimPrefix(id, "term:")); ok {
			a.actTerminal(d)
		}
	case strings.HasPrefix(id, "view:"):
		if d, ok := a.deviceByID(strings.TrimPrefix(id, "view:")); ok {
			a.actViewer(d)
		}
	case strings.HasPrefix(id, "web:"):
		if d, ok := a.deviceByID(strings.TrimPrefix(id, "web:")); ok {
			a.actWeb(d)
		}
	case strings.HasPrefix(id, "row:") && clicks >= 2:
		if d, ok := a.deviceByID(strings.TrimPrefix(id, "row:")); ok {
			a.actWeb(d)
		}
	}
}

// typeText nimmt Tastatureingaben entgegen: in der Liste ins Suchfeld, in der
// Anmeldemaske ins fokussierte Feld.
func (a *app) typeText(s string) {
	if s == "" {
		return
	}
	a.mu.Lock()
	if a.view == viewLogin {
		a.form.fields[a.form.focus] += s
	} else {
		a.filter += s
		a.scroll = 0
	}
	a.mu.Unlock()
	a.markDirty()
}

func (a *app) keyDown(ke sdl.KeyboardEvent) {
	ctrl := ke.Mod&sdl.KeymodCtrl != 0
	a.mu.Lock()
	login := a.view == viewLogin
	a.mu.Unlock()

	switch {
	case ctrl && ke.Key == sdl.KeycodeQ:
		a.quitting = true
		return
	case ctrl && ke.Key == sdl.KeycodeR:
		a.requestRefresh()
		return
	case ctrl && ke.Key == sdl.KeycodeV:
		a.typeText(strings.TrimSpace(sdl.GetClipboardText()))
		return
	case ctrl && (ke.Key == sdl.KeycodePlus || ke.Key == sdl.KeycodeEquals || ke.Key == sdl.KeycodeKpPlus):
		a.zoom(0.1, false) // Oberfläche größer
		return
	case ctrl && (ke.Key == sdl.KeycodeMinus || ke.Key == sdl.KeycodeKpMinus):
		a.zoom(-0.1, false)
		return
	case ctrl && (ke.Key == sdl.Keycode0 || ke.Key == sdl.KeycodeKp0):
		a.zoom(0, true) // wieder automatisch
		return
	}

	switch ke.Key {
	case sdl.KeycodeEscape:
		a.mu.Lock()
		hadFilter := !login && a.filter != ""
		if hadFilter {
			a.filter = ""
		}
		a.mu.Unlock()
		if hadFilter {
			a.markDirty()
			return
		}
		a.hideWindow()

	case sdl.KeycodeF5:
		a.requestRefresh()

	case sdl.KeycodeBackspace:
		a.mu.Lock()
		if login {
			f := []rune(a.form.fields[a.form.focus])
			if len(f) > 0 {
				a.form.fields[a.form.focus] = string(f[:len(f)-1])
			}
		} else {
			f := []rune(a.filter)
			if len(f) > 0 {
				a.filter = string(f[:len(f)-1])
			}
		}
		a.mu.Unlock()
		a.markDirty()

	case sdl.KeycodeTab:
		if !login {
			return
		}
		a.mu.Lock()
		n := 3
		if a.form.needsCode {
			n = 4
		}
		if ke.Mod&sdl.KeymodShift != 0 {
			a.form.focus = (a.form.focus + n - 1) % n
		} else {
			a.form.focus = (a.form.focus + 1) % n
		}
		a.mu.Unlock()
		a.markDirty()

	case sdl.KeycodeReturn, sdl.KeycodeKpEnter:
		if login {
			a.submitLogin()
		}

	case sdl.KeycodePageDown:
		a.scroll += 5 * rowHeight * a.s()
		a.markDirty()

	case sdl.KeycodePageUp:
		a.scroll -= 5 * rowHeight * a.s()
		if a.scroll < 0 {
			a.scroll = 0
		}
		a.markDirty()
	}
}
