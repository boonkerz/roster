# Changelog

Alle nennenswerten Änderungen an Roster stehen hier. Das Format folgt
[Keep a Changelog](https://keepachangelog.com/de/1.1.0/), die Versionen
[Semantic Versioning](https://semver.org/lang/de/). Englische Fassung: [CHANGELOG.md](https://github.com/boonkerz/roster/blob/master/CHANGELOG.md).

## [Unreleased]

## [0.17.1] - 2026-11-01

### Behoben
- Richtlinien: Der Richtlinien-Editor stürzte beim Laden des Compliance-Regelkatalogs ab („Cannot read properties of null“) – Regeln ohne Profil-Einschränkung kamen als `null`; der Server liefert jetzt leere Listen, der Editor verträgt fehlende Felder.

## [0.17.0] - 2026-11-01

### Neu
- Patches: neue Seite **Patches** mit allen ausstehenden Updates der Flotte – Sicherheits-Updates
  zuerst, Filter (Firma, Standort, nur Sicherheit, nur genehmigte, Suche), Mehrfachauswahl mit
  Genehmigen/Aufheben und Installieren (genehmigte / Sicherheit / genehmigte Sicherheit / alle,
  apt-Strategie). Kopfkacheln (Geräte mit Sicherheits-Updates, Anteil ohne Sicherheits-Updates,
  nie geprüft).
- Patches: der Agent meldet **Sicherheits-Updates** getrennt (apt `-security`-Suiten, dnf-Security-
  Advisories, Windows-Update-Kategorien/MSRC-Schweregrad, macOS „Security“) sowie neue und
  installierte Version und Quelle. Spalte und Filter „Sicherheits-Updates“ in der Geräteliste,
  Smart-Group-Feld `security_updates_count`, Übersichts-Kachel und Statistik-Kennzahl `security_patches`.
- Patches: „Sicherheits-Updates installieren“ je Gerät, in der Sammelleiste der Geräteliste und in
  der Sammelaktion (Umfang: alle / genehmigte / nur Sicherheit, apt-Strategie).
- Patches: **Patch-Verlauf** – der Agent liest installierte, aktualisierte und entfernte Pakete aus
  den Paketmanager-Logs (apt `history.log` inkl. rotierter Dateien, `dnf.rpm.log`, `yum.log`,
  `pacman.log`), der Windows-Update-Historie und `softwareupdate --history` (macOS). Flotten-
  Zeitleiste auf der Patches-Seite (Tab „Verlauf“ mit Zeitraum/Aktion/Firma-Filter), 90-Tage-Verlauf
  je Gerät im Patches-Tab, Aufbewahrung ein Jahr; der Health-Bericht bekommt die Spalte
  „Installiert (7 Tage)“.
- Patches: **Paket- und Repository-Inventar** – Tab „Pakete“ (installierte Software der Flotte
  nach Paket und Version mit Verteilungsbalken und den Geräten je Version) und Tab „Repositories“
  (apt inkl. deb822, dnf/yum, zypper, pacman, apk, winget, brew; Filter auf Fremd-Repos,
  deaktivierte Quellen und Quellen ohne Signaturprüfung). Je Gerät: Paketquellen im Patches-Tab,
  **Status automatischer Updates** (unattended-upgrades, dnf-automatic, Windows Update, macOS),
  **laufender vs. installierter Kernel** (Neustart-Hinweis; der Reboot-Check nutzt jetzt auch
  `needs-restarting` und den Kernelvergleich) sowie OS-Details (Codename, Windows-Build/Edition,
  macOS-Build). Neue Spalten, Filter und Smart-Group-Felder (`auto_updates`, `os_codename`,
  `os_edition`, `kernel_pending`).
- Patches: **OS-Lebenszyklus (EOL)** – der Server gleicht die OS-Version jedes Geräts (Ubuntu,
  Debian, RHEL-Familie, Fedora, Alpine, openSUSE/SLES, Amazon/Oracle Linux, FreeBSD, macOS,
  Windows-Client inkl. Enterprise-/LTSC-Zyklen, Windows Server) und Proxmox VE mit endoflife.date
  ab (täglich abgerufen, serverseitig gecacht). Tab „Lebenszyklus“ auf der Patches-Seite, Badge
  „Support-Ende“ am Gerät, Listen-Spalte, Filter und Smart-Group-Feld `eol_status`, Übersichts-
  Kachel, Alarm bei nahendem (Vorwarnzeit einstellbar, Standard 90 Tage) oder erreichtem
  Support-Ende. Einstellungen → Benachrichtigungen: Vorwarnzeit, Alarm-Schalter und „Jetzt neu
  bewerten“. Rolling-Release-Systeme werden übersprungen.
- Sicherheit: **Datei-Integritätsüberwachung (FIM)** – Richtlinien bekommen den Bereich
  „Sicherheitsüberwachung“ mit überwachten Pfaden (Datei, Verzeichnis oder Glob; rekursiv;
  Ausschluss-Globs; Plattform; Alarm an/aus; „Standardpfade einfügen“ für /etc, /usr/local/bin,
  ~/.ssh, cron, Windows hosts/Tasks/Autostart, macOS LaunchDaemons) und einem Prüfabstand. Der
  Agent hält eine Baseline (SHA-256, Rechte, Besitzer, Änderungszeit) in seinem Zustandsverzeichnis
  und meldet neue, geänderte, gelöschte Dateien sowie Rechte-/Besitzerwechsel; Alarme werden je
  Checkin gebündelt. Neue Seite „Sicherheit“ mit dem flottenweiten Tab „Datei-Integrität“ (Filter,
  Bestätigen), Geräte-Tab „Datei-Integrität“ (neu prüfen, Baseline neu setzen) in der neuen
  Tab-Gruppe „Sicherheit“, Zähler offener Änderungen am Gerät.
- Sicherheit: **Anmeldungs-Überwachung und Vorfälle** – ist „Anmeldungen überwachen“ in einer
  Richtlinie aktiv, liest der Agent SSH-/Konsolen-Anmeldungen, sudo/su und Kontoänderungen
  inkrementell aus journald bzw. `/var/log/auth.log`/`secure`, dem Windows-Sicherheitsprotokoll
  (4624/4625/4740/4720/4726/4728/4732 …) und dem macOS Unified Log. Der Server leitet **Vorfälle**
  ab: Brute-Force je Quell-IP (Schwellwert und Zeitfenster einstellbar), erfolgreiche Anmeldung nach
  Brute-Force (kritisch), erste Anmeldung von neuer Quell-IP (nach Lernphase), Konto-/Gruppen-
  änderungen (Admin-Gruppen als Warnung) und Sperrungen – jeweils über die üblichen Kanäle
  gemeldet. Sicherheitsseite mit Tabs „Vorfälle“ (bestätigen/erledigen, Filter, häufigste
  Quell-IPs) und „Anmeldungen“, Geräte-Tab „Anmeldungen“, Zähler offener Vorfälle in Navigation,
  Geräteliste und Übersicht. Neuer Einstellungsbereich „Sicherheitsüberwachung“ (Schwellwerte,
  Lernphase, Nie-sperren-Liste, Cooldown).
- Sicherheit: **Compliance-/Baseline-Prüfung** – CIS-artiger Regelkatalog (Linux: SSH-Einstellungen,
  Rechte von /etc/shadow und /etc/passwd, leere Passwörter, weitere UID 0, sudo NOPASSWD, Passwort-
  alter, Firewall, auditd, NTP, SELinux/AppArmor, ASLR, IP-Forwarding, ICMP-Redirects, SYN-Cookies,
  weltweit beschreibbares /etc, telnet/rsh, Core-Dumps; Windows: UAC, SMBv1, RDP-NLA/aus, Gast- und
  Administratorkonto, Firewall-Profile, Defender, BitLocker, AutoPlay, LLMNR, Kennwortrichtlinie,
  Kontosperre, PowerShell 2.0, WDigest, LSA-PPL; macOS: FileVault, Firewall, Tarnmodus, Gatekeeper,
  SIP, Bildschirmschoner-Passwort, Gast, Remote Login; alle: automatische Updates, ausstehender
  Neustart). Je Richtlinie aktivierbar mit Profil (Server/Arbeitsplatz), Intervall und Regelauswahl;
  der Agent wertet aus und meldet einen Score. Geräte-Tab „Compliance“ (Befunde, Empfehlungen, neu
  prüfen), Flottenansicht nach Gerät und nach Regel auf der Sicherheitsseite, Listen-Spalte, Filter
  und Smart-Group-Feld `compliance_score`, Übersichts-Kachel sowie der neue Check-Typ
  **Compliance-Score** (Mindest-Score, optional Fehler bei jeder Regel mit Schwere „hoch“) für
  Alarme und Remediation.
- Sicherheit: **Active Response** – Richtlinien reagieren automatisch auf Vorfälle: Quell-IP für
  N Minuten oder dauerhaft sperren (Linux nftables mit Set-Timeouts, iptables-Fallback; Windows-
  Firewall-Regel; macOS pf-Tabelle `roster_block`), lokales Konto sperren (nie root/Administrator)
  oder ein Bibliotheks-Skript mit `ROSTER_TRIGGER`, `ROSTER_SOURCE_IP`, `ROSTER_USER`,
  `ROSTER_DEVICE` in der Umgebung ausführen. Nie-sperren-Liste und Cooldown unter Einstellungen →
  Sicherheitsüberwachung; Loopback und die Server-Adresse sind immer geschützt. Jede Reaktion wird
  auditiert und mit Ergebnis am Vorfall angezeigt; Datei-Integritäts-Alarme erzeugen jetzt auch einen
  Vorfall `fim_change`, damit sie Skripte auslösen können. Sicherheitsseite mit Tab „Sperren“ (Status,
  Ablauf, Aufheben), manuelle Knöpfe „IP sperren“ / „Konto sperren“ am Vorfall, aktive Sperren im
  Geräte-Tab „Anmeldungen“. Neue Agent-Kommandos `block_ip`, `unblock_ip`, `lock_user`,
  `unlock_user`, `list_blocks`.
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
- API: `PUT /settings/general` ändert nur noch die gesendeten Felder (Zeitzone, EOL-Vorwarnzeit,
  EOL-Alarme).
- API: `POST /bulk/install-updates` versteht `approved`, `security_only` und `apt_mode`;
  `POST /devices/{id}/install-updates` versteht `security_only`. Update-Einträge tragen `version`,
  `current_version`, `source`, `security` und `kind`.
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

[Unreleased]: https://github.com/boonkerz/roster/compare/v0.17.1...HEAD
[0.17.1]: https://github.com/boonkerz/roster/compare/v0.17.0...v0.17.1
[0.17.0]: https://github.com/boonkerz/roster/compare/v0.16.2...v0.17.0
[0.16.2]: https://github.com/boonkerz/roster/compare/v0.16.1...v0.16.2
[0.16.1]: https://github.com/boonkerz/roster/compare/v0.16.0...v0.16.1
[0.16.0]: https://github.com/boonkerz/roster/compare/v0.15.0...v0.16.0
[0.15.0]: https://github.com/boonkerz/roster/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/boonkerz/roster/releases/tag/v0.14.0
