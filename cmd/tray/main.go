// Command roster-tray ist die Taskleisten-App von Roster: ein Symbol im System-Tray
// und ein schlankes Fenster, das nach der Anmeldung alle Server mit Check- und
// Task-Ergebnis listet. Je Zeile öffnet ein Knopf ein Remote-Terminal (im
// Terminal-Emulator des Systems, siehe term.go) oder die Fernsteuerung im nativen
// roster-viewer.
//
// Cgo-frei (CGO_ENABLED=0): gerendert wird mit SDL3 über das purego-Binding, die
// SDL3-Laufzeit wird zur Laufzeit geladen (Linux: libSDL3.so.0, Windows: SDL3.dll,
// macOS: libSDL3.dylib) – wie beim Viewer.
package main

import (
	"flag"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
)

var version = "dev"

// appID muss zum Dateinamen des Desktop-Eintrags passen (deploy/linux/
// de.boonkerz.roster.tray.desktop) – sonst findet die Leiste kein Symbol.
const appID = "de.boonkerz.roster.tray"

func main() {
	runtime.LockOSThread() // SDL: Video/Events müssen auf dem Main-Thread laufen.
	log.SetFlags(0)

	var (
		termDevice = flag.String("term", "", "Remote-Terminal zu dieser Geräte-ID öffnen (interner Modus)")
		shell      = flag.String("shell", "", "Shell fürs Remote-Terminal (shell|bash|cmd|powershell)")
		runas      = flag.String("runas", "system", "Kontext fürs Remote-Terminal (system|user)")
		hold       = flag.Bool("hold", false, "Am Ende auf Tastendruck warten (für selbst geöffnete Terminalfenster)")
		showVer    = flag.Bool("version", false, "Version ausgeben")
		hidden     = flag.Bool("hidden", false, "Nur ins Tray starten, ohne Fenster (Autostart)")
		shot       = flag.String("screenshot", "", "Einen Frame als PNG speichern und beenden (Doku/Diagnose)")
		selftest   = flag.Bool("selftest", false, "SDL3, Fenster und Tray-Symbol prüfen und beenden")
		writeIcon  = flag.String("write-icon", "", "Anwendungssymbol als PNG schreiben (Paketierung/Desktop-Eintrag)")
		iconSize   = flag.Int("icon-size", 256, "Kantenlänge für --write-icon")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("roster-tray " + version)
		return
	}

	if *writeIcon != "" {
		if err := writeIconPNG(*writeIcon, *iconSize); err != nil {
			log.Fatalf("roster-tray: symbol schreiben: %v", err)
		}
		return
	}

	if *selftest {
		if err := runSelftest(); err != nil {
			log.Fatalf("roster-tray: selftest: %v", err)
		}
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("roster-tray: konfiguration: %v", err)
	}

	// Terminal-Modus: kein SDL, nur stdin/stdout ↔ WebSocket.
	if *termDevice != "" {
		if err := runTerminal(cfg, *termDevice, *shell, *runas, *hold); err != nil {
			log.Fatalf("roster-tray: %v", err)
		}
		return
	}

	// Zweiter Start (z. B. nochmal aus dem Anwendungsmenü): laufende Instanz nach
	// vorn holen, statt ein zweites Tray-Symbol anzulegen.
	if *shot == "" && notifyRunning() {
		return
	}

	if err := runUI(cfg, *hidden, *shot); err != nil {
		log.Fatalf("roster-tray: %v", err)
	}
}

func runUI(cfg *config, startHidden bool, screenshot string) error {
	// Auf Wayland-Sitzungen den Wayland-Treiber bevorzugen: der x11-Treiber meldet
	// unter XWayland keine Skalierung, wodurch die Oberfläche auf HiDPI winzig wird.
	if !sdlui.InitVideo(appID) {
		return fmt.Errorf("sdl init: %s", sdl.GetError())
	}
	defer sdl.Quit()

	a := newApp(cfg)

	// HighPixelDensity: auf HiDPI bekommt das Fenster den vollen Pixel-Backbuffer,
	// wir zeichnen in Pixeln und skalieren Schrift/Maße entsprechend – scharf statt
	// hochgerechnet.
	flags := sdl.WindowResizable | sdl.WindowHidden | sdl.WindowHighPixelDensity
	window := sdl.CreateWindow("Roster", baseWinW, baseWinH, flags)
	if window == nil {
		return fmt.Errorf("fenster: %s", sdl.GetError())
	}
	defer sdl.DestroyWindow(window)
	renderer := sdl.CreateRenderer(window, "")
	if renderer == nil {
		return fmt.Errorf("renderer: %s", sdl.GetError())
	}
	defer sdl.DestroyRenderer(renderer)
	sdl.SetRenderDrawBlendMode(renderer, sdl.BlendModeBlend)

	a.win, a.rn = window, renderer
	a.updatePixelRatio()
	a.scale = a.autoScale()
	txt, err := sdlui.NewText(renderer, float64(baseFontPx*a.scale))
	if err != nil {
		return fmt.Errorf("font: %w", err)
	}
	a.txt = txt
	a.resizeToScale()
	log.Printf("anzeige: treiber=%s skalierung=%.2f pixel/logisch=%.2f", sdl.GetCurrentVideoDriver(), a.scale, a.pxRatio)
	sdl.StartTextInput(window)

	// Tray-Symbol; fehlt die Unterstützung (alte SDL3-Version, keine
	// StatusNotifier-Leiste), läuft die App als normales Fenster weiter.
	if tr, terr := newTrayIcon(a); terr == nil {
		a.tr = tr
		defer tr.destroy()
	} else {
		log.Printf("kein Tray-Symbol (%v) – Fenster bleibt sichtbar", terr)
		startHidden = false
	}

	if !startHidden {
		a.showWindow()
		a.rescale() // nach dem Anzeigen kennt der Compositor erst die endgültige Skalierung
	}
	a.updateTray()

	defer a.listenSingleton()()

	go a.pollLoop()
	defer close(a.done)

	// Screenshot-Modus: einmal Daten holen, einen Frame zeichnen, speichern, fertig.
	if screenshot != "" {
		a.showWindow()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			var ev sdl.Event
			for sdl.PollEvent(&ev) {
				a.handleEvent(&ev)
			}
			a.render()
			a.mu.Lock()
			ready := a.cli == nil || !a.lastUpdate.IsZero()
			a.mu.Unlock()
			if ready {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		a.draw() // ohne Present – RenderReadPixels liest den Backbuffer
		return a.saveScreenshot(screenshot)
	}

	a.markDirty()
	a.loop()
	return nil
}

// loop ist die Event-Schleife: sie wartet ereignisgesteuert (max. 100 ms), damit
// die App im Leerlauf praktisch keine CPU braucht, und zeichnet nur bei Bedarf neu.
func (a *app) loop() {
	var ev sdl.Event
	for !a.quitting {
		if sdl.WaitEventTimeout(&ev, 100) {
			a.handleEvent(&ev)
			for sdl.PollEvent(&ev) { // Rest der Warteschlange in einem Rutsch
				a.handleEvent(&ev)
			}
		}
		// Wünsche aus dem Tray-Menü abarbeiten (dort läuft nur ein Flag-Setzen).
		if a.wantShow.Swap(false) {
			a.showWindow()
			a.requestRefresh()
		}
		if a.wantLogout.Swap(false) {
			a.logout()
			a.showWindow()
		}
		if a.armed != "" && time.Since(a.armedAt) > confirmWindow {
			a.armed = "" // Bestätigung verfallen: Knopf wieder normal zeichnen
			a.markDirty()
		}
		if a.trayDirty.Swap(false) {
			a.updateTray() // SDL-Tray nur vom Hauptthread anfassen
		}
		if a.wantQuit.Swap(false) {
			a.quitting = true
			break
		}
		if a.visible && a.dirty.Swap(false) {
			a.render()
		}
	}
}

func (a *app) showWindow() {
	sdl.ShowWindow(a.win)
	sdl.RaiseWindow(a.win)
	a.visible = true
	a.markDirty()
}

// hideWindow versteckt das Fenster ins Tray. Ohne Tray-Symbol wäre die App danach
// nicht mehr erreichbar – dann wird stattdessen beendet.
func (a *app) hideWindow() {
	if a.tr == nil {
		a.quitting = true
		return
	}
	sdl.HideWindow(a.win)
	a.visible = false
}
