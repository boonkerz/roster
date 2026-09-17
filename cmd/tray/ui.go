package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
)

// Farbpalette – dieselbe Sprache wie Web-UI und Viewer (dunkel, ruhig, ein Akzent).
var (
	colBg     = [3]uint8{0x0b, 0x0e, 0x14}
	colPanel  = [3]uint8{0x14, 0x1a, 0x22}
	colRow    = [3]uint8{0x11, 0x17, 0x20}
	colHover  = [3]uint8{0x1b, 0x24, 0x30}
	colLine   = [3]uint8{0x23, 0x2b, 0x38}
	colText   = [3]uint8{0xd7, 0xdc, 0xe5}
	colMuted  = [3]uint8{0x84, 0x8e, 0x9f}
	colGreen  = [3]uint8{0x2e, 0x7d, 0x32}
	colRed    = [3]uint8{0x9c, 0x2b, 0x2b}
	colAmber  = [3]uint8{0x9a, 0x6b, 0x1f}
	colAccent = [3]uint8{0x2f, 0x5a, 0x8f}
	colWhite  = [3]uint8{0xff, 0xff, 0xff}
)

const (
	baseFontPx = 14
	rowHeight  = 46
	guestRowH  = 40 // eingerückte Proxmox-Gast-Zeilen
	headerH    = 38
	searchH    = 30
	footerH    = 24
	padX       = 10
)

// render zeichnet das Fenster neu und stellt es dar.
func (a *app) render() {
	a.draw()
	sdl.RenderPresent(a.rn)
}

// draw zeichnet das komplette Fenster in den Backbuffer und sammelt dabei die
// klickbaren Bereiche ein (Immediate Mode: Zeichnen und Treffer bleiben so immer
// synchron). Ohne Present – damit der Screenshot-Modus den Frame zurücklesen kann.
func (a *app) draw() {
	// In Pixeln zeichnen (HiDPI-Backbuffer); Mauskoordinaten werden dafür in
	// events.go mit a.pxRatio umgerechnet.
	var wpx, hpx int32
	sdl.GetWindowSizeInPixels(a.win, &wpx, &hpx)
	w, h := float32(wpx), float32(hpx)

	a.hits = a.hits[:0]
	sdl.SetRenderDrawColor(a.rn, colBg[0], colBg[1], colBg[2], 0xff)
	sdl.RenderClear(a.rn)

	a.mu.Lock()
	view := a.view
	a.mu.Unlock()
	if view == viewLogin {
		a.drawLogin(w, h)
	} else {
		a.drawList(w, h)
	}
	a.drawFooter(w, h)
}

func (a *app) s() float32 { return a.scale }

// addHit merkt einen klickbaren Bereich für den nächsten Klick/Hover-Test. Ist ein
// Clip-Bereich aktiv (Liste), wird der Treffer darauf beschnitten – geklickt werden
// kann nur, was auch zu sehen ist.
func (a *app) addHit(id string, x, y, w, h float32) {
	if c := a.hitClip; c != nil {
		top, bottom := maxF(y, c.y), minF(y+h, c.y+c.h)
		if bottom <= top {
			return
		}
		y, h = top, bottom-top
	}
	a.hits = append(a.hits, hitRect{id: id, x: x, y: y, w: w, h: h})
}

// button zeichnet einen Knopf und liefert seine Breite. accent: 0 = neutral,
// 1 = Akzent (blau), 2 = warnend (rot, z. B. scharf geschaltet zur Bestätigung).
func (a *app) button(id, label string, x, y, h float32, accent int) float32 {
	s := a.s()
	w := a.txt.Width(label) + 2*padX*s
	hovered := a.hover == id
	var c [3]uint8
	switch {
	case accent == 1 && hovered:
		c = [3]uint8{0x3b, 0x6f, 0xad}
	case accent == 1:
		c = colAccent
	case accent == 2 && hovered:
		c = [3]uint8{0xb8, 0x35, 0x35}
	case accent == 2:
		c = colRed
	case hovered:
		c = [3]uint8{0x2b, 0x36, 0x45}
	default:
		c = [3]uint8{0x1e, 0x26, 0x31}
	}
	sdlui.FillRound(a.rn, x, y, w, h, 6*s, c[0], c[1], c[2], 0xff)
	tc := colText
	if hovered || accent >= 1 {
		tc = colWhite
	}
	a.txt.Draw(label, x+padX*s, y+(h-a.txt.LineH())/2, tc[0], tc[1], tc[2])
	a.addHit(id, x, y, w, h)
	return w
}

// badge zeichnet eine kleine Statusplakette (z. B. „Checks 4/5") und liefert die Breite.
func (a *app) badge(label string, x, y, h float32, c [3]uint8) float32 {
	s := a.s()
	w := a.txt.Width(label) + 2*7*s
	sdlui.FillRound(a.rn, x, y, w, h, 5*s, c[0], c[1], c[2], 0x66)
	a.txt.Draw(label, x+7*s, y+(h-a.txt.LineH())/2, colText[0], colText[1], colText[2])
	return w
}

func (a *app) drawList(w, h float32) {
	s := a.s()
	lineH := a.txt.LineH()

	// --- Kopfzeile: Titel, Zusammenfassung, Aktionen ---
	sdlui.FillRound(a.rn, 0, 0, w, headerH*s, 0, colPanel[0], colPanel[1], colPanel[2], 0xff)
	a.txt.Draw("Roster", 12*s, (headerH*s-lineH)/2, colWhite[0], colWhite[1], colWhite[2])

	bx := w - 12*s
	bh := 24 * s
	by := (headerH*s - bh) / 2
	bw := a.button("logout", "Abmelden", bx-a.txt.Width("Abmelden")-2*padX*s, by, bh, 0)
	bx -= bw + 8*s
	label := "Aktualisieren"
	a.mu.Lock()
	if a.loading {
		label = "lädt …"
	}
	total, offline, failing := len(a.devices), 0, 0
	for _, d := range a.devices {
		if d.Status == "offline" {
			offline++
		}
		if d.ChecksFailing > 0 || d.TasksFailing > 0 {
			failing++
		}
	}
	a.mu.Unlock()
	bw = a.button("refresh", label, bx-a.txt.Width(label)-2*padX*s, by, bh, 0)
	bx -= bw + 8*s

	sum := fmt.Sprintf("%d Server · %d offline · %d mit Fehlern", total, offline, failing)
	a.txt.Draw(a.txt.Ellipsize(sum, bx-90*s), 80*s, (headerH*s-lineH)/2, colMuted[0], colMuted[1], colMuted[2])

	// --- Suchfeld ---
	sy := headerH*s + 6*s
	sdlui.FillRound(a.rn, 12*s, sy, w-24*s, searchH*s, 6*s, colRow[0], colRow[1], colRow[2], 0xff)
	a.mu.Lock()
	filter := a.filter
	a.mu.Unlock()
	txt, col := filter, colText
	if txt == "" {
		txt, col = "Suchen (Name, Standort, Kunde) …", colMuted
	}
	a.txt.Draw(a.txt.Ellipsize(txt, w-48*s), 22*s, sy+(searchH*s-lineH)/2, col[0], col[1], col[2])
	a.addHit("search", 12*s, sy, w-24*s, searchH*s)

	// --- Geräteliste (mit aufgeklappten Proxmox-Gästen) ---
	listY := sy + searchH*s + 6*s
	listH := h - listY - footerH*s
	rows := a.filtered()
	items := a.listItems(rows)
	var listTotal float32
	for _, it := range items {
		listTotal += it.h
	}
	maxScroll := listTotal - listH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if a.scroll > maxScroll {
		a.scroll = maxScroll
	}
	if a.scroll < 0 {
		a.scroll = 0
	}
	if len(rows) == 0 {
		msg := "Keine Server gefunden."
		a.mu.Lock()
		if a.cli != nil && a.lastUpdate.IsZero() {
			msg = "Lade Server …"
		}
		a.mu.Unlock()
		a.txt.Draw(msg, 22*s, listY+10*s, colMuted[0], colMuted[1], colMuted[2])
		return
	}

	// Auf den Listenbereich zuschneiden: eine halb sichtbare Zeile darf nicht in
	// Suchfeld oder Fußzeile ragen.
	sdl.SetRenderClipRect(a.rn, &sdl.Rect{X: 0, Y: int32(listY), W: int32(w), H: int32(listH)})
	a.hitClip = &hitRect{y: listY, h: listH}
	defer func() {
		sdl.SetRenderClipRect(a.rn, nil)
		a.hitClip = nil
	}()
	indent := 30 * s
	y := listY - a.scroll
	for _, it := range items {
		if y > listY+listH {
			break
		}
		if y+it.h >= listY {
			if it.guest != nil {
				a.drawGuestRow(it.dev, *it.guest, 12*s+indent, y, w-24*s-indent, it.h-4*s)
			} else {
				a.drawRow(it.dev, 12*s, y, w-24*s, it.h-4*s)
			}
		}
		y += it.h
	}
}

// listItem ist eine Zeile der Liste: ein Gerät oder ein Proxmox-Gast darunter.
type listItem struct {
	dev   device
	guest *pveGuest
	h     float32
}

// listItems legt die sichtbaren Zeilen fest. Gäste erscheinen unter aufgeklappten
// Hosts; trifft die Suche nur Gäste (nicht den Host selbst), werden genau diese
// gezeigt – ohne dass man erst aufklappen muss.
func (a *app) listItems(rows []device) []listItem {
	s := a.s()
	a.mu.Lock()
	f := strings.ToLower(strings.TrimSpace(a.filter))
	a.mu.Unlock()
	items := make([]listItem, 0, len(rows))
	for _, d := range rows {
		items = append(items, listItem{dev: d, h: rowHeight * s})
		if len(d.ProxmoxGuests) == 0 {
			continue
		}
		onlyGuests := f != "" && !deviceMatches(d, f)
		if !onlyGuests && !a.expanded[d.ID] {
			continue
		}
		for i := range d.ProxmoxGuests {
			g := d.ProxmoxGuests[i]
			if onlyGuests && !guestMatches(g, f) {
				continue
			}
			items = append(items, listItem{dev: d, guest: &g, h: guestRowH * s})
		}
	}
	return items
}

// drawRow zeichnet eine Serverzeile. Der Aufrufer hat den Listenbereich als
// SDL-Clip gesetzt, halb sichtbare Zeilen werden also sauber abgeschnitten.
func (a *app) drawRow(d device, x, y, w, h float32) {
	s := a.s()
	lineH := a.txt.LineH()
	rowID := "row:" + d.ID
	bg := colRow
	if a.hover == rowID || strings.HasSuffix(a.hover, ":"+d.ID) {
		bg = colHover
	}
	sdlui.FillRound(a.rn, x, y, w, h, 8*s, bg[0], bg[1], bg[2], 0xff)
	a.addHit(rowID, x, y, w, h)

	// Statuspunkt.
	dot := colGreen
	switch {
	case d.Revoked:
		dot = colMuted
	case d.Status == "offline":
		dot = colRed
	case d.Status != "online":
		dot = colAmber
	}
	sdlui.FillDisc(a.rn, x+16*s, y+h/2, 5*s, dot[0], dot[1], dot[2], 0xff)

	// Rechts: Aktionsknöpfe (von rechts nach links gelegt).
	bh := 24 * s
	by := y + (h-bh)/2
	bx := x + w - 8*s
	if d.Managed && !d.Revoked {
		lbl := "Viewer"
		bx -= a.txt.Width(lbl) + 2*padX*s
		a.button("view:"+d.ID, lbl, bx, by, bh, 1)
		bx -= 6 * s
		lbl = "Terminal"
		bx -= a.txt.Width(lbl) + 2*padX*s
		a.button("term:"+d.ID, lbl, bx, by, bh, 1)
		bx -= 6 * s
	}
	// SFTP geht nicht durch den Tunnel, sondern direkt per SSH – daher nur, wenn
	// eine Adresse bekannt ist (gilt auch für Geräte ohne Agent aus dem Netz-Scan).
	if deviceAddress(d) != "" && !d.Revoked {
		lbl := "SFTP"
		bx -= a.txt.Width(lbl) + 2*padX*s
		a.button("sftp:"+d.ID, lbl, bx, by, bh, 0)
		bx -= 6 * s
	}
	lbl := "Web"
	bx -= a.txt.Width(lbl) + 2*padX*s
	a.button("web:"+d.ID, lbl, bx, by, bh, 0)

	// Mitte: Check- und Task-Plaketten.
	badgeH := 20 * s
	badgeY := y + (h-badgeH)/2
	// Plaketten ohne Aussage („keine Tasks", „keine Checks") weglassen – der Platz
	// gehört dem Hostnamen.
	bx -= 4 * s
	if d.TasksTotal > 0 {
		tb := taskBadge(d)
		bx -= 6 * s
		bx -= a.txt.Width(tb.label) + 14*s
		a.badge(tb.label, bx, badgeY, badgeH, tb.color)
	}
	if d.ChecksTotal > 0 {
		cb := checkBadge(d)
		bx -= 6 * s
		bx -= a.txt.Width(cb.label) + 14*s
		a.badge(cb.label, bx, badgeY, badgeH, cb.color)
	}
	if fb, ok := fanBadge(d.Fans); ok {
		bx -= 6 * s
		bx -= a.txt.Width(fb.label) + 14*s
		a.badge(fb.label, bx, badgeY, badgeH, fb.color)
	}
	if tb, ok := tempBadge(d.Temperatures); ok {
		bx -= 6 * s
		bx -= a.txt.Width(tb.label) + 14*s
		a.badge(tb.label, bx, badgeY, badgeH, tb.color)
	}
	if len(d.ProxmoxGuests) > 0 {
		n, bad := pveSummary(d, time.Now())
		label, col := fmt.Sprintf("%d Gäste", n), colLine
		if bad > 0 {
			label, col = fmt.Sprintf("%d Gäste · %d ohne Backup", n, bad), colRed
		}
		bx -= 6 * s
		bx -= a.pillWidth(label)
		a.togglePill("pvetoggle:"+d.ID, label, bx, badgeY, badgeH, a.expanded[d.ID], col)
	}

	// Links: Name und Zusatzzeile.
	nameW := bx - (x + 30*s) - 10*s
	a.txt.Draw(a.txt.Ellipsize(d.Hostname, nameW), x+30*s, y+h/2-lineH-1*s, colText[0], colText[1], colText[2])
	a.txt.Draw(a.txt.Ellipsize(subline(d), nameW), x+30*s, y+h/2+1*s, colMuted[0], colMuted[1], colMuted[2])
}

// pillWidth ist die Breite der Aufklapp-Plakette (Dreieck + Text).
func (a *app) pillWidth(label string) float32 {
	return a.txt.Width(label) + 28*a.s()
}

// togglePill zeichnet die Plakette zum Auf-/Zuklappen der Gäste eines Hosts.
func (a *app) togglePill(id, label string, x, y, h float32, open bool, c [3]uint8) {
	s := a.s()
	w := a.pillWidth(label)
	alpha := uint8(0x66)
	if a.hover == id {
		alpha = 0xaa
	}
	sdlui.FillRound(a.rn, x, y, w, h, 5*s, c[0], c[1], c[2], alpha)
	drawChevron(a.rn, x+11*s, y+h/2, 4*s, open, colText)
	a.txt.Draw(label, x+21*s, y+(h-a.txt.LineH())/2, colText[0], colText[1], colText[2])
	a.addHit(id, x, y, w, h)
}

// drawChevron zeichnet ein kleines gefülltes Dreieck: nach rechts (zu) oder unten (auf).
// Selbst gezeichnet, weil die eingebettete Schrift keine Pfeil-Glyphen hat.
func drawChevron(rn *sdl.Renderer, cx, cy, r float32, open bool, c [3]uint8) {
	sdl.SetRenderDrawColor(rn, c[0], c[1], c[2], 0xff)
	for i := float32(0); i <= r; i++ {
		if open { // ▼: oben breit, unten spitz
			sdl.RenderLine(rn, cx-r+i, cy-r/2+i, cx+r-i, cy-r/2+i)
		} else { // ▶: links breit, rechts spitz
			sdl.RenderLine(rn, cx-r/2+i, cy-r+i, cx-r/2+i, cy+r-i)
		}
	}
}

// drawGuestRow zeichnet eine eingerückte Zeile für eine VM / einen Container.
func (a *app) drawGuestRow(d device, g pveGuest, x, y, w, h float32) {
	s := a.s()
	lineH := a.txt.LineH()
	key := guestKey(d.ID, g.VMID)
	bg := [3]uint8{0x0e, 0x13, 0x1b}
	if strings.HasSuffix(a.hover, ":"+key) {
		bg = colHover
	}
	// Baumlinie zum Host.
	sdl.SetRenderDrawColor(a.rn, colLine[0], colLine[1], colLine[2], 0xff)
	sdl.RenderFillRect(a.rn, &sdl.FRect{X: x - 16*s, Y: y - 4*s, W: 1 * s, H: h/2 + 4*s})
	sdl.RenderFillRect(a.rn, &sdl.FRect{X: x - 16*s, Y: y + h/2, W: 12 * s, H: 1 * s})

	sdlui.FillRound(a.rn, x, y, w, h, 7*s, bg[0], bg[1], bg[2], 0xff)
	a.addHit("guestrow:"+key, x, y, w, h)

	dot := colMuted
	switch g.Status {
	case "running":
		dot = colGreen
	case "paused", "suspended":
		dot = colAmber
	}
	if !g.Template {
		sdlui.FillDisc(a.rn, x+14*s, y+h/2, 4*s, dot[0], dot[1], dot[2], 0xff)
	}

	// Rechts: Aktionen (von rechts nach links).
	bh := 22 * s
	by := y + (h-bh)/2
	bx := x + w - 8*s
	a.mu.Lock()
	busy := a.guestBusy[key]
	a.mu.Unlock()
	switch {
	case g.Template:
	case busy:
		lbl := "läuft …"
		bx -= a.txt.Width(lbl) + 2*padX*s
		a.txt.Draw(lbl, bx+padX*s, by+(bh-lineH)/2, colMuted[0], colMuted[1], colMuted[2])
	case g.Status == "running":
		bx = a.guestButton(d, g, "stop", "Stoppen", bx, by, bh)
		bx = a.guestButton(d, g, "reboot", "Neustart", bx, by, bh)
		bx = a.guestButton(d, g, "shutdown", "Herunterfahren", bx, by, bh)
	default:
		bx = a.guestButton(d, g, "start", "Starten", bx, by, bh)
	}

	// Mitte: Backup-Plakette (bzw. „Vorlage").
	badgeH := 20 * s
	bb := backupBadge(g, time.Now())
	if g.Template {
		bb = badgeInfo{"Vorlage", colLine}
	}
	bx -= 10 * s
	bx -= a.txt.Width(bb.label) + 14*s
	a.badge(bb.label, bx, y+(h-badgeH)/2, badgeH, bb.color)

	// Links: VMID + Name, darunter Typ/Node/Ressourcen.
	nameX := x + 26*s
	maxW := bx - nameX - 10*s
	title := strconv.Itoa(g.VMID)
	if g.Name != "" {
		title += "  " + g.Name
	}
	sub := guestKind(g) + " · " + g.Node
	if g.CPUs > 0 {
		sub += fmt.Sprintf(" · %.0f vCPU", g.CPUs)
	}
	if g.MaxMem > 0 {
		sub += fmt.Sprintf(" · %.1f GB", float64(g.MaxMem)/(1<<30))
	}
	if g.BackupTaskStatus == "failed" && g.BackupTaskMsg != "" {
		sub += " · " + g.BackupTaskMsg
	}
	tc := colText
	if g.Template {
		tc = colMuted
	}
	a.txt.Draw(a.txt.Ellipsize(title, maxW), nameX, y+h/2-lineH, tc[0], tc[1], tc[2])
	a.txt.Draw(a.txt.Ellipsize(sub, maxW), nameX, y+h/2, colMuted[0], colMuted[1], colMuted[2])
}

// guestButton zeichnet einen Aktionsknopf rechtsbündig vor bx und liefert das neue bx.
// Ein scharf geschalteter Knopf (Stopp/Neustart nach erstem Klick) wird rot.
func (a *app) guestButton(d device, g pveGuest, action, label string, bx, by, bh float32) float32 {
	s := a.s()
	id := "pve:" + action + ":" + guestKey(d.ID, g.VMID)
	accent := 0
	if action == "start" {
		accent = 1
	}
	if a.armed == id && time.Since(a.armedAt) <= confirmWindow {
		label, accent = "Sicher?", 2
	}
	bx -= a.txt.Width(label) + 2*padX*s
	a.button(id, label, bx, by, bh, accent)
	return bx - 6*s
}

type badgeInfo struct {
	label string
	color [3]uint8
}

func checkBadge(d device) badgeInfo {
	switch {
	case d.ChecksTotal == 0:
		return badgeInfo{"Checks –", colLine}
	case d.ChecksFailing > 0:
		return badgeInfo{fmt.Sprintf("Checks %d/%d rot", d.ChecksFailing, d.ChecksTotal), colRed}
	default:
		return badgeInfo{fmt.Sprintf("Checks %d/%d ok", d.ChecksTotal, d.ChecksTotal), colGreen}
	}
}

func taskBadge(d device) badgeInfo {
	switch {
	case d.TasksTotal == 0:
		return badgeInfo{"Tasks –", colLine}
	case d.TasksFailing > 0:
		return badgeInfo{fmt.Sprintf("Tasks %d/%d rot", d.TasksFailing, d.TasksTotal), colRed}
	default:
		return badgeInfo{fmt.Sprintf("Tasks %d/%d ok", d.TasksTotal, d.TasksTotal), colGreen}
	}
}

// subline fasst Kunde/Standort, Betriebssystem und Erreichbarkeit zusammen.
func subline(d device) string {
	parts := make([]string, 0, 4)
	if d.ClientName != "" || d.SiteName != "" {
		parts = append(parts, strings.TrimPrefix(strings.TrimSuffix(d.ClientName+" · "+d.SiteName, " · "), " · "))
	}
	if d.OS != "" {
		parts = append(parts, d.OS)
	}
	switch {
	case d.Revoked:
		parts = append(parts, "widerrufen")
	case !d.Managed:
		parts = append(parts, "ohne Agent")
	case d.Status == "offline" && d.LastSeen != nil:
		parts = append(parts, "zuletzt "+humanAge(*d.LastSeen))
	}
	return strings.Join(parts, " · ")
}

func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "gerade eben"
	case d < time.Hour:
		return fmt.Sprintf("vor %d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("vor %d h", int(d.Hours()))
	default:
		return fmt.Sprintf("vor %d Tagen", int(d.Hours()/24))
	}
}

func (a *app) drawFooter(w, h float32) {
	s := a.s()
	lineH := a.txt.LineH()
	y := h - footerH*s
	sdl.SetRenderDrawColor(a.rn, colLine[0], colLine[1], colLine[2], 0xff)
	sdl.RenderLine(a.rn, 0, y, w, y)

	a.mu.Lock()
	msg, bad, last, who := a.status, a.statusBad, a.lastUpdate, a.username
	a.mu.Unlock()
	col := colMuted
	if bad {
		col = [3]uint8{0xff, 0x8f, 0x8f}
	}
	if msg == "" && !last.IsZero() {
		msg = "Stand " + last.Format("15:04:05")
		if who != "" {
			msg += " · angemeldet als " + who
		}
	}
	a.txt.Draw(a.txt.Ellipsize(msg, w-24*s), 12*s, y+(footerH*s-lineH)/2, col[0], col[1], col[2])
}

// drawLogin zeichnet die Anmeldemaske (URL, Benutzer, Passwort, ggf. TOTP).
func (a *app) drawLogin(w, h float32) {
	s := a.s()
	lineH := a.txt.LineH()
	a.mu.Lock()
	form := a.form
	a.mu.Unlock()

	labels := []string{"Serveradresse", "Benutzer", "Passwort", "Zwei-Faktor-Code"}
	n := 3
	if form.needsCode {
		n = 4
	}
	fieldH := 30 * s
	fieldBlock := lineH + 4*s + fieldH + 12*s
	pw := minF(420*s, w-40*s)
	ph := 50*s + float32(n)*fieldBlock + 32*s + 12*s + lineH + 18*s
	px := (w - pw) / 2
	py := (h - ph) / 2
	if py < 10*s {
		py = 10 * s
	}
	sdlui.FillRound(a.rn, px, py, pw, ph, 10*s, colPanel[0], colPanel[1], colPanel[2], 0xff)

	a.txt.Draw("Bei Roster anmelden", px+18*s, py+16*s, colWhite[0], colWhite[1], colWhite[2])
	y := py + 50*s
	for i := 0; i < n; i++ {
		a.txt.Draw(labels[i], px+18*s, y, colMuted[0], colMuted[1], colMuted[2])
		fy := y + lineH + 4*s
		bg := colRow
		if form.focus == i {
			bg = [3]uint8{0x18, 0x22, 0x2f}
		}
		sdlui.FillRound(a.rn, px+18*s, fy, pw-36*s, fieldH, 6*s, bg[0], bg[1], bg[2], 0xff)
		if form.focus == i {
			sdl.SetRenderDrawColor(a.rn, colAccent[0], colAccent[1], colAccent[2], 0xff)
			sdl.RenderRect(a.rn, &sdl.FRect{X: px + 18*s, Y: fy, W: pw - 36*s, H: fieldH})
		}
		val := form.fields[i]
		if i == 2 {
			val = strings.Repeat("•", len([]rune(val)))
		}
		val = a.txt.Clip(val, pw-52*s)
		tw := a.txt.Draw(val, px+27*s, fy+(fieldH-lineH)/2, colText[0], colText[1], colText[2])
		if form.focus == i { // Schreibmarke als schmaler Balken (nicht jede Schrift hat ein Cursor-Zeichen)
			sdl.SetRenderDrawColor(a.rn, colText[0], colText[1], colText[2], 0xff)
			sdl.RenderFillRect(a.rn, &sdl.FRect{X: px + 28*s + tw, Y: fy + 6*s, W: 2 * s, H: fieldH - 12*s})
		}
		a.addHit(fmt.Sprintf("f%d", i), px+18*s, fy, pw-36*s, fieldH)
		y = fy + fieldH + 12*s
	}

	label := "Anmelden"
	if form.busy {
		label = "Anmeldung läuft …"
	}
	a.button("login", label, px+18*s, y, 32*s, 1)
	msg, col := form.err, [3]uint8{0xff, 0x8f, 0x8f}
	if msg == "" {
		msg, col = "Zugang wird lokal als API-Token gespeichert.", colMuted
	}
	a.txt.Draw(a.txt.Ellipsize(msg, pw-36*s), px+18*s, y+32*s+12*s, col[0], col[1], col[2])
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
