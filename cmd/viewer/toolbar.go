package main

import (
	"github.com/boonkerz/roster/internal/sdlui"
)

// Schwebende, abgerundete Bedienleiste (AnyDesk-Stil) im oberen Fensterstreifen.
// Das Remote-Bild wird darunter gerendert (barHeight reserviert). Scharfer Text via
// sdlui.Text (Go-Font), Hover-Highlights, Akzentfarben.

// Basiswerte der Leisten-Maße bei UI-Skalierung 1.0.
const (
	baseBarHeight float32 = 44 // reservierter Streifen oben
	basePillY     float32 = 6
	basePillH     float32 = 32
	baseBtnPadX   float32 = 14
	baseFontPx            = 15 // Font-Basisgröße in px bei Skalierung 1.0
)

// Aktuelle (skalierte) Leisten-Maße.
var (
	barHeight = baseBarHeight
	pillY     = basePillY
	pillH     = basePillH
	btnPadX   = baseBtnPadX
)

// applyUIScale setzt die Leisten-Maße für den HiDPI-Faktor s (idempotent, immer aus
// den Basiswerten – so kann der Faktor zur Laufzeit neu gesetzt werden). Der Text
// skaliert separat über die Font-Größe.
func applyUIScale(s float32) {
	barHeight = baseBarHeight * s
	pillY = basePillY * s
	pillH = basePillH * s
	btnPadX = baseBtnPadX * s
}

type button struct {
	id, label string
	x, w      float32
	accent    int // 0=normal, 1=Trennen (rot)
}

type toolbar struct {
	txt        *sdlui.Text
	buttons    []button
	barX, barW float32
}

func newToolbar(txt *sdlui.Text) *toolbar {
	return &toolbar{txt: txt, buttons: []button{
		{id: "sas", label: "Strg+Alt+Entf"},
		{id: "win", label: "Win"},
		{id: "alttab", label: "Alt+Tab"},
		{id: "esc", label: "Esc"},
		{id: "lock", label: "Sperren"},
		{id: "msg", label: "Meldung"},
		{id: "files", label: "Dateien"},
		{id: "qual", label: "Qualität: M"},
		{id: "res", label: "Auflösung: Nativ"},
		{id: "zoomout", label: "A−"},
		{id: "zoomin", label: "A+"},
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
		w := t.txt.Width(t.buttons[i].label) + 2*btnPadX
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
	rn := t.txt.Renderer()
	sdlui.FillRound(rn, t.barX, pillY, t.barW, pillH, 9, 0x20, 0x27, 0x31, 0xf2) // Pille
	for _, b := range t.buttons {
		bx := t.barX + b.x
		hover := b.id == hoverID
		active := b.id == "lock" && lockActive
		switch {
		case active:
			sdlui.FillRound(rn, bx+3, pillY+3, b.w-6, pillH-6, 6, 0x2e, 0x7d, 0x32, 0xff)
		case hover && b.accent == 1:
			sdlui.FillRound(rn, bx+3, pillY+3, b.w-6, pillH-6, 6, 0x8a, 0x2f, 0x2f, 0xff)
		case hover:
			sdlui.FillRound(rn, bx+3, pillY+3, b.w-6, pillH-6, 6, 0x33, 0x3f, 0x4f, 0xff)
		}
		tr, tg, tb := uint8(0xd7), uint8(0xde), uint8(0xe6)
		if b.accent == 1 {
			tr, tg, tb = 0xff, 0x8f, 0x8f
		}
		if hover || active {
			tr, tg, tb = 0xff, 0xff, 0xff
		}
		tw := t.txt.Width(b.label)
		tx := bx + (b.w-tw)/2
		ty := pillY + (pillH-t.txt.LineH())/2
		t.txt.Draw(b.label, tx, ty, tr, tg, tb)
	}
}
