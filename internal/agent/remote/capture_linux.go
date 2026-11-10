//go:build linux

package remote

import (
	"errors"
	"fmt"
	"log/slog"
)

// newScreenSource sucht die grafische Sitzung des angemeldeten Nutzers: Wayland
// (wlroots: labwc/Wayfire/Sway – Raspberry Pi OS) oder X11. Ohne Sitzung bzw. bei nicht
// unterstütztem Desktop zeigt ein Hinweisbild den Grund (statt eines Testbilds).
func newScreenSource(log *slog.Logger, monitor int) (screenSource, error) {
	sessions := findGUISessions()
	if len(sessions) == 0 {
		return newInfoSource("Keine grafische Sitzung", "Am Gerät ist niemand an einem Desktop angemeldet (Wayland oder X11).", "Die Anzeige erscheint, sobald sich jemand anmeldet."), nil
	}
	var reasons []string
	for _, s := range sessions {
		if s.wayland != "" {
			src, err := newWaylandSource(s.wayland, monitor)
			if err == nil {
				log.Info("fernsteuerung: wayland-sitzung", "nutzer", s.user, "socket", s.wayland, "groesse", fmt.Sprintf("%dx%d", src.w, src.h), "eingaben", src.vptr != 0 && src.vkbd != 0)
				return src, nil
			}
			reasons = append(reasons, err.Error())
			if errors.Is(err, errNoScreencopy) {
				// GNOME/KDE: Xwayland würde nur X11-Fenster zeigen – nicht nutzen.
				return newInfoSource("Desktop nicht unterstützt", "Wayland-Sitzung von "+s.user+" ohne wlr-screencopy (GNOME/KDE).", "Unterstützt: wlroots-Desktops (labwc, Wayfire, Sway) und X11-Sitzungen."), nil
			}
			continue
		}
		if s.display != "" {
			src, err := newX11Source(s.display, s.xauth, s.home)
			if err == nil {
				log.Info("fernsteuerung: x11-sitzung", "nutzer", s.user, "display", s.display, "groesse", fmt.Sprintf("%dx%d", src.w, src.h), "eingaben", src.input)
				return src, nil
			}
			reasons = append(reasons, err.Error())
		}
	}
	return nil, fmt.Errorf("keine Aufnahme möglich: %v", reasons)
}

// RunCaptureHelper existiert nur unter Windows (Session-0-Umgehung); anderswo No-op.
func RunCaptureHelper(_ int) {}
