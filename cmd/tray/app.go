package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
)

type viewMode int

const (
	viewLogin viewMode = iota
	viewList
)

// app hält den kompletten Zustand der Taskleisten-App. Alles unter mu wird auch von
// Hintergrund-Goroutinen (Poll, Login, Fernsteuerung) geschrieben; der Rest gehört
// dem SDL-Hauptthread.
type app struct {
	cfg *config
	cli *client

	win *sdl.Window
	rn  *sdl.Renderer
	txt *sdlui.Text
	tr  *trayIcon

	scale   float32 // Oberflächen-Skalierung (Schrift, Maße)
	pxRatio float32 // Pixel je logischer Einheit – Mauskoordinaten kommen logisch

	mu         sync.Mutex
	devices    []device
	username   string
	loading    bool
	lastUpdate time.Time
	status     string // Meldung in der Fußzeile
	statusBad  bool
	guestBusy  map[string]bool // Proxmox-Gast (guestKey) mit laufender Aktion

	// UI-Zustand (nur Hauptthread).
	view     viewMode
	filter   string
	scroll   float32
	hover    string // ID des überfahrenen Hit-Bereichs
	hits     []hitRect
	hitClip  *hitRect        // aktiver Clip-Bereich: Treffer werden darauf beschnitten
	expanded map[string]bool // Proxmox-Host (Geräte-ID) aufgeklappt?
	armed    string          // scharf geschalteter Stopp-/Neustart-Knopf (Hit-ID)
	armedAt  time.Time
	form     loginForm
	visible  bool
	quitting bool

	dirty      atomic.Bool
	refreshNow chan struct{}
	done       chan struct{}

	// Wünsche aus dem Tray-Menü; die Event-Schleife arbeitet sie ab, damit in den
	// SDL-Callbacks nichts Langes läuft.
	wantShow   atomic.Bool
	wantQuit   atomic.Bool
	wantLogout atomic.Bool
	// trayDirty: Symbol/Tooltip neu setzen. SDL-Tray-Aufrufe gehören auf den
	// Hauptthread, deshalb melden Hintergrund-Goroutinen nur den Änderungsbedarf.
	trayDirty atomic.Bool
}

// loginForm ist die Anmeldemaske (Feldindex = Fokus).
type loginForm struct {
	fields    [4]string // 0=URL, 1=Benutzer, 2=Passwort, 3=TOTP-Code
	focus     int
	needsCode bool
	busy      bool
	err       string
}

type hitRect struct {
	id   string
	x, y float32
	w, h float32
}

func newApp(cfg *config) *app {
	a := &app{cfg: cfg, refreshNow: make(chan struct{}, 1), done: make(chan struct{}),
		expanded: map[string]bool{}, guestBusy: map[string]bool{}}
	for _, id := range cfg.ExpandedHosts {
		a.expanded[id] = true
	}
	a.form.fields[0] = cfg.URL
	a.form.fields[1] = cfg.User
	if cfg.URL != "" && cfg.Token != "" {
		a.cli = newClient(cfg.URL, cfg.Token, cfg.Insecure)
		a.view = viewList
	} else {
		a.view = viewLogin
		a.form.focus = 0
		if cfg.URL != "" {
			a.form.focus = 1
		}
	}
	return a
}

func (a *app) markDirty() { a.dirty.Store(true) }

func (a *app) setStatus(msg string, bad bool) {
	a.mu.Lock()
	a.status, a.statusBad = msg, bad
	a.mu.Unlock()
	a.markDirty()
}

// requestRefresh stößt einen sofortigen Abruf der Geräteliste an (nicht blockierend).
func (a *app) requestRefresh() {
	select {
	case a.refreshNow <- struct{}{}:
	default:
	}
}

// pollLoop lädt die Geräteliste periodisch und auf Anforderung.
func (a *app) pollLoop() {
	t := time.NewTicker(time.Duration(a.cfg.refreshInterval()) * time.Second)
	defer t.Stop()
	for {
		a.reload()
		select {
		case <-t.C:
		case <-a.refreshNow:
		case <-a.done:
			return
		}
	}
}

func (a *app) reload() {
	a.mu.Lock()
	cli := a.cli
	if cli == nil || a.loading {
		a.mu.Unlock()
		return
	}
	a.loading = true
	a.mu.Unlock()
	a.markDirty()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	devs, err := cli.devices(ctx)
	var who string
	if err == nil {
		a.mu.Lock()
		known := a.username
		a.mu.Unlock()
		if known == "" {
			if me, merr := cli.me(ctx); merr == nil {
				who = me.Username
			}
		}
	}
	cancel()

	a.mu.Lock()
	a.loading = false
	switch {
	case err == nil:
		sort.Slice(devs, func(i, j int) bool {
			return strings.ToLower(devs[i].Hostname) < strings.ToLower(devs[j].Hostname)
		})
		a.devices = devs
		if who != "" {
			a.username = who
		}
		a.lastUpdate = time.Now()
		a.status, a.statusBad = "", false
	case isUnauthorized(err):
		a.cli = nil
		a.devices = nil
		a.username = ""
		a.status, a.statusBad = "Anmeldung abgelaufen – bitte neu anmelden.", true
		a.view = viewLogin
		a.form.busy = false
		a.form.err = "Zugang ungültig – bitte neu anmelden."
	default:
		a.status, a.statusBad = "Abruf fehlgeschlagen: "+err.Error(), true
	}
	a.mu.Unlock()
	a.markDirty()
	a.trayDirty.Store(true)
}

// summary zählt Geräte, Offline-Geräte und rote Checks/Tasks für Tray und Kopfzeile.
func (a *app) summary() (total, offline, failing int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range a.devices {
		if d.Revoked {
			continue
		}
		total++
		if d.Status == "offline" {
			offline++
		}
		if d.ChecksFailing > 0 || d.TasksFailing > 0 {
			failing++
		}
	}
	return
}

// updateTray hält Symbolfarbe und Tooltip am Zustand.
func (a *app) updateTray() {
	if a.tr == nil {
		return
	}
	total, offline, failing := a.summary()
	st := iconOK
	tip := fmt.Sprintf("Roster – %d Server, alles grün", total)
	a.mu.Lock()
	loggedOut := a.cli == nil
	a.mu.Unlock()
	switch {
	case loggedOut:
		st, tip = iconIdle, "Roster – nicht angemeldet"
	case failing > 0:
		st = iconAlert
		tip = fmt.Sprintf("Roster – %d von %d Servern mit Fehlern", failing, total)
	case offline > 0:
		st = iconWarn
		tip = fmt.Sprintf("Roster – %d von %d Servern offline", offline, total)
	}
	a.tr.set(st, tip)
}

// filtered liefert die anzuzeigenden Geräte (Suchfeld über Name, Standort, Kunde).
func (a *app) filtered() []device {
	a.mu.Lock()
	devs := append([]device(nil), a.devices...)
	f := strings.ToLower(strings.TrimSpace(a.filter))
	a.mu.Unlock()
	if f == "" {
		return devs
	}
	out := devs[:0]
	for _, d := range devs {
		if deviceMatches(d, f) || anyGuestMatches(d, f) {
			out = append(out, d)
		}
	}
	return out
}

// deviceMatches prüft den Suchbegriff gegen die Gerätedaten selbst.
func deviceMatches(d device, f string) bool {
	hay := strings.ToLower(d.Hostname + " " + d.SiteName + " " + d.ClientName + " " + d.OS)
	return strings.Contains(hay, f)
}

// anyGuestMatches: trifft der Suchbegriff eine VM/einen Container des Hosts?
func anyGuestMatches(d device, f string) bool {
	for _, g := range d.ProxmoxGuests {
		if guestMatches(g, f) {
			return true
		}
	}
	return false
}

// deviceByID sucht ein Gerät für eine ausgelöste Aktion.
func (a *app) deviceByID(id string) (device, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, d := range a.devices {
		if d.ID == id {
			return d, true
		}
	}
	return device{}, false
}
