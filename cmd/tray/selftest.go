package main

import (
	"fmt"
	"time"

	"github.com/jupiterrider/purego-sdl3/sdl"

	"github.com/boonkerz/roster/internal/sdlui"
)

// runSelftest prüft ohne Server, ob die Laufzeitumgebung passt: SDL3 lädt, ein
// Fenster samt Renderer entsteht, das Tray-Symbol wird angenommen und ein
// Menü-Callback erreicht die App. Praktisch beim Einrichten auf einem neuen
// Desktop („warum sehe ich kein Symbol?").
func runSelftest() error {
	if !sdlui.InitVideo(appID) {
		return fmt.Errorf("sdl init: %s", sdl.GetError())
	}
	defer sdl.Quit()

	// Sichtbar und mit HiDPI-Backbuffer: die Skalierung meldet der Compositor erst
	// für ein echtes, angezeigtes Fenster – genau das wollen wir hier messen.
	window := sdl.CreateWindow("Roster Selbsttest", 320, 200, sdl.WindowHighPixelDensity)
	if window == nil {
		return fmt.Errorf("fenster: %s", sdl.GetError())
	}
	defer sdl.DestroyWindow(window)
	renderer := sdl.CreateRenderer(window, "")
	if renderer == nil {
		return fmt.Errorf("renderer: %s", sdl.GetError())
	}
	defer sdl.DestroyRenderer(renderer)
	if _, err := sdlui.NewText(renderer, 14); err != nil {
		return fmt.Errorf("font: %w", err)
	}
	fmt.Println("sdl3 + renderer + schrift: ok")

	var ev sdl.Event
	for i := 0; i < 15; i++ { // dem Compositor Zeit für die Konfiguration geben
		for sdl.PollEvent(&ev) {
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Anzeige-Diagnose – das ist der übliche Grund für „alles winzig": der
	// x11-Treiber meldet unter XWayland keine Skalierung.
	var lw, lh, pw, ph int32
	sdl.GetWindowSize(window, &lw, &lh)
	sdl.GetWindowSizeInPixels(window, &pw, &ph)
	fmt.Printf("anzeige: treiber=%s skalierung=%.2f (displayScale=%.2f pixeldichte=%.2f contentScale=%.2f) fenster=%dx%d pixel=%dx%d\n",
		sdl.GetCurrentVideoDriver(), sdlui.DetectUIScale(window),
		sdl.GetWindowDisplayScale(window), sdl.GetWindowPixelDensity(window),
		sdl.GetDisplayContentScale(sdl.GetPrimaryDisplay()), lw, lh, pw, ph)
	if sdlui.DetectUIScale(window) == 1 && sdl.GetCurrentVideoDriver() == "x11" {
		fmt.Println("hinweis: x11-Treiber ohne Skalierungs-Info – auf HiDPI \"ui_scale\" in tray.json setzen oder Strg+/Strg- benutzen")
	}

	if !sdlui.TrayAvailable() {
		return fmt.Errorf("diese SDL3-Version bringt keine Tray-Unterstützung mit")
	}
	a := newApp(&config{})
	tr, err := newTrayIcon(a)
	if err != nil {
		return fmt.Errorf("tray: %w", err)
	}
	defer tr.destroy()
	fmt.Println("tray-symbol: ok")

	// Menüeintrag programmatisch auslösen – kommt der Wunsch an, funktioniert die
	// Callback-Kette (C → purego → App).
	tr.refresh.Click()
	select {
	case <-a.refreshNow:
		fmt.Println("menü-callback: ok")
	default:
		return fmt.Errorf("menü-callback kam nicht an")
	}
	fmt.Println("selftest ok")
	return nil
}
