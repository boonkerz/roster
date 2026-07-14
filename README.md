# PC-Inventory

**English** · [Deutsch](#deutsch)

A self-hosted, TacticalRMM-style **inventory & remote-management** platform for
computers and servers. A single Go server (with the React web UI and all agent
binaries embedded) talks to a lightweight cross-platform Go agent. Everything ships
as **one binary**; the UI is fully **bilingual (English / German)**.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)
![React](https://img.shields.io/badge/UI-React%20%2B%20TypeScript-61DAFB?logo=react)

---

## Screenshots

**Dashboard** (English UI)

![Dashboard](docs/screenshots/dashboard-en.png)

**Device panel – live utilization** (German UI)

![Live utilization](docs/screenshots/live-de.png)

> More views and both languages: run [`scripts/screenshots.sh`](scripts/screenshots.sh)
> to regenerate all screenshots locally (throwaway demo server + a real agent on your
> machine, captured in English and German into `docs/screenshots/`).

---

## Features

- **Inventory** – hardware, OS, network interfaces (IP/MAC), disks, installed
  software, printers. Cross-platform agents for Windows / Linux / macOS with
  **auto-update**.
- **Checks** – disk / memory / CPU / pending-updates / script, plus native
  **Ping / TCP-port / HTTP-status** checks. Per-check frequency, severity, output
  comparison, platform targeting.
- **Tasks** – scheduled scripts with frequency; last run per task + full run history.
- **Live utilization + history** – on-demand CPU (per core) / RAM / disk / network graphs,
  plus stored CPU/RAM/disk **time series** with 24 h / 7 d / 30 d charts.
- **Remote desktop** – built-in remote control (own RFB/VNC server, no third-party
  software) through the same tunnel: screen + mouse/keyboard, login screen / UAC, clipboard
  sync, monitor selection, drag-&-drop file transfer, **live resolution switch** (+ VM
  guest-driver install for Proxmox/VirtIO/SPICE). Use it in the **browser**, or a bundled
  **native viewer** (`pcinv-viewer` – SDL3 via purego, **cgo-free**, cross-builds for
  Linux/Windows/macOS with no toolchain) with a floating AnyDesk-style toolbar (real
  **Ctrl+Alt+Del** via SendSAS, block local input, on-screen message, quality) and full
  keyboard capture on Wayland – one-click launch via `pcinv://` link.
- **Remote terminal** – interactive shell over an on-demand WebSocket (+ pop-out window).
- **File browser & transfer** – browse the agent's filesystem, download / upload (≤ 32 MB).
- **Services & processes** – list + start/stop/restart services, kill processes.
- **Storage explorer** – TreeSize-style live directory scan with a pie chart.
- **Security collectors** – Antivirus/Defender status, BitLocker (with recovery-key
  escrow), SMART disk health, Windows Event Log / journald viewer.
- **Patch management** – scan, approve, install (apt `upgrade` vs `dist-upgrade`
  selectable), reboot.
- **Self-healing** – automatic remediation: run a script/service-restart when a check fails.
- **Software distribution** – catalog of deployable packages (winget / choco / apt / dnf /
  brew); roll out to a device / client / site / tag.
- **Vulnerability scan (CVE)** – match installed software against **OSV.dev**; per-device
  tab + fleet overview + daily background scan (best coverage for Linux packages).
- **Network discovery** – an agent (or the server itself) scans a CIDR (TCP / ARP / DNS),
  imports hosts as **assets** into a site; adopt them as **unmanaged devices**; **SNMP**
  printer info (model / serial / firmware / page count / toner).
- **Alerting** – modular channels (email / webhook / Pushover / Telegram / ntfy),
  scoped to client/site/device, severity filter, **recovery notifications**,
  **maintenance windows**, optional software-change alerts.
- **Custom fields** – TRMM-style custom fields on client/site/device, JSON collector
  tasks, Twig-like placeholders with filters (`{{ agent.domains | first }}`).
- **Bulk actions** – run a script, update-scan/install, or install software across a whole
  client / site / tag.
- **Tags & Smart Groups** – free labels plus **rule-based** dynamic groups
  (e.g. `OS contains windows AND updates > 0`).
- **Saved filters** – build custom device-list filters (any field, AND/OR) and save them.
- **Install one-liner** – short-URL PowerShell/bash installer per platform.
- **Wake-on-LAN** – wake an offline device via an online neighbor in the same site.
- **Dashboard** – health summary across all devices (online/offline, failing
  checks/tasks, pending patches) + recent status changes.
- **Global search** – hostname, IP/MAC, OS, serial, installed software, custom fields.
- **Reports** – health report per client as printable HTML + scheduled email delivery.
- **RBAC** – three roles: Viewer (read-only), Technician (operate devices), Admin (all).
- **Audit log** – every change action + sign-ins (incl. failed attempts).
- **2FA** – mandatory TOTP with backup codes; admin/CLI reset.
- **Bilingual UI** – switch between English and German at any time.

## Architecture

```
Agent (Go, service) --HTTPS/TLS + bearer token--> Server (Go) <--HTTPS--> Web UI (React)
   Win/Linux/macOS         enroll + check-in          REST API           embedded in binary
                                                          |
                                                   SQLite | PostgreSQL
```

- **Server** – a single Go binary with the React frontend and all agent binaries
  embedded. Runs as a Windows service or Linux systemd service. Database: **SQLite**
  (small deployments) or **PostgreSQL** (large), switchable via `database_url`.
- **Agent** – a CGo-free Go binary running as a service (`kardianos/service`).
  Inventory via `gopsutil` + OS-specific extras.
- **Communication** – outbound HTTPS polling (firewall-friendly, pull model): the
  server queues commands, the agent picks them up on check-in. An on-demand
  "wake" long-poll enables real-time terminal / immediate command execution.

## Quickstart

```bash
# 1. Build the server (embeds the web UI + cross-compiled agents)
make server            # -> bin/server

# 2. Run it (SQLite, HTTP for local testing – see below for production/TLS)
./bin/server run
# On first start it prints a generated admin password. Log in at http://localhost:8443
```

Enroll an agent: in the UI create an **enrollment token** (Settings → Enrollment
tokens), then the "Add computer" dialog generates a ready-to-run install script for
Windows / Linux / macOS.

### Build targets

```bash
make web            # build the React frontend into web/dist
make agents-embed   # cross-compile agents for all platforms into the embed dir
make server         # web + agents-embed + server binary
make agent          # agent for the current platform
make cross          # agents for all OS + server for Linux/Windows
make test           # go test ./...
```

## Configuration

Config comes from a YAML file (`--config`) or environment variables. Common ones:

| Env | Meaning |
|-----|---------|
| `PCINV_ADDR` | Listen address (e.g. `:8443` or `:80`) |
| `PCINV_DB` | `sqlite://./inventory.db` or `postgres://…` |
| `PCINV_BEHIND_PROXY` | `true` when behind a TLS-terminating reverse proxy |
| `PCINV_REQUIRE_2FA` | Enforce TOTP for all users (default `true`) |
| `PCINV_RESULT_RETENTION_DAYS` | History retention (default 30; `0` = keep forever) |

**Production:** do **not** set a fixed seed enrollment token; create tokens in the UI.
Behind a reverse proxy, forward WebSocket upgrades and use generous timeouts (≥ 60 s)
for the `/agent/wait` long-poll and the terminal.

### Account recovery (CLI)

```bash
pc-inventory-server list-users
pc-inventory-server reset-password <user> [new-password]
pc-inventory-server disable-2fa <user>
```

## Security

1. **TLS** for the whole server (own certificate; the agent can pin the CA) or a
   TLS-terminating reverse proxy.
2. **Enrollment**: an admin issues a short-lived token; the agent exchanges it for a
   unique, per-device agent token. Tokens are stored hashed.
3. **Pull model**: agents open only outbound connections — no inbound port on clients.
4. **2FA** (mandatory TOTP), **RBAC**, **audit log**, hashed passwords.

## License

[MIT](LICENSE) © 2026 Thomas Peterson

---
---

<a name="deutsch"></a>

# PC-Inventory (Deutsch)

[English](#pc-inventory) · **Deutsch**

Eine selbst gehostete Plattform im Stil von TacticalRMM zur **Inventarisierung &
Fernverwaltung** von Computern und Servern. Ein einzelner Go-Server (mit
eingebetteter React-Oberfläche und allen Agent-Binaries) kommuniziert mit einem
schlanken, plattformübergreifenden Go-Agent. Alles kommt als **ein Binary**; die
Oberfläche ist vollständig **zweisprachig (Deutsch / Englisch)**.

## Screenshots

> Lokal erzeugen mit [`scripts/screenshots.sh`](scripts/screenshots.sh) (startet eine
> Demo-Instanz + echten lokalen Agent und nimmt die Bilder oben auf).

## Funktionen

- **Inventar** – Hardware, OS, Netzwerkschnittstellen (IP/MAC), Datenträger,
  installierte Software, Drucker. Agents für Windows / Linux / macOS mit **Auto-Update**.
- **Checks** – Disk / RAM / CPU / ausstehende Updates / Skript, dazu native
  **Ping / TCP-Port / HTTP-Status**-Checks. Frequenz, Schweregrad, Ausgabe-Vergleich,
  Plattform-Targeting je Check.
- **Tasks** – geplante Skripte mit Frequenz; letzter Lauf je Task + Lauf-Historie.
- **Live-Auslastung + Verlauf** – on-demand CPU (je Kern) / RAM / Disk / Netzwerk, plus
  gespeicherte CPU/RAM/Disk-**Zeitreihen** mit 24 h / 7 d / 30 d-Charts.
- **Fernsteuerung (Remote Desktop)** – **eingebaute** Fernsteuerung (eigener RFB/VNC-
  Server, keine Fremdsoftware) über denselben Tunnel: Bildschirm + Maus/Tastatur,
  Anmeldebildschirm / UAC, Zwischenablage-Sync, Monitor-Auswahl, Datei-Drag&Drop,
  **Live-Auflösungswechsel** (+ VM-Gasttreiber-Installer für Proxmox/VirtIO/SPICE).
  Nutzbar im **Browser** oder per mitgeliefertem **nativem Viewer** (`pcinv-viewer` –
  SDL3 via purego, **cgo-frei**, cross-baut für Linux/Windows/macOS ohne Toolchain) mit
  schwebender AnyDesk-artiger Bedienleiste (echtes **Strg+Alt+Entf** via SendSAS, Eingaben
  sperren, Meldung, Qualität) und vollständiger Tastatur-Erfassung auf Wayland –
  Ein-Klick-Start via `pcinv://`-Link.
- **Remote-Terminal** – interaktive Shell über On-demand-WebSocket (+ Popout-Fenster).
- **Dateibrowser & -transfer** – Dateisystem durchsuchen, herunter-/hochladen (≤ 32 MB).
- **Dienste & Prozesse** – auflisten + Start/Stop/Neustart, Prozesse beenden.
- **Speicher-Explorer** – TreeSize-artiger Live-Scan mit Tortendiagramm.
- **Security-Collectors** – Virenschutz/Defender, BitLocker (mit Recovery-Key-Escrow),
  SMART-Festplattengesundheit, Windows-Event-Log / journald.
- **Patch-Management** – prüfen, genehmigen, installieren (apt `upgrade` vs
  `dist-upgrade` wählbar), Neustart.
- **Self-Healing** – automatische Behebung: Skript/Dienst-Neustart bei fehlerhaftem Check.
- **Software-Verteilung** – Katalog verteilbarer Pakete (winget / choco / apt / dnf /
  brew); Ausrollen auf Gerät / Client / Standort / Tag.
- **Schwachstellen-Scan (CVE)** – Abgleich installierter Software gegen **OSV.dev**;
  Tab je Gerät + Flotten-Übersicht + täglicher Hintergrund-Scan (beste Abdeckung: Linux).
- **Netzwerk-Discovery** – ein Agent (oder der Server selbst) scannt eine CIDR (TCP / ARP /
  DNS), importiert Hosts als **Assets** in eine Site; als **nicht verwaltete Geräte**
  übernehmbar; **SNMP**-Druckerinfos (Modell / Seriennr. / Firmware / Seitenzähler / Toner).
- **Alerting** – modulare Kanäle (E-Mail / Webhook / Pushover / Telegram / ntfy),
  Geltungsbereich Client/Site/Gerät, Schweregrad-Filter, **Recovery-Meldungen**,
  **Wartungsfenster**, optional Software-Änderungs-Alarme.
- **Custom Fields** – eigene Felder auf Client/Site/Gerät, JSON-Collector-Tasks,
  Twig-artige Platzhalter mit Filtern (`{{ agent.domains | first }}`).
- **Sammelaktionen** – Skript, Update-Scan/-Installation oder Software-Installation auf
  ganze Clients / Standorte / Tags.
- **Tags & Smart Groups** – freie Labels plus **regelbasierte** dynamische Gruppen
  (z. B. `OS enthält windows UND Updates > 0`).
- **Eigene Filter** – Geräteliste per Bedingungen (beliebige Felder, UND/ODER) filtern und
  benannt speichern.
- **Install-One-Liner** – Kurz-URL-Installer (PowerShell/bash) je Plattform.
- **Wake-on-LAN** – offline Geräte über einen Nachbarn im selben Standort wecken.
- **Dashboard** – Health-Übersicht über alle Geräte + letzte Statuswechsel.
- **Globale Suche** – Hostname, IP/MAC, OS, Seriennr., Software, Custom Fields.
- **Berichte** – Health-Report je Kunde als druckbares HTML + geplanter E-Mail-Versand.
- **RBAC** – Viewer (nur lesen), Techniker (bedienen), Admin (alles).
- **Audit-Log** – alle ändernden Aktionen + Anmeldungen (inkl. Fehlversuche).
- **2FA** – TOTP-Pflicht mit Backup-Codes; Reset per Admin/CLI.
- **Zweisprachig** – jederzeit zwischen Deutsch und Englisch umschaltbar.

## Architektur

```
Agent (Go, Dienst) --HTTPS/TLS + Bearer-Token--> Server (Go) <--HTTPS--> Web-UI (React)
   Win/Linux/macOS       Enroll + Checkin           REST API          im Binary eingebettet
                                                        |
                                                 SQLite | PostgreSQL
```

Ausgehendes HTTPS-Polling (firewall-freundlich, Pull-Modell): der Server stellt
Befehle in eine Queue, der Agent holt sie beim Checkin ab. Ein On-demand-„Wake"-
Long-Poll ermöglicht Echtzeit-Terminal und sofortige Befehlsausführung.

## Schnellstart

```bash
make server        # baut bin/server (inkl. Web-UI + Agents)
./bin/server run   # SQLite, HTTP – druckt beim ersten Start das Admin-Passwort
```

Agent registrieren: in der UI ein **Enrollment-Token** anlegen (Einstellungen →
Enrollment-Tokens); der Dialog „Neuer Computer" erzeugt ein fertiges Install-Skript
für Windows / Linux / macOS.

## Sicherheit

1. **TLS** für den gesamten Server (eigenes Zertifikat; Agent kann die CA pinnen)
   oder ein TLS-terminierender Reverse-Proxy.
2. **Enrollment**: ein Admin erzeugt ein kurzlebiges Token; der Agent tauscht es gegen
   ein eindeutiges Agent-Token pro Gerät. Tokens werden gehasht gespeichert.
3. **Pull-Modell**: Agents öffnen nur ausgehende Verbindungen — kein offener Port am Client.
4. **2FA** (TOTP-Pflicht), **RBAC**, **Audit-Log**, gehashte Passwörter.

## Lizenz

[MIT](LICENSE) © 2026 Thomas Peterson
