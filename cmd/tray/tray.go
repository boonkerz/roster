package main

import (
	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
)

// trayIcon kapselt das Symbol in der Taskleiste samt Kontextmenü. Die Menüpunkte
// setzen nur Wünsche (atomare Flags); abgearbeitet werden sie in der Event-Schleife.
type trayIcon struct {
	t       *sdlui.Tray
	icons   map[iconState]*sdl.Surface
	cur     iconState
	tip     string
	refresh *sdlui.TrayItem // für den Selbsttest der Callback-Kette
}

func newTrayIcon(a *app) (*trayIcon, error) {
	icons := map[iconState]*sdl.Surface{
		iconIdle:  makeIcon(iconIdle),
		iconOK:    makeIcon(iconOK),
		iconWarn:  makeIcon(iconWarn),
		iconAlert: makeIcon(iconAlert),
	}
	t, err := sdlui.NewTray(icons[iconIdle], "Roster")
	if err != nil {
		for _, s := range icons {
			sdl.DestroySurface(s)
		}
		return nil, err
	}
	t.AddItem("Roster öffnen", func() { a.wantShow.Store(true) })
	refresh := t.AddItem("Jetzt aktualisieren", func() { a.requestRefresh() })
	t.AddSeparator()
	t.AddItem("Abmelden", func() { a.wantLogout.Store(true) })
	t.AddItem("Beenden", func() { a.wantQuit.Store(true) })
	return &trayIcon{t: t, icons: icons, cur: iconIdle, refresh: refresh}, nil
}

func (t *trayIcon) set(st iconState, tip string) {
	if st != t.cur {
		if s := t.icons[st]; s != nil {
			t.t.SetIcon(s)
			t.cur = st
		}
	}
	if tip != t.tip {
		t.t.SetTooltip(tip)
		t.tip = tip
	}
}

func (t *trayIcon) destroy() {
	if t == nil {
		return
	}
	t.t.Destroy()
	for _, s := range t.icons {
		sdl.DestroySurface(s)
	}
}
