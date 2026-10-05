package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
)

// Detail-Panel: ein Klick auf eine Serverzeile öffnet rechts neben der Liste die
// Gerätedaten (Hardware, Adressen, Checks, Notizen) samt benutzerdefinierten
// Feldern. Die Daten kommen aus GET /devices/{id} und /custom-field-values und
// werden je Auswahl frisch geholt – beim nächsten Listen-Abruf ebenso.

const (
	detailW    = 360 // Breite des Panels in Oberflächen-Einheiten
	detailPad  = 14
	detailKeyW = 118 // Spalte für die Feldbezeichnung
)

// deviceDetail ist der geladene Stand eines ausgewählten Geräts.
type deviceDetail struct {
	id      string
	dev     device
	fields  []customFieldValue
	err     string // Fehler beim Laden (leer = ok)
	loading bool
	loaded  time.Time
}

// customField ist die Definition eines benutzerdefinierten Felds (Ausschnitt).
type customField struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"` // text | number | checkbox | select | multiselect | datetime | list
	Link  bool   `json:"link"`
	Model string `json:"model"`
}

type customFieldValue struct {
	Field customField `json:"field"`
	Value string      `json:"value"`
}

func (c *client) device(ctx context.Context, id string) (*device, error) {
	var out device
	err := c.do(ctx, http.MethodGet, "/devices/"+url.PathEscape(id), nil, &out)
	return &out, err
}

func (c *client) customFields(ctx context.Context, id string) ([]customFieldValue, error) {
	var out []customFieldValue
	err := c.do(ctx, http.MethodGet, "/custom-field-values?model=device&entity_id="+url.QueryEscape(id), nil, &out)
	return out, err
}

// selectDevice öffnet das Panel für ein Gerät (bzw. schließt es bei erneutem Klick).
func (a *app) selectDevice(id string) {
	a.mu.Lock()
	if a.selected == id {
		id = ""
	}
	a.selected = id
	a.detailScroll = 0
	if id != "" {
		a.detail = &deviceDetail{id: id, loading: true}
	} else {
		a.detail = nil
	}
	a.mu.Unlock()
	a.markDirty()
	if id != "" {
		go a.loadDetail(id)
	}
}

func (a *app) closeDetail() {
	a.mu.Lock()
	a.selected, a.detail = "", nil
	a.mu.Unlock()
	a.markDirty()
}

// loadDetail holt Gerätedaten und Felder im Hintergrund; ist inzwischen ein anderes
// Gerät gewählt, wird das Ergebnis verworfen.
func (a *app) loadDetail(id string) {
	a.mu.Lock()
	cli := a.cli
	a.mu.Unlock()
	if cli == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	d := &deviceDetail{id: id, loaded: time.Now()}
	dev, err := cli.device(ctx, id)
	if err != nil {
		d.err = "Gerätedaten: " + err.Error()
	} else {
		d.dev = *dev
		if fields, ferr := cli.customFields(ctx, id); ferr == nil {
			d.fields = fields
		} else {
			d.err = "Benutzerdefinierte Felder: " + ferr.Error()
		}
	}
	a.mu.Lock()
	if a.selected == id {
		a.detail = d
	}
	a.mu.Unlock()
	a.markDirty()
}

// refreshDetail lädt das gewählte Gerät nach einem Listen-Abruf neu (ohne Flackern:
// der alte Stand bleibt sichtbar, bis der neue da ist).
func (a *app) refreshDetail() {
	a.mu.Lock()
	id := a.selected
	a.mu.Unlock()
	if id != "" {
		go a.loadDetail(id)
	}
}

// drawDetail zeichnet das Panel in den Bereich (x,y,w,h) und sammelt die Treffer.
func (a *app) drawDetail(x, y, w, h float32) {
	s := a.s()
	lineH := a.txt.LineH()
	pad := detailPad * s

	a.mu.Lock()
	d := a.detail
	var det deviceDetail
	if d != nil {
		det = *d
	}
	scroll := a.detailScroll
	a.mu.Unlock()
	if d == nil {
		return
	}

	sdlui.FillRound(a.rn, x, y, w, h, 8*s, colPanel[0], colPanel[1], colPanel[2], 0xff)
	a.addHit("detail", x, y, w, h)
	a.detailRect = hitRect{x: x, y: y, w: w, h: h}

	// Kopf: Hostname + Schließen.
	headH := 36 * s
	bh := 24 * s
	closeW := a.closeButton("detail:close", x+w-pad-bh, y+(headH-bh)/2, bh)
	title := det.dev.Hostname
	if title == "" {
		if dev, ok := a.deviceByID(det.id); ok {
			title = dev.Hostname
		}
	}
	a.txt.Draw(a.txt.Ellipsize(title, w-2*pad-closeW-10*s), x+pad, y+(headH-lineH)/2, colStrong[0], colStrong[1], colStrong[2])
	sdl.SetRenderDrawColor(a.rn, colLine[0], colLine[1], colLine[2], 0xff)
	sdl.RenderLine(a.rn, x, y+headH, x+w, y+headH)

	// Inhalt: geclippt und scrollbar.
	cy := y + headH
	ch := h - headH
	sdl.SetRenderClipRect(a.rn, &sdl.Rect{X: int32(x), Y: int32(cy), W: int32(w), H: int32(ch)})
	prevClip := a.hitClip
	a.hitClip = &hitRect{y: cy, h: ch}
	defer func() {
		sdl.SetRenderClipRect(a.rn, nil)
		a.hitClip = prevClip
	}()

	if det.loading && det.dev.ID == "" {
		a.txt.Draw("Lade …", x+pad, cy+pad, colMuted[0], colMuted[1], colMuted[2])
		return
	}

	p := &detailPainter{a: a, x: x + pad, y: cy + pad - scroll, w: w - 2*pad, keyW: detailKeyW * s, lineH: lineH, s: s}
	dev := det.dev
	if det.err != "" {
		p.para(det.err, colErrText)
		p.gap(6)
	}

	p.section("Gerät")
	p.kv("Status", statusLabel(dev))
	if dev.ClientName != "" || dev.SiteName != "" {
		p.kv("Kunde / Standort", strings.Trim(dev.ClientName+" · "+dev.SiteName, " ·"))
	}
	p.kv("System", strings.TrimSpace(dev.OS+" "+dev.OSVersion))
	if dev.Vendor != "" || dev.Model != "" {
		p.kv("Hardware", strings.TrimSpace(dev.Vendor+" "+dev.Model))
	}
	p.kv("Seriennummer", dev.Serial)
	if dev.CPUModel != "" {
		cpu := dev.CPUModel
		if dev.CPUCores > 0 {
			cpu += fmt.Sprintf(" (%d Kerne)", dev.CPUCores)
		}
		p.kv("CPU", cpu)
	}
	if dev.MemoryBytes > 0 {
		p.kv("RAM", fmtGB(dev.MemoryBytes))
	}
	p.kv("Adressen", strings.Join(deviceAddresses(dev), ", "))
	p.kv("Öffentliche IP", dev.PublicIP)
	p.kv("Angemeldet", strings.Join(dev.LoggedInUsers, ", "))
	if dev.LastSeen != nil {
		p.kv("Zuletzt gesehen", humanAge(*dev.LastSeen)+" ("+dev.LastSeen.Local().Format("02.01. 15:04")+")")
	}
	if dev.UpdatesCount != nil {
		p.kv("Updates", fmt.Sprintf("%d ausstehend", *dev.UpdatesCount))
	}
	p.kv("Agent", dev.AgentVersion)
	if len(dev.Groups) > 0 {
		names := make([]string, 0, len(dev.Groups))
		for _, g := range dev.Groups {
			names = append(names, g.Name)
		}
		p.kv("Tags", strings.Join(names, ", "))
	}

	// Benutzerdefinierte Felder – der Grund für das Panel; leer heißt „keine".
	p.gap(8)
	p.section("Benutzerdefinierte Felder")
	if len(det.fields) == 0 {
		p.para("keine", colMuted)
	}
	for _, cv := range det.fields {
		p.kv(cv.Field.Name, fmtCustomValue(cv))
	}

	if len(dev.CheckResults) > 0 {
		p.gap(8)
		p.section("Checks")
		res := append([]checkResult(nil), dev.CheckResults...)
		sort.SliceStable(res, func(i, j int) bool { return checkRank(res[i].Status) < checkRank(res[j].Status) })
		for _, r := range res {
			col := colMuted
			switch r.Status {
			case "failing":
				col = colErrText
			case "warning":
				col = colWarnText
			case "passing":
				col = colOkText
			}
			p.line(r.Name, checkStatusLabel(r.Status), col)
		}
	}

	if len(dev.Disks) > 0 {
		p.gap(8)
		p.section("Datenträger")
		for _, dk := range dev.Disks {
			p.kv(dk.Name, fmt.Sprintf("%.0f %% belegt · %s frei von %s", dk.UsedPercent, fmtGB(dk.FreeBytes), fmtGB(dk.SizeBytes)))
		}
	}

	if strings.TrimSpace(dev.Notes) != "" {
		p.gap(8)
		p.section("Notizen")
		for _, ln := range strings.Split(strings.ReplaceAll(dev.Notes, "\r", ""), "\n") {
			p.para(ln, colText)
		}
	}

	// Scrollbereich merken (für Rad/Bild-Tasten), Scroll auf Inhalt begrenzen.
	content := p.y + scroll - (cy + pad) + pad
	a.detailContentH = content
	if max := content - ch; scroll > max && max >= 0 {
		a.mu.Lock()
		a.detailScroll = max
		a.mu.Unlock()
		a.markDirty()
	}
}

// closeButton zeichnet einen quadratischen Knopf mit selbst gezeichnetem Kreuz –
// die eingebettete Schrift hat kein ✕-Zeichen. Liefert die Breite.
func (a *app) closeButton(id string, x, y, size float32) float32 {
	s := a.s()
	c := colBtn
	if a.hover == id {
		c = colBtnHover
	}
	sdlui.FillRound(a.rn, x, y, size, size, 6*s, c[0], c[1], c[2], 0xff)
	sdl.SetRenderDrawColor(a.rn, colText[0], colText[1], colText[2], 0xff)
	r := size * 0.22
	cx, cy := x+size/2, y+size/2
	for i := float32(-0.5); i <= 0.5; i += 0.5 { // 2 px stark
		sdl.RenderLine(a.rn, cx-r+i, cy-r, cx+r+i, cy+r)
		sdl.RenderLine(a.rn, cx-r+i, cy+r, cx+r+i, cy-r)
	}
	a.addHit(id, x, y, size, size)
	return size
}

// scrollDetail verschiebt den Panel-Inhalt (Rad über dem Panel).
func (a *app) scrollDetail(dy float32) {
	a.mu.Lock()
	a.detailScroll += dy
	if a.detailScroll < 0 {
		a.detailScroll = 0
	}
	if max := a.detailContentH - a.detailRect.h + 36*a.s(); max > 0 && a.detailScroll > max {
		a.detailScroll = max
	} else if max <= 0 {
		a.detailScroll = 0
	}
	a.mu.Unlock()
	a.markDirty()
}

// detailPainter setzt Absätze, Abschnitte und Schlüssel/Wert-Zeilen untereinander.
type detailPainter struct {
	a           *app
	x, y, w     float32
	keyW, lineH float32
	s           float32
}

func (p *detailPainter) gap(units float32) { p.y += units * p.s }

func (p *detailPainter) section(title string) {
	a := p.a
	a.txt.Draw(strings.ToUpper(title), p.x, p.y, colAccentText[0], colAccentText[1], colAccentText[2])
	p.y += p.lineH + 2*p.s
	sdl.SetRenderDrawColor(a.rn, colLine[0], colLine[1], colLine[2], 0xff)
	sdl.RenderLine(a.rn, p.x, p.y, p.x+p.w, p.y)
	p.y += 6 * p.s
}

// kv schreibt Bezeichner links und den (umgebrochenen) Wert rechts; leere Werte
// entfallen, damit das Panel nicht mit „—" vollläuft.
func (p *detailPainter) kv(key, val string) {
	if strings.TrimSpace(val) == "" {
		return
	}
	a := p.a
	a.txt.Draw(a.txt.Ellipsize(key, p.keyW-8*p.s), p.x, p.y, colMuted[0], colMuted[1], colMuted[2])
	lines := a.wrap(val, p.w-p.keyW)
	for _, ln := range lines {
		a.txt.Draw(ln, p.x+p.keyW, p.y, colText[0], colText[1], colText[2])
		p.y += p.lineH + 2*p.s
	}
	p.y += 2 * p.s
}

// line schreibt einen Namen links und einen kurzen Status rechtsbündig.
func (p *detailPainter) line(name, status string, col [3]uint8) {
	a := p.a
	sw := a.txt.Width(status)
	a.txt.Draw(a.txt.Ellipsize(name, p.w-sw-10*p.s), p.x, p.y, colText[0], colText[1], colText[2])
	a.txt.Draw(status, p.x+p.w-sw, p.y, col[0], col[1], col[2])
	p.y += p.lineH + 4*p.s
}

func (p *detailPainter) para(text string, col [3]uint8) {
	for _, ln := range p.a.wrap(text, p.w) {
		p.a.txt.Draw(ln, p.x, p.y, col[0], col[1], col[2])
		p.y += p.lineH + 2*p.s
	}
}

// wrap bricht Text an Wortgrenzen auf maxW um; überlange Wörter werden hart getrennt.
func (a *app) wrap(text string, maxW float32) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		cur := ""
		for _, wd := range words {
			try := wd
			if cur != "" {
				try = cur + " " + wd
			}
			if a.txt.Width(try) <= maxW {
				cur = try
				continue
			}
			if cur != "" {
				out = append(out, cur)
			}
			// Wort allein zu lang: zeichenweise brechen.
			cur = ""
			for _, r := range wd {
				if a.txt.Width(cur+string(r)) > maxW && cur != "" {
					out = append(out, cur)
					cur = ""
				}
				cur += string(r)
			}
		}
		out = append(out, cur)
	}
	return out
}

// fmtCustomValue macht aus dem gespeicherten Wert eine lesbare Zeile: Listen und
// Mehrfachauswahl liegen als JSON-Array vor, Checkboxen als true/false.
func fmtCustomValue(cv customFieldValue) string {
	v := strings.TrimSpace(cv.Value)
	switch cv.Field.Type {
	case "list", "multiselect":
		var arr []any
		if json.Unmarshal([]byte(v), &arr) == nil {
			parts := make([]string, 0, len(arr))
			for _, e := range arr {
				parts = append(parts, fmt.Sprint(e))
			}
			return strings.Join(parts, "\n")
		}
	case "checkbox":
		switch v {
		case "true", "1":
			return "ja"
		case "", "false", "0":
			return "nein"
		}
	case "datetime":
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.Local().Format("02.01.2006 15:04")
		}
	}
	return v
}

func fmtGB(b uint64) string {
	gb := float64(b) / (1 << 30)
	if gb >= 1 {
		return fmt.Sprintf("%.1f GB", gb)
	}
	return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
}

// deviceAddresses listet alle IPv4-Adressen der Schnittstellen (ohne Loopback).
func deviceAddresses(d device) []string {
	var out []string
	for _, i := range d.Interfaces {
		for _, ip := range strings.Split(i.IPv4, ",") {
			ip = strings.TrimSpace(ip)
			if ip != "" && !strings.HasPrefix(ip, "127.") {
				out = append(out, ip)
			}
		}
	}
	return out
}

func statusLabel(d device) string {
	switch {
	case d.Revoked:
		return "widerrufen"
	case !d.Managed:
		return "ohne Agent"
	case d.Status == "online":
		return "online"
	case d.Status == "offline":
		return "offline"
	}
	return d.Status
}

func checkStatusLabel(st string) string {
	switch st {
	case "passing":
		return "ok"
	case "failing":
		return "fehlerhaft"
	case "warning":
		return "Warnung"
	}
	return st
}

func checkRank(st string) int {
	switch st {
	case "failing":
		return 0
	case "warning":
		return 1
	case "passing":
		return 3
	}
	return 2
}
