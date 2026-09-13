package sdlui

import (
	"os"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// InitVideo initialisiert das SDL-Video-Subsystem, setzt die Anwendungs-ID und
// bevorzugt auf einer Wayland-Sitzung den Wayland-Treiber.
//
// appID ist die umgekehrte Domain-Schreibweise des Desktop-Eintrags (z. B.
// "de.boonkerz.roster.tray"). Wayland nutzt sie als app_id, X11 als WM_CLASS – nur
// wenn sie zum Dateinamen der .desktop-Datei passt, zeigen Leiste und Anwendungs-
// menü Symbol und Namen der App an.
//
// Zum Treiber: sind DISPLAY und WAYLAND_DISPLAY gesetzt (Compositor mit XWayland),
// wählt SDL je nach Build den x11-Treiber. Der meldet unter XWayland KEINE
// Skalierung – das Fenster bekommt aber physische Pixel (bei 3840×2160 mit Scale
// 1,75 also 1,75× mehr), sodass die Oberfläche winzig gerendert wird. Mit dem
// Wayland-Treiber liefert SDL die fraktionale Skalierung korrekt.
//
// Die Vorgabe läuft über SDL_SetHint statt über eine Umgebungsvariable: os.Setenv
// erreicht in cgo-freien Builds die C-Bibliothek nicht, SDL würde die Variable also
// gar nicht sehen. Hat der Nutzer selbst einen Treiber vorgegeben, bleibt es dabei;
// schlägt Wayland fehl, wird ohne Vorgabe erneut versucht (X11-Fallback).
func InitVideo(appID string) bool {
	if appID != "" {
		sdl.SetHint(sdl.HintAppID, appID)
	}
	if !driverForced() && os.Getenv("WAYLAND_DISPLAY") != "" {
		sdl.SetHint(sdl.HintVideoDriver, "wayland")
		if sdl.Init(sdl.InitVideo) {
			return true
		}
		sdl.SetHint(sdl.HintVideoDriver, "")
	}
	return sdl.Init(sdl.InitVideo)
}

// driverForced meldet, ob der Nutzer den Videotreiber selbst vorgegeben hat
// (SDL_VIDEO_DRIVER ist der SDL3-Name, SDL_VIDEODRIVER der klassische).
func driverForced() bool {
	return os.Getenv("SDL_VIDEO_DRIVER") != "" || os.Getenv("SDL_VIDEODRIVER") != ""
}
