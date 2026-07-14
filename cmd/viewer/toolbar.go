package main

import (
	"math"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// Schwebende, abgerundete Bedienleiste (AnyDesk-Stil) im oberen Fensterstreifen.
// Das Remote-Bild wird darunter gerendert (barHeight reserviert). Scharfer Text via
// textRenderer (Go-Font), Hover-Highlights, Akzentfarben.

const (
	barHeight float32 = 44 // reservierter Streifen oben
	pillY     float32 = 6
	pillH     float32 = 32
	btnPadX   float32 = 14
)

type button struct {
	id, label string
	x, w      float32
	accent    int // 0=normal, 1=Trennen (rot)
}

type toolbar struct {
	txt        *textRenderer
	buttons    []button
	barX, barW float32
}

func newToolbar(txt *textRenderer) *toolbar {
	return &toolbar{txt: txt, buttons: []button{
		{id: "sas", label: "Strg+Alt+Entf"},
		{id: "win", label: "Win"},
		{id: "alttab", label: "Alt+Tab"},
		{id: "esc", label: "Esc"},
		{id: "lock", label: "Sperren"},
		{id: "msg", label: "Meldung"},
		{id: "qual", label: "Qualität: M"},
		{id: "full", label: "Vollbild"},
		{id: "disc", label: "Trennen", accent: 1},
	}}
}

func (t *toolbar) setLabel(id, label string) {
	for i := range t.buttons {
		if t.buttons[i].id == id {
			t.buttons[i].label = label
		}
	}
}

// layout ordnet die Buttons an und zentriert die Pille im Fenster.
func (t *toolbar) layout(winW float32) {
	x := float32(0)
	for i := range t.buttons {
		w := t.txt.width(t.buttons[i].label) + 2*btnPadX
		t.buttons[i].x = x
		t.buttons[i].w = w
		x += w
	}
	t.barW = x
	t.barX = (winW - t.barW) / 2
	if t.barX < 6 {
		t.barX = 6
	}
}

// hit liefert die Button-ID an Fensterposition (mx,my) oder "".
func (t *toolbar) hit(mx, my float32) string {
	if my < pillY || my > pillY+pillH || mx < t.barX || mx > t.barX+t.barW {
		return ""
	}
	lx := mx - t.barX
	for _, b := range t.buttons {
		if lx >= b.x && lx < b.x+b.w {
			return b.id
		}
	}
	return ""
}

func (t *toolbar) draw(hoverID string, lockActive bool) {
	rn := t.txt.renderer
	fillRound(rn, t.barX, pillY, t.barW, pillH, 9, 0x20, 0x27, 0x31, 0xf2) // Pille
	for _, b := range t.buttons {
		bx := t.barX + b.x
		hover := b.id == hoverID
		active := b.id == "lock" && lockActive
		switch {
		case active:
			fillRound(rn, bx+3, pillY+3, b.w-6, pillH-6, 6, 0x2e, 0x7d, 0x32, 0xff)
		case hover && b.accent == 1:
			fillRound(rn, bx+3, pillY+3, b.w-6, pillH-6, 6, 0x8a, 0x2f, 0x2f, 0xff)
		case hover:
			fillRound(rn, bx+3, pillY+3, b.w-6, pillH-6, 6, 0x33, 0x3f, 0x4f, 0xff)
		}
		tr, tg, tb := uint8(0xd7), uint8(0xde), uint8(0xe6)
		if b.accent == 1 {
			tr, tg, tb = 0xff, 0x8f, 0x8f
		}
		if hover || active {
			tr, tg, tb = 0xff, 0xff, 0xff
		}
		tw := t.txt.width(b.label)
		tx := bx + (b.w-tw)/2
		ty := pillY + (pillH-t.txt.lineH())/2
		t.txt.draw(b.label, tx, ty, tr, tg, tb)
	}
}

// fillRound zeichnet ein gefülltes Rechteck mit abgerundeten Ecken (zeilenweise).
func fillRound(rn *sdl.Renderer, x, y, w, h, rad float32, cr, cg, cb, ca uint8) {
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
