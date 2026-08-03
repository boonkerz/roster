package main

import "github.com/jupiterrider/purego-sdl3/sdl"

// Farben (RGBA).
var (
	fmBg      = [4]uint8{0x0d, 0x11, 0x17, 0xf7}
	fmPanel   = [4]uint8{0x14, 0x1a, 0x22, 0xff}
	fmHeader  = [4]uint8{0x1e, 0x2a, 0x3a, 0xff}
	fmHeaderF = [4]uint8{0x2b, 0x4c, 0x74, 0xff} // fokussiertes Panel
	fmSelF    = [4]uint8{0x2f, 0x5a, 0x8f, 0xff} // Auswahl im fokussierten Panel
	fmSel     = [4]uint8{0x2a, 0x33, 0x40, 0xff} // Auswahl im inaktiven Panel
	fmDir     = [3]uint8{0x8f, 0xc9, 0xff}
	fmFile    = [3]uint8{0xd7, 0xde, 0xe6}
	fmMuted   = [3]uint8{0x8a, 0x96, 0xa4}
	fmWhite   = [3]uint8{0xff, 0xff, 0xff}
)

func (fm *fileManager) fill(x, y, w, h float32, c [4]uint8) {
	sdl.SetRenderDrawColor(fm.rn, c[0], c[1], c[2], c[3])
	sdl.RenderFillRect(fm.rn, &sdl.FRect{X: x, Y: y, W: w, H: h})
}

// draw rendert das Overlay. winW/winH sind die aktuellen Fenstermaße.
func (fm *fileManager) draw(winW, winH float32) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	s := fm.scale
	if s < 1 {
		s = 1
	}
	lh := fm.txt.lineH()
	rowH := lh + 6*s
	fm.rowH = rowH

	top := barHeight + 6*s
	pad := 8 * s
	gap := 8 * s
	footerH := lh + 14*s
	btnBarH := lh + 12*s
	colW := (winW - 2*pad - gap) / 2
	fm.colW = colW
	headerH := lh + 10*s
	listY := top + headerH
	fm.listY = listY
	listH := winH - listY - footerH - btnBarH - pad
	if listH < rowH {
		listH = rowH
	}
	visRows := int(listH / rowH)

	// Hintergrund über das ganze Fenster (unter der Bedienleiste).
	fm.fill(0, barHeight, winW, winH-barHeight, fmBg)

	panels := [2]*fmPane{&fm.local, &fm.remote}
	for i, p := range panels {
		x := pad + float32(i)*(colW+gap)
		fm.colX[i] = x
		focused := fm.focus == i
		// Panel-Fläche + Kopf.
		fm.fill(x, top, colW, headerH+listH, fmPanel)
		hc := fmHeader
		if focused {
			hc = fmHeaderF
		}
		fm.fill(x, top, colW, headerH, hc)
		title := paneTitle(p)
		fm.txt.draw(clip(fm.txt, title, colW-16*s), x+8*s, top+5*s, fmWhite[0], fmWhite[1], fmWhite[2])

		// Auswahl in den sichtbaren Bereich scrollen.
		if p.sel < p.top {
			p.top = p.sel
		}
		if p.sel >= p.top+visRows {
			p.top = p.sel - visRows + 1
		}
		if p.top < 0 {
			p.top = 0
		}

		if p.loading {
			fm.txt.draw("… lädt", x+8*s, listY+4*s, fmMuted[0], fmMuted[1], fmMuted[2])
			continue
		}
		if p.err != "" {
			fm.txt.draw(clip(fm.txt, "Fehler: "+p.err, colW-16*s), x+8*s, listY+4*s, 0xff, 0x8f, 0x8f)
			continue
		}
		for r := 0; r < visRows; r++ {
			idx := p.top + r
			if idx >= len(p.entries) {
				break
			}
			e := p.entries[idx]
			ry := listY + float32(r)*rowH
			if idx == p.sel {
				sc := fmSel
				if focused {
					sc = fmSelF
				}
				fm.fill(x+2*s, ry, colW-4*s, rowH, sc)
			}
			col := fmFile
			name := e.name
			if e.dir {
				col = fmDir
				if name != ".." {
					name = name + "/"
				}
			}
			// Größe rechtsbündig (nur Dateien).
			sizeStr := ""
			if !e.dir {
				sizeStr = humanSize(e.size)
			}
			sw := fm.txt.width(sizeStr)
			nameMax := colW - 20*s - sw
			fm.txt.draw(clip(fm.txt, name, nameMax), x+8*s, ry+3*s, col[0], col[1], col[2])
			if sizeStr != "" {
				fm.txt.draw(sizeStr, x+colW-8*s-sw, ry+3*s, fmMuted[0], fmMuted[1], fmMuted[2])
			}
		}
	}

	// Aktionsleiste mit anklickbaren Buttons (unter den Listen).
	fm.drawButtons(winW, winH-footerH-btnBarH, btnBarH, s, lh)

	// Fußzeile: Tastenhinweise bzw. Status/Prompt.
	fy := winH - footerH
	fm.fill(0, fy, winW, footerH, fmHeader)
	msg := "Tab=Seite  Enter=öffnen  F5=kopieren  F7=Ordner  F8=löschen  Esc=schließen"
	col := fmMuted
	if fm.prompt.kind != "" {
		if fm.prompt.kind == "mkdir" {
			msg = fm.prompt.label + " " + fm.prompt.text + "▏"
		} else {
			msg = fm.prompt.label
		}
		col = fmWhite
	} else if fm.status != "" {
		msg = fm.status
		col = fmWhite
	}
	fm.txt.draw(clip(fm.txt, msg, winW-16*s), 8*s, fy+7*s, col[0], col[1], col[2])
}

// drawButtons rendert die Aktionsleiste und merkt sich die Trefferzonen (fm.btns)
// für die Klickbehandlung. Wird mit gehaltenem fm.mu aufgerufen.
func (fm *fileManager) drawButtons(winW, y, h, s, lh float32) {
	fm.fill(0, y, winW, h, fmHeader)
	defs := []fmButton{
		{id: "copy", label: "Kopieren F5"},
		{id: "mkdir", label: "Ordner F7"},
		{id: "delete", label: "Löschen F8"},
		{id: "refresh", label: "Aktualisieren"},
		{id: "close", label: "Schließen Esc"},
	}
	padX := 12 * s
	bx := 8 * s
	for i := range defs {
		w := fm.txt.width(defs[i].label) + 2*padX
		defs[i].x = bx
		defs[i].w = w
		accent := defs[i].id == "copy"
		cr, cg, cb := uint8(0x2b), uint8(0x38), uint8(0x49)
		if accent {
			cr, cg, cb = 0x2f, 0x5a, 0x8f // Kopieren hervorheben
		}
		fillRound(fm.rn, bx, y+3*s, w, h-6*s, 6*s, cr, cg, cb, 0xff)
		tw := fm.txt.width(defs[i].label)
		fm.txt.draw(defs[i].label, bx+(w-tw)/2, y+(h-lh)/2, fmWhite[0], fmWhite[1], fmWhite[2])
		bx += w + 6*s
	}
	fm.btns = defs
	fm.btnY, fm.btnH = y, h
}

func paneTitle(p *fmPane) string {
	loc := "🖳 Lokal"
	if p.remote {
		loc = "🖥 Gerät"
	}
	path := p.path
	if path == "" {
		path = "(Laufwerke)"
	}
	return loc + "  " + path
}

// clip kürzt s von links mit „…", bis es in maxW passt.
func clip(t *textRenderer, s string, maxW float32) string {
	if t.width(s) <= maxW || maxW <= 0 {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[1:]
		if t.width("…"+string(r)) <= maxW {
			return "…" + string(r)
		}
	}
	return string(r)
}
