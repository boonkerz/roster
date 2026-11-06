---
title: Roster
hide:
  - navigation
  - toc
---

<div class="rs-hero" markdown>

<div class="rs-hero-text" markdown>

# Deine Flotte, ein Binary. { .rs-title }

**Roster** ist eine selbst gehostete Plattform für **Inventar und Fernwartung** im Stil
von TacticalRMM, für Windows, Linux und macOS, mit flottenweiter Patch-Verwaltung und
Sicherheitsüberwachung an Bord. Ein Go-Server mit eingebetteter Web-UI und allen Agents,
ein schlanker Agent pro Rechner, sonst nichts zu installieren.

[Erste Schritte](getting-started.md){ .md-button .md-button--primary }
[GitHub](https://github.com/boonkerz/roster){ .md-button }
[Changelog](changelog.md){ .md-button }

```bash
curl -LO https://github.com/boonkerz/roster/releases/latest/download/roster-server-linux-amd64
chmod +x roster-server-linux-amd64 && ./roster-server-linux-amd64 run
```

<p class="rs-hero-note">MIT-Lizenz · SQLite ohne Konfiguration, PostgreSQL optional · UI auf Deutsch und Englisch</p>

</div>

<div class="rs-hero-shot" markdown>
![Roster-Übersicht](screenshots/dashboard.png){ .shadow }
</div>

</div>

<div class="rs-pillars" markdown>

- :material-cube-outline: **Ein Binary, selbst gehostet.** Keine externen Dienste, kein zweiter Daemon. Hinter den eigenen Reverse-Proxy stellen oder TLS direkt ausliefern lassen.
- :material-monitor-cellphone: **Agents für alle Plattformen:** Windows, Linux, macOS und FreeBSD, mit Auto-Update bei jedem neuen Server-Release – auf OPNsense als upgrade-festes Plugin.
- :material-remote-desktop: **Echte Fernsteuerung eingebaut.** Browser oder nativer Viewer, interaktives Terminal und Dateibrowser über einen Tunnel auf Abruf.
- :material-shield-account: **Feingranulare Rechte.** Eigene Rollen plus Firmen- und Standortbereich je Benutzer, Zwei-Faktor-Login, Audit-Log.

</div>

## Alles, was ein RMM braucht { .rs-center }

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Geräteliste mit eigenen Spalten und Filter](screenshots/devices-filter.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Inventar, das Fragen beantwortet
Hardware, Betriebssystem, Netzwerk, Datenträger, Software, Drucker, Temperaturen,
Docker-Container: automatisch erfasst und bei jedem Checkin aktualisiert. Eigene Felder
anlegen, Spalten wählen und auf alles filtern, gespeichert je Benutzer.
[Inventar](features/inventory.md){ .rs-more }
</div>
</section>

<section class="rs-feature rs-reverse" markdown>
<div class="rs-feature-shot" markdown>
![Verlaufsdialog eines Checks](screenshots/check-history.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Checks, Tasks und Selbstheilung
Disk, RAM, CPU, Updates, Ping, TCP, HTTP, offene Ports, Dienste, Zertifikate,
Temperaturen und eigene Skripte. Jedes Ergebnis landet im Verlauf, ein Klick öffnet das
Diagramm. Fehlgeschlagene Checks können erst ein Reparatur-Skript ausführen, bevor
jemand alarmiert wird.
[Checks & Tasks](features/checks-tasks.md){ .rs-more }
</div>
</section>

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Interaktives Terminal im Browser](screenshots/terminal.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Fernsteuerung, Terminal und Dateien
Bildschirm im Browser oder im nativen Viewer übernehmen, mit Auflösungswechsel im Betrieb
und echtem Strg+Alt+Entf. Interaktive Shell oder Dateisystem über denselben Tunnel.
Keine VNC-Software von Dritten, nichts am Gerät nach außen offen.
[Fernsteuerung & Terminal](features/remote.md){ .rs-more }
</div>
</section>

<section class="rs-feature rs-reverse" markdown>
<div class="rs-feature-shot" markdown>
![Übersicht mit konfigurierbaren Diagrammen](screenshots/dashboard-dark.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Eine Übersicht, die du selbst zusammenstellst
Ringe, Balken und Verlaufslinien über jede Kennzahl, jeden Check-Status oder Feldwert.
Kacheln per Drag-and-drop anordnen, Breite wählen, und aus stündlichen Snapshots wächst
ein Jahr Historie. Helles und dunkles Design inklusive.
[Übersicht](features/dashboard.md){ .rs-more }
</div>
</section>

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Rollen und Datenbereich](screenshots/data-scope.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Rollen und Datenbereich
Eigene Rollen aus einzelnen Berechtigungen zusammenstellen und Benutzer auf bestimmte
Firmen oder Standorte beschränken. Zwei-Faktor-Authentifizierung, API-Tokens für die
iOS-App und ein Audit-Log sind eingebaut.
[Rollen & Berechtigungen](features/permissions.md){ .rs-more }
</div>
</section>

<section class="rs-feature rs-reverse" markdown>
<div class="rs-feature-shot" markdown>
![Richtlinien mit Checks und Tasks](screenshots/policy-detail.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Richtlinien, Patches und Software
Checks, Tasks und Backups an Firmen, Standorte oder einzelne Geräte zuweisen. Die Seite
„Patches“ zeigt alle ausstehenden Updates der Flotte, Sicherheits-Updates zuerst: gesammelt
freigeben und installieren, den Patch-Verlauf verfolgen, Paketversionen und Repositories
vergleichen und sehen, welchen Betriebssystemen das Support-Ende bevorsteht. Von Ubuntu
zurückgestellte Phased Updates sind markiert und lassen sich auf Wunsch sofort mitinstallieren.
[Patches & Software](features/patching.md){ .rs-more }
</div>
</section>

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Sicherheitsvorfälle](screenshots/security-incidents.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Sicherheitsüberwachung
Datei-Integritätsüberwachung mit Baseline je Gerät, Anmeldungs-Überwachung, die
Brute-Force, Anmeldungen nach Brute-Force, neue Quell-IPs und Kontoänderungen zu
Vorfällen macht, CIS-artige Compliance-Prüfungen mit Score je Gerät und Active Response,
die eine IP sperrt oder ein Konto automatisch deaktiviert. Liest auch die Protokolle von
nginx-, SFTP- und FTP-Containern (mit Test-Knopf und Lesestatus je Gerät), sperrt Angreifer
flottenweit oder je Netz ohne Klick, und die Sperre gilt auch für Docker-Container.
Quell-IPs zeigen ihr Land aus einer eingebetteten Datenbank: Flaggen, Länderfilter bei den
Vorfällen und Dashboard-Diagramme gesperrter IPs und Angriffsquellen je Land.

[Sicherheitsüberwachung](features/security-monitoring.md){ .rs-more }
</div>
</section>

<section class="rs-feature rs-reverse" markdown>
<div class="rs-feature-shot" markdown>
![Bedrohungsdaten-Feeds](screenshots/security-threat.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Bedrohungsdaten & tiefe Host-Überwachung
Fünf öffentliche Blocklisten (Spamhaus DROP, blocklist.de, Feodo Tracker, CINS Army,
Tor-Exit-Nodes) werden täglich geladen: gelistete Angreifer sind überall markiert und werden
beim ersten Ereignis flottenweit gesperrt. Windows meldet Dienstinstallationen, geplante
Aufgaben, gelöschte Sicherheitsprotokolle, verdächtige Prozesse und PowerShell-Skriptblöcke;
Registry, Verursacher von Dateiänderungen (auditd/SACL), Systemzustand (Ports, Module, Cron,
Admin-Konten, Autostarts) und Rootkit-Spuren werden ebenso überwacht, geänderte Dateien per
ClamAV oder Defender geprüft. HAProxy, Traefik, MySQL, Firewall-Drops, Journal und
Docker-Ereignisse speisen Portscan- und Container-Exec-Erkennung. 106 CIS-artige
Compliance-Regeln tragen Framework-Referenzen, jeder Vorfall seine MITRE-ATT&CK-Techniken.
[Sicherheitsüberwachung](features/security-monitoring.md#bedrohungsdaten){ .rs-more }
</div>
</section>

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Container-Sicherheit: Images mit Schwachstellen und Diensten](screenshots/security-containers.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Container-Sicherheit
Der Agent liest die Pakete jedes laufenden Container-Images und gleicht sie mit dem passenden
Distributions-Release gegen OSV.dev ab – ohne Docker Scout, Trivy oder Registry-Anmeldung.
Gleiche Images auf vielen Hosts erscheinen als eine Zeile, Dienste wie sshd im Image sind
markiert. MongoDB-, MySQL- und PostgreSQL-Container melden ihren Datenbank-Bestand: Neue,
gelöschte oder geleerte Datenbanken werden zum Vorfall – das Muster der Datenbank-Erpressung.
[Container-Sicherheit](features/security-monitoring.md#container-sicherheit){ .rs-more }
</div>
</section>

<section class="rs-feature rs-reverse" markdown>
<div class="rs-feature-shot" markdown>
![Netzwerk-Scan](screenshots/network-scan.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Netzwerk-Erkennung und mehr
CIDR-Bereiche scannen, Druckerdetails per SNMP lesen, unverwaltete Hosts übernehmen.
Proxmox-VE-Hosts melden Gäste und Backup-Zustand, geplante Backups laufen mit
Startbedingung und Folgeaktion, und eine Taskleisten-App hält die Flotte einen Klick
entfernt.
[Netzwerk & Erkennung](features/network.md){ .rs-more } · [Proxmox VE](features/proxmox.md){ .rs-more } · [Taskleisten-App](features/tray-app.md){ .rs-more }
</div>
</section>

<div class="rs-cta" markdown>

## In ein paar Minuten startklar { .rs-center }

Server-Binary laden, starten, den ersten Agent mit dem generierten Einzeiler anbinden.
**[Erste Schritte](getting-started.md)** führt durch Build, Start, Enrollment und die
Organisation der Flotte.

[Erste Schritte](getting-started.md){ .md-button .md-button--primary }
[Konfiguration](configuration.md){ .md-button }

</div>
