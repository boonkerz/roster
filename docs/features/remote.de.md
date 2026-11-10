# Fernsteuerung & Terminal

Roster bringt **eingebaute Fernsteuerung und ein interaktives Terminal** mit – keine
VNC-Software von Dritten, kein zweiter Daemon. Alles läuft über denselben **On-demand-Tunnel**:
der Agent parkt einen leichtgewichtigen Long-Poll, und eine Sitzung wird erst aufgebaut, wenn du
eine startest.

![Fernsteuerung](../screenshots/remote-control.png){ .shadow }

## Fernsteuerung

Der Agent *ist* der VNC-Server (ein eingebauter RFB-Server), es gibt also nichts zusätzlich zu
installieren. Bildschirm, Maus und Tastatur werden durch den Tunnel weitergereicht. Funktionen:

- Abdeckung von **Anmeldebildschirm / UAC / Sperrbildschirm** unter Windows (der Capture-Helper
  folgt dem Eingabe-Desktop).
- **Zwischenablage-Sync**, **Monitorauswahl**, **Dateiübertragung per Drag-&-Drop** auf den
  entfernten Desktop.
- **Live-Auflösungswechsel** (RFB `DesktopSize`) – die Auflösung ändern ohne schwarzen Bildschirm,
  plus ein **Gasttreiber-Installer** per Klick für VMs (Proxmox / VirtIO / SPICE), bei denen die
  Auflösung ausgegraut ist.

### Linux (Wayland und X11)

Unter Linux sucht der Agent die grafische Sitzung des angemeldeten Nutzers selbst – er läuft als
Dienst und kennt dessen Anzeige nicht, liest sie also aus den Prozessen des Nutzers. Es muss
nichts nachinstalliert werden (kein wayvnc, kein x11vnc).

| Desktop | Bild | Maus / Tastatur |
|---|---|---|
| **Wayland mit wlroots** – labwc (Standard auf Raspberry Pi OS), Wayfire, Sway, niri, Hyprland | `wlr-screencopy` (mit Mauszeiger) | virtueller Zeiger/virtuelle Tastatur des Compositors |
| **X11** – Xorg, z.B. Raspberry Pi OS „X11“, XFCE, MATE, Cinnamon | X-Server direkt (Zeiger über XFixes) | XTEST |
| GNOME / KDE unter Wayland | noch nicht unterstützt – der Viewer zeigt einen Hinweis | – |

Ist niemand angemeldet, zeigt der Viewer „Keine grafische Sitzung“ und wechselt von selbst auf den
Desktop, sobald sich jemand anmeldet. Zeichen, die das X11-Tastaturlayout nicht kennt, legt der
Agent kurzzeitig auf eine freie Taste. Bei mehreren Bildschirmen wird der erste übertragen;
eine Monitorauswahl gibt es unter Linux noch nicht.

**Zwischenablage:** Text wird in beide Richtungen abgeglichen – unter X11 über die übliche
Zwischenablage (Strg+C/Strg+V), unter Wayland über das Data-Control-Protokoll des Compositors
(labwc, Sway, Wayfire, niri, Hyprland bieten es an). Was du aus dem Browser einfügst, gehört der
Zwischenablage des Geräts nur, solange die Fernsteuerung offen ist – danach verschwindet
es wie bei jedem geschlossenen Programm. Wie bei VNC üblich werden nur Zeichen aus
Latin-1 übertragen (Umlaute ja, z. B. „€“ oder Emojis nicht).

### Zwei Wege zum Verbinden

=== "Browser (noVNC)"

    Klicke auf **Fernsteuerung öffnen** und es läuft direkt in deinem Browser. Unter Windows/Chromium
    aktiviert der Vollbildmodus die Keyboard Lock API, damit Systemtasten das Gerät erreichen.

=== "Nativer Viewer (empfohlen unter Linux/Wayland)"

    Ein mitgelieferter `roster-viewer` (SDL3, **cgo-frei**, Downloads für Linux/Windows/macOS ohne
    Toolchain) fängt **alle** Tasten per `keyboard-shortcuts-inhibit` ab – einschließlich Win+T,
    Win+Ziffern und Alt+Tab, die der Browser unter Wayland nicht abgreifen kann. Er hat eine
    schwebende Toolbar im AnyDesk-Stil mit **echtem Strg+Alt+Entf** (SendSAS), Sperren der lokalen
    Eingabe, Bildschirmnachricht und einer Qualitätsauswahl. Starte ihn mit einem Klick über einen
    `roster://`-Link oder füge den Startcode in seinen Verbindungsdialog ein. Pakete für jede
    Plattform, die der Server mitführt, sind unter **Einstellungen → Downloads** gelistet
    (Linux x86-64/ARM64, Windows; der macOS-Build hängt am GitHub-Release).

## Remote-Terminal

Eine interaktive Shell über einen On-demand-WebSocket, mit ausklinkbarem Fenster. Unter Windows
kannst du als SYSTEM oder als angemeldeter Benutzer arbeiten.

![Remote-Terminal](../screenshots/terminal.png){ .shadow }

## Zustimmung (beaufsichtigt vs. unbeaufsichtigt)

Remote-Sitzungen können eine **Zustimmung am Gerät** verlangen (eine Abfrage, die der Benutzer
bestätigen muss) oder **unbeaufsichtigt** laufen – konfigurierbar pro Firma/Standort/Gerät, sodass
Benutzer-PCs nachfragen können, während Server unbeaufsichtigt verbunden werden.

## Sicherheit

- Der VNC-Server bindet nur an `127.0.0.1` und läuft **nur während einer Sitzung** – niemals im
  LAN exponiert.
- Der Zugriff unterliegt demselben RBAC wie der Rest der App (Berechtigung `Geräte bedienen`)
  und wird protokolliert.
