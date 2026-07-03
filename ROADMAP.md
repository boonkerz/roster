# PC-Inventory – Feature-Liste / Roadmap

Stand: laufend gepflegt. Die ursprüngliche Roadmap ist **komplett umgesetzt**
(siehe „Erledigt" weiter unten). Der folgende **Ideenpool** sammelt mögliche
nächste Features – noch nichts davon ist gebaut.

## Ideenpool – mögliche nächste Features

### Top-Empfehlungen (größter Mehrwert, bauen auf vorhandener Infrastruktur auf)

- [x] **Automatisierung / Self-Healing** – Remediation-Skript je Check: wechselt ein Check
  auf „failing", führt der Server automatisch das hinterlegte Skript auf dem Gerät aus
  (Cooldown 30 min gegen Flapping, Audit-Eintrag). Konfiguration im Policy-Check-Formular.
  (Geplante Auto-Aufgaben gibt es bereits über Tasks.)
- [ ] **Metrik-Historie + Verlaufsgraphen** – Zeitreihen für CPU/RAM/Disk/Netz speichern und
  als Charts (24 h / 7 d / 30 d) zeigen (aktuell nur Live). Basis für Trend-/Kapazitätsalarme.
- [ ] **Software-Verteilung** – Pakete/Installer gezielt ausrollen (Windows winget/choco,
  Linux apt/dnf): „installiere Firefox auf allen Geräten dieses Standorts". Ergänzt
  Patch-Management um Drittsoftware.

### Fernzugriff

- [ ] **Remote-Desktop** – Bildschirm ansehen/steuern (WebRTC/VNC-Tunnel). Groß, aufwändig.
- [ ] **Sitzungs-/Power-Steuerung** – angemeldete Benutzer abmelden, Nachricht an den Nutzer,
  Shutdown/Sleep (Neustart + WoL sind vorhanden).

### Sicherheit & Compliance

- [ ] **Schwachstellen-Abgleich** – installierte Softwareversionen gegen CVE-Feeds prüfen.
- [ ] **Compliance-/Baseline-Checks** (CIS-artig), Firewall-Status, offene Ports,
  lokale Admin-Konten auditieren.

### Integration & API

- [ ] **Öffentliche REST-API + API-Tokens** für Automatisierung/Skripting von außen.
- [ ] **Native Slack/Teams/Discord-Alarm-Kanäle** (Webhook/Telegram/ntfy/Pushover vorhanden).
- [ ] **Webhook-Ausgänge für Ticketsysteme** (PSA-Anbindung).

### Organisation & Daten

- [ ] **Smart Groups** – dynamische Gruppen per Regel (z. B. „OS = Windows & Check fehlerhaft").
- [ ] **CSV/Excel-Export** von Geräte-/Software-/Audit-Listen.
- [ ] **Asset-Verwaltung** – Kaufdatum, Garantie, Kosten, Standort (dedizierte Felder).
- [ ] **Hardware-Änderungs-Tracking** (analog zum Software-Tracking).

### Betrieb

- [ ] **Agent-Installer als MSI/pkg/deb** statt Skript (einfacheres GPO-/MDM-Rollout).
- [ ] **Server-Backup/Restore** (DB + Config), Konfig-Export.
- [ ] **Prometheus-`/metrics`-Endpoint** für den Server selbst (Selbstüberwachung).
- [ ] **Mandantenfähigkeit / MSP-Modus** mit Kunden-Portal (read-only je Kunde) + Branding.

---

## Erledigt

Alle folgenden Punkte sind umgesetzt und live.

## Ursprüngliche Roadmap (umgesetzt)

- [x] **Software-Änderungs-Tracking** – Software-Inventar zwischen Checkins diffen,
  neu installierte / entfernte / aktualisierte Programme protokollieren (Verlauf).
  Offen: optional alarmieren.
- [x] **Übersichts-Dashboard** – Health-Zusammenfassung über alle Geräte: fehlschlagende
  Checks, ausstehende Patches, Offline-Geräte, Task-Fehler; Status-Donut + letzte Wechsel.

## Geplant – schnelle Gewinne (nutzen vorhandene Plumbing)

- [x] **Wartungsfenster / Alarme stummschalten** – Zeitfenster pro Client/Site/Gerät, in dem
  `alertTransitions` nichts sendet (Checks laufen + Verlauf bleibt). Verwaltung in Einstellungen.
- [x] **Native Netzwerk-Checks** – Ping / TCP-Port / HTTP-Status als eigene Check-Typen
  (mit optionalen Latenz-Schwellen; Platzhalter in Host/URL nutzbar).
- [x] **Dienste & Prozesse** – Windows-Dienste / systemd-Units + Prozesse on-demand,
  mit Start/Stop/Neustart bzw. Beenden über die Command-Queue (Tab „Dienste/Prozesse").
- [x] **Wake-on-LAN** – Magic Packet von einem online Nachbar-Agent im selben Standort
  („Aufwecken"-Button bei offline Geräten).
- [x] **Sammelaktionen (Bulk)** – Skript ausführen oder Update-Scan auf allen Geräten
  eines Ziels (Client/Site/Tag/alle); Nav „Sammelaktion".

## Geplant – größer, hoher Wert

- [x] **Audit-Log** – wer hat wann was getan (Login/Fehlversuche + alle ändernden
  Aktionen via Middleware); Ansicht in Einstellungen.
- [x] **Feingranulare Rollen (RBAC)** – Viewer (nur lesen), Techniker (Geräte bedienen:
  Skripte/Dienste/Prozesse/Updates/Neustart/WoL/Notizen/Sammelaktion), Admin (alles).
- [x] **Dateibrowser / -transfer** – Verzeichnisse durchsuchen, Dateien
  herunterladen/hochladen (bis 32 MB) über dedizierte Transfer-Endpoints. Tab „Dateien".
- [x] **Geplante Reports (E-Mail/HTML)** – Health-Bericht je Kunde (Geräte online/offline,
  fehlerhafte Checks/Tasks, ausstehende Patches). On-demand als HTML (druckbar zu PDF)
  + geplanter Versand (täglich/wöchentlich/monatlich) über einen Alarm-Kanal.

## Sicherheits-/Inventar-Collectors (Tabs „Sicherheit" + „Ereignisse")

- [x] **Defender/AV-Status** (Windows Get-MpComputerStatus; Linux ClamAV-Hinweis).
- [x] **BitLocker-Status** (+ Recovery-Key-Escrow serverseitig).
- [x] **SMART-Festplattengesundheit** (Windows Get-PhysicalDisk, Linux smartctl).
- [x] **Windows-Event-Log / journald-Viewer** (on-demand, Filter).
- [x] **Geräte-Notizen / Doku** pro Gerät (Übersicht-Tab, auch durchsuchbar).

## Erledigt (Auszug)

- [x] Inventar (Hardware, Software, Netzwerk, Datenträger), cross-platform Agents mit Auto-Update
- [x] Checks (Disk/Memory/CPU/Updates/Script) mit Frequenz, Schweregrad, Ausgabe-Vergleich, Plattform-Targeting
- [x] Tasks (geplante Skripte) mit Frequenz; letzter Lauf je Task + Lauf-Historie
- [x] Custom Fields (TRMM-Stil) + JSON-Collector + Twig-artige Platzhalter mit Filtern
- [x] Modulares Alerting (E-Mail/Webhook/Pushover/Telegram/ntfy), Scope + Schweregrad, Recovery-Meldung
- [x] Check-Statuswechsel-Verlauf (Historie) inkl. Benachrichtigungs-Status
- [x] Remote-Terminal (On-demand Wake-Poll) + Popout-Fenster
- [x] Patch-Management (Scan/Genehmigen/Installieren)
- [x] Ad-hoc-Skripte mit Push-Trigger; Command-Queue
- [x] 2FA (TOTP, Pflicht) + Backup-Codes
- [x] Organisation: Client/Site/Device-Hierarchie, Tags/Gruppen
- [x] Neustart, Token-Widerruf
- [x] TreeSize-Speicheransicht (live hochzählend, Tortendiagramm)
- [x] Konto-Selbstverwaltung (Web-UI): Kontodaten + Passwort ändern
- [x] Konto-Wiederherstellung per CLI (list-users, reset-password, disable-2fa)
- [x] Historie-Pruning (30 Tage), Deploy hinter Reverse-Proxy
- [x] Dienste & Prozesse (on-demand + Steuerung), Wake-on-LAN, Sammelaktionen
- [x] Globale Gerätesuche (Hostname, IP/MAC, OS, Seriennr., Software, Custom Fields)
- [x] Live-Auslastung (CPU je Kern / RAM / Disk / Netzwerk, fortlaufend abgefragt)
- [x] Zweisprachige Oberfläche (Deutsch / Englisch) mit Sprachumschalter
- [x] Öffentliches Repo (GitHub): MIT-Lizenz, zweisprachiges README, Screenshots

## Offene Kleinigkeiten / Schulden

- [x] **2FA-Reset durch Admin** in der Web-UI (Benutzerliste → „2FA zurücksetzen").
- [x] **Software-Änderungen alarmieren** (opt-in in den Alarm-Einstellungen).
- [x] **Audit-Log-Aufbewahrung** (Pruning, doppelte Retention wie sonst).
