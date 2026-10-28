# Changelog

Alle nennenswerten Änderungen an Roster stehen hier. Das Format folgt
[Keep a Changelog](https://keepachangelog.com/de/1.1.0/), die Versionen
[Semantic Versioning](https://semver.org/lang/de/). Englische Fassung: [CHANGELOG.md](https://github.com/boonkerz/roster/blob/master/CHANGELOG.md).

## [Unreleased]

### Neu
- Richtlinien: Bereich **Benachrichtigungen** – Software-Änderungsalarme lassen sich je Richtlinie
  ein-/ausschalten (Vorrang Gerät > Standort > Firma), nach Änderungsart (installiert/entfernt/
  aktualisiert) und über Ignorier-Muster (z. B. `Microsoft Edge*`) filtern. Die Einstellung am Gerät
  ist jetzt dreistufig (*Wie Richtlinie / Melden / Nicht melden*) und zeigt den wirksamen Wert samt Quelle.
- Checks: Verlaufsdiagramm je Check am Gerät (Messwert + Statusstreifen, 1 h–30 d),
  `check_samples` mit 10 Tagen Aufbewahrung.
- Übersicht: konfigurierbare Diagramme (Kennzahlen, Check-Status, Feldwerte) mit stündlicher
  Statistik-Historie; Geräte-Status und Status-Wechsel als Kacheln; Kachelbreite und Drag-and-drop.
- Geräteliste: Filter auf benutzerdefinierte Felder (gesetzt/nicht gesetzt, typgerechter Vergleich);
  wählbare Spalten inkl. benutzerdefinierter Felder, gespeichert im Benutzerkonto; markierte Geräte
  als CSV exportieren mit Spaltenauswahl.
- Taskleisten-App: Neustart-Knopf, SSH-Schlüssel für SFTP, helles Thema, Einstellungen; Suche trifft
  auch benutzerdefinierte Felder.
- Doku: zweisprachige Website (DE/EN) mit Landing-Page und neuen Screenshots auf roster-rmm.dev.
- Changelog (diese Datei) und automatische GitHub-Release-Notizen.

### Geändert
- Check-Alarme bei Auto-Remediation werden erst gemeldet, wenn die Remediation nicht geholfen hat.
- Geräteliste: Sammelleiste bestätigt inline statt per Browser-Dialog.
- Web-UI: `no-cache` für index.html und `immutable` für gehashte Assets.
- Übersicht: Diagramme füllen die Kachelbreite und sitzen mittig.

### Entfernt
- Checkbox „Software-Änderungen nicht melden“ am Gerät (ersetzt durch die dreistufige Einstellung;
  stummgeschaltete Geräte werden zu *Nicht melden* migriert). API: `PUT /devices/{id}/mute-software`
  ersetzt durch `PUT/GET /devices/{id}/software-alerts`.

## [0.16.2] - 2026-09-29

### Neu
- Proxmox-Backups: Aufbewahrung „behalte die letzten N“ und Aufräumen voller Speicher.
- Taskleisten-App: Detail-Panel mit Custom Fields, scharfer Text; als Download in den Einstellungen.
- Geräteliste: nach fehlerhaftem Check filtern und Auswahl per Sammelaktion behandeln.
- Viewer und Taskleisten-App: `--install` richtet alles in einem Schritt ein.

## [0.16.1] - 2026-09-22

### Behoben
- Backup-Check: erfolgreicher Lauf zählt, auch wenn das Zielgerät schläft.

## [0.16.0] - 2026-09-18

### Neu
- Zentrale Zeitzone für Backup- und Agent-Zeitpläne.
- Backup-Zielspeicher je Gast.

## [0.15.0] - 2026-09-18

### Neu
- Backup-Bereich in den Richtlinien (serverseitig geplant, Startbedingung, Folgeaktion auf
  anderem Gerät).
- Viewer-Downloads in den Einstellungen.

## [0.14.0] - 2026-09-13

### Neu
- Taskleisten-App (Tray + Fenster mit Check-/Task-Status, Terminal, Viewer, SFTP).
- Proxmox-Gäste über den Agent auf dem PVE-Host (Start/Stop, Backup-Status).
- Temperatursensoren mit Verlauf und Check `temperature`.

[Unreleased]: https://github.com/boonkerz/roster/compare/v0.16.2...HEAD
[0.16.2]: https://github.com/boonkerz/roster/compare/v0.16.1...v0.16.2
[0.16.1]: https://github.com/boonkerz/roster/compare/v0.16.0...v0.16.1
[0.16.0]: https://github.com/boonkerz/roster/compare/v0.15.0...v0.16.0
[0.15.0]: https://github.com/boonkerz/roster/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/boonkerz/roster/releases/tag/v0.14.0
