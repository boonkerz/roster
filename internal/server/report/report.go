// Package report erzeugt Health-/Inventar-Berichte (HTML zum Ansehen/Drucken,
// Klartext für den E-Mail-Versand) aus der Geräteliste.
package report

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// Row ist die Zusammenfassung je Kunde (Client).
type Row struct {
	Client         string
	Total          int
	Online         int
	Offline        int
	FailingChecks  int
	FailingTasks   int
	PendingPatches int
	// PatchesInstalled zählt installierte/aktualisierte Patches der letzten 7 Tage.
	PatchesInstalled int
}

// Report bündelt die Zeilen samt Gesamtsumme.
type Report struct {
	Title     string
	Generated time.Time
	Rows      []Row
	Totals    Row
}

// Build aggregiert die (mit Status versehenen) Geräte je Kunde. devices müssen ihr
// Status-Feld gesetzt haben (online/offline/unknown). installed zählt je Geräte-ID die
// in den letzten 7 Tagen installierten Patches (nil = unbekannt).
func Build(title string, devices []model.Device, installed map[string]int) Report {
	byClient := map[string]*Row{}
	get := func(name string) *Row {
		if name == "" {
			name = "(ohne Kunde)"
		}
		r, ok := byClient[name]
		if !ok {
			r = &Row{Client: name}
			byClient[name] = r
		}
		return r
	}
	var t Row
	t.Client = "Gesamt"
	for i := range devices {
		d := &devices[i]
		if d.Revoked {
			continue
		}
		r := get(d.ClientName)
		add := func(r *Row) {
			r.Total++
			switch d.Status {
			case "online":
				r.Online++
			default:
				r.Offline++
			}
			if d.ChecksFailing > 0 {
				r.FailingChecks += d.ChecksFailing
			}
			if d.TasksFailing > 0 {
				r.FailingTasks += d.TasksFailing
			}
			if d.UpdatesCount != nil && *d.UpdatesCount > 0 {
				r.PendingPatches += *d.UpdatesCount
			}
			r.PatchesInstalled += installed[d.ID]
		}
		add(r)
		add(&t)
	}
	rows := make([]Row, 0, len(byClient))
	for _, r := range byClient {
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Client < rows[j].Client })
	return Report{Title: title, Generated: time.Now(), Rows: rows, Totals: t}
}

// Text liefert eine Klartext-Fassung (für E-Mail).
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\nErstellt: %s\n\n", r.Title, r.Generated.Format("02.01.2006 15:04"))
	fmt.Fprintf(&b, "%-24s %6s %7s %8s %6s %6s %8s %9s\n", "Kunde", "Geräte", "Online", "Offline", "Checks", "Tasks", "Patches", "Inst. 7T")
	line := func(x Row) {
		fmt.Fprintf(&b, "%-24s %6d %7d %8d %6d %6d %8d %9d\n",
			trunc(x.Client, 24), x.Total, x.Online, x.Offline, x.FailingChecks, x.FailingTasks, x.PendingPatches, x.PatchesInstalled)
	}
	for _, row := range r.Rows {
		line(row)
	}
	b.WriteString(strings.Repeat("-", 80) + "\n")
	line(r.Totals)
	return b.String()
}

// HTML liefert eine eigenständige HTML-Seite (druckbar zu PDF).
func (r Report) HTML() string {
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"de\"><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(r.Title))
	b.WriteString("</title><style>")
	b.WriteString(`body{font-family:system-ui,sans-serif;margin:32px;color:#1a1a1a}
h1{margin:0 0 4px}p.meta{color:#666;margin:0 0 20px}
table{border-collapse:collapse;width:100%}th,td{padding:8px 10px;text-align:right;border-bottom:1px solid #ddd}
th:first-child,td:first-child{text-align:left}thead th{border-bottom:2px solid #333}
tfoot td{font-weight:700;border-top:2px solid #333}
.bad{color:#c00;font-weight:600}.warn{color:#b60}
@media print{body{margin:0}}`)
	b.WriteString("</style></head><body>")
	fmt.Fprintf(&b, "<h1>%s</h1><p class=\"meta\">Erstellt: %s</p>",
		html.EscapeString(r.Title), r.Generated.Format("02.01.2006 15:04"))
	b.WriteString("<table><thead><tr><th>Kunde</th><th>Geräte</th><th>Online</th><th>Offline</th><th>Checks fehlerhaft</th><th>Tasks fehlerhaft</th><th>Ausstehende Patches</th><th>Installiert (7 Tage)</th></tr></thead><tbody>")
	cell := func(n int, cls string) {
		if n > 0 && cls != "" {
			fmt.Fprintf(&b, "<td class=\"%s\">%d</td>", cls, n)
		} else {
			fmt.Fprintf(&b, "<td>%d</td>", n)
		}
	}
	rowHTML := func(x Row) {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%d</td>", html.EscapeString(x.Client), x.Total)
		cell(x.Online, "")
		cell(x.Offline, "bad")
		cell(x.FailingChecks, "bad")
		cell(x.FailingTasks, "warn")
		cell(x.PendingPatches, "warn")
		cell(x.PatchesInstalled, "")
		b.WriteString("</tr>")
	}
	for _, row := range r.Rows {
		rowHTML(row)
	}
	b.WriteString("</tbody><tfoot><tr><td>Gesamt</td>")
	fmt.Fprintf(&b, "<td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td></tr></tfoot></table>",
		r.Totals.Total, r.Totals.Online, r.Totals.Offline, r.Totals.FailingChecks, r.Totals.FailingTasks, r.Totals.PendingPatches, r.Totals.PatchesInstalled)
	b.WriteString("</body></html>")
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
