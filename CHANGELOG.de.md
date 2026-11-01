# Changelog

Alle nennenswerten Änderungen an Roster stehen hier. Das Format folgt
[Keep a Changelog](https://keepachangelog.com/de/1.1.0/), die Versionen
[Semantic Versioning](https://semver.org/lang/de/). Englische Fassung: [CHANGELOG.md](https://github.com/boonkerz/roster/blob/master/CHANGELOG.md).

## [Unreleased]

### Behoben
- **Automatische Sperre sperrt keine privaten/internen Adressen mehr** (10/8, 172.16/12, 192.168/16, 100.64/10, fd00::/7, Link-Local). Hintergrund: Eine Log-Quelle hinter einem Reverse-Proxy sah nur die Docker-interne Adresse des Proxys; der Vorfall „Angriffsmuster“ sperrte sie flottenweit in DOCKER-USER und trennte den Proxy von den Backends. Der Vorfall wird weiter angelegt, die Reaktion steht auf *skipped: private address*. Wer es wirklich will, schaltet es unter Einstellungen → Sicherheitsüberwachung ein; manuelle Sperren bleiben möglich.

### Neu
- Sicherheit → Sperren: **Suche** (IP/Netz, Gerät, Grund, Ersteller) und **Status-Filter** (wirksam/eingereiht, abgelaufen, aufgehoben, fehlgeschlagen) – serverseitig, bis 1.000 Zeilen; Knopf **überall** hebt eine IP mit einem Klick auf allen Geräten auf (`POST /security/unblock-ip`).

## [0.18.4] - 2026-11-01

### Behoben
- Log-Quellen: Protokollzeilen von **nginx-proxy** (forego-Präfix mit Farbcodes) wurden nicht sauber ausgepackt; dadurch griff die Erkennung von Anfragen an die nackte Server-IP nicht. Jetzt werden ANSI-Codes entfernt. Dazu neue Angriffsmuster: Befehls-/Code-Injection in Pfad oder Query (`cmd=`, `=curl`, `${jndi`, `<script`, `/etc/passwd`, `/proc/self` …), versteckte Dateien (`/.git`, `/.terraform`, `/.env`), Zugangsdaten-/Konfigurationsdateien (`server.key`, `credentials`, `.tfvars`, `.toml`, `.properties`, `.pem` …), **503** eines Reverse-Proxys ohne passenden Backend-Host zählt wie 404, und fehlgeschlagene Anfragen an die **IP-Adresse statt des Domainnamens** (Default-Host `_`) gelten als Scanner. An einem echten nginx-proxy-Protokoll (5.000 Zeilen) steigt die Erkennung von 1.820 auf 2.709 Treffer.

## [0.18.3] - 2026-11-01

### Neu
- Richtlinien → Sicherheit → Log-Quellen: **Testen** – der Agent eines wählbaren Geräts liest die letzten Zeilen der Quelle (auch vor dem Speichern) und zeigt gefundene Dateien/Container, Treffer und eine Stichprobe der erkannten Ereignisse; passt kein Container, die Namen der laufenden Container.
- Gerät → Anmeldungen: Karte **Überwachung auf diesem Gerät** – wirksame Anmeldungs-Überwachung, Datei-Integrität, Compliance und Log-Quellen mit dem vom Agent gemeldeten Lesestatus (Richtlinie, gefundene Ziele, Ereignisse seit Agent-Start, letzter Treffer, Fehler) und Testen je Quelle. Der Agent meldet den Status mit jedem Checkin.
- **Länder der Quell-IPs**: eingebettete IP-zu-Land-Datenbank (ip-location-db, Public Domain, kein externer Dienst). Flaggen an Vorfällen, Anmeldeereignissen und Sperren, Spalte *Land* bei den Sperren, Kacheln *Angriffsquellen nach Land (24 h)* und *Gesperrte IPs nach Land (30 Tage)* auf der Seite Sicherheit, und die Dashboard-Kennzahlen **Gesperrte IPs nach Land** und **Angriffsquellen nach Land** (Balken/Ring/Verlauf; neue Nutzer bekommen das Länder-Diagramm standardmäßig). `make geoip` erneuert die Datenbank.

## [0.18.2] - 2026-11-01

### Neu
- Richtlinien → Benachrichtigungen: **Sicherheitsvorfälle melden** – eine Checkbox je Vorfallsart (Brute-Force, Anmeldung nach Brute-Force, Angriffsmuster, neue Quell-IP, Kontoänderung, Kontosperre, Datei-Änderung) und die Option *nicht melden, wenn die Quell-IP automatisch gesperrt wurde*. Vorfälle werden immer gespeichert; das steuert nur die Alarmkanäle (Push, E-Mail …). Die Meldungen enthalten jetzt die ausgeführte Reaktion.
- Sicherheits-Log-Quellen: Format **Mail** (Postfix SASL, Dovecot, Exim) und Apache-Unterstützung im Web-Fehlerprotokoll; die Vorlagen sind jetzt allgemein (Webserver als Datei oder Docker-Container, SSH-/SFTP-Container, FTP, Mail, eigenes Muster).

### Behoben
- Richtlinien: das Formular für Log-Quellen lief aus dem Rahmen; Layout neu aufgebaut.

## [0.18.1] - 2026-11-01

### Geändert
- Richtlinien: der Richtlinien-Editor ist in Tabs gegliedert – **Checks, Tasks, Backups, Benachrichtigungen, Sicherheit, Zuweisungen** – mit Anzahl der Einträge je Tab, statt einer langen Seite. README und Landing-Page beschreiben jetzt das Flotten-Patch-Management und die Sicherheitsüberwachung inkl. Log-Quellen und automatischer Sperre.

## [0.18.0] - 2026-11-01

### Neu
- Sicherheit: **automatische Sperre ohne Richtlinien-Regel** – Einstellungen → Sicherheitsüberwachung kann die Quell-IP jedes Brute-Force-, Anmeldung-nach-Brute-Force- und Angriffsmuster-Vorfalls auf dem meldenden Gerät sperren (Dauer von einer Stunde bis dauerhaft). Optional sperrt die **Flotten-Sperre** die Adresse auf allen Geräten, deren Richtlinie Anmeldungen überwacht oder Log-Quellen liest, und die **Netzbereichs-Sperre** das ganze IPv4-/24, sobald binnen 24 Stunden mehrere auffällige IPs aus demselben Netz kamen.
- Sicherheit: **Log-Quellen** je Richtlinie – der Agent liest jede Minute zusätzliche Protokolle: Dateien (Glob) oder die Ausgabe von Docker-Containern (Name oder Muster). Formate: nginx-Zugriffsprotokoll (401 → Fehlversuch; Scanner-Pfade, kaputte Anfragen → verdächtige Anfrage), nginx-Fehlerprotokoll, sshd-Ausgabe von SSH-/SFTP-Containern (z. B. atmoz/sftp), FTP-Server (vsftpd, proftpd, pure-ftpd) und eigene reguläre Ausdrücke. Vorlagen für nginx-proxy- und SFTP-Container.
- Sicherheit: neuer Vorfall **Angriffsmuster** (N verdächtige Anfragen von einer IP im Zeitfenster, Standard 20) mit denselben Reaktionen wie Brute-Force, und eine zweite Brute-Force-Regel für **verteilte, langsame Angriffe**: N Versuche mit unbekannten Benutzernamen in 24 Stunden (Standard 5) unabhängig vom Schwellwert je Zeitfenster.
- Sicherheit: **Mehrfach-Sperren** im Tab Vorfälle – viele Vorfälle auswählen, alle Quell-IPs mit einem Klick sperren (Dauer, Flotten-Option) und dabei bestätigen. Manuelle Sperren akzeptieren Netze in CIDR-Schreibweise und können alle überwachten Geräte treffen.

### Geändert
- Agent: IP-Sperren greifen jetzt auch im nftables-**forward**-Hook (bzw. `FORWARD`/`DOCKER-USER` bei iptables), damit gesperrte Adressen keine Docker-Container mit veröffentlichten Ports mehr erreichen; Netze liegen in Interval-Sets. Ältere Agents sperren weiterhin nur im input-Hook.

## [0.17.3] - 2026-11-01

### Neu
- Sicherheit: manuelles IP-Sperren im Tab **Sperren** (Gerät, IP, Dauer, Grund) und Dauer-Auswahl am Vorfall – eine Stunde, 24 Stunden, 7 Tage, 30 Tage oder **dauerhaft**.

### Behoben
- Sicherheit: Eine bereits gesperrte IP erzeugt beim erneuten Sperren keinen doppelten Eintrag mehr – die bestehende Sperre wird mit der neuen Dauer erneuert (so wird auch eine befristete Sperre dauerhaft).
- Schwachstellen: Die Spalte **Stufe** war bei fast allen Funden leer. OSV-Details wurden nur für die ersten 300 Einträge je Scan geladen und CVSS-Vektoren nicht ausgewertet. Der Scanner leitet die Stufe jetzt aus Distributions-Prioritäten (Ubuntu/Debian) ab, rechnet CVSS-v2/v3-Basiswerte, erbt die Stufe verknüpfter CVEs (z. B. USN-Notizen), nutzt bereits gespeicherte Details wieder, lädt bis zu 1000 neue parallel und nimmt die Langbeschreibung, wenn die Zusammenfassung fehlt.

## [0.17.2] - 2026-11-01

### Behoben
- Agent: **Checkin-Sturm** behoben – Checks mit Häufigkeit „jeder Checkin“ lieferten immer frische Ergebnisse und stießen damit sofort den nächsten Checkin an, sodass Agents etwa jede Sekunde statt im eingestellten Intervall eincheckten. Ein Sofort-Checkin wird jetzt nur noch bei geändertem Check-Status oder nach einem Task-Lauf angefordert, höchstens alle 30 Sekunden.

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

[Unreleased]: https://github.com/boonkerz/roster/compare/v0.18.4...HEAD
[0.18.4]: https://github.com/boonkerz/roster/compare/v0.18.3...v0.18.4
[0.18.3]: https://github.com/boonkerz/roster/compare/v0.18.2...v0.18.3
[0.18.2]: https://github.com/boonkerz/roster/compare/v0.18.1...v0.18.2
[0.18.1]: https://github.com/boonkerz/roster/compare/v0.18.0...v0.18.1
[0.18.0]: https://github.com/boonkerz/roster/compare/v0.17.3...v0.18.0
[0.17.3]: https://github.com/boonkerz/roster/compare/v0.17.2...v0.17.3
[0.17.2]: https://github.com/boonkerz/roster/compare/v0.17.1...v0.17.2
[0.17.1]: https://github.com/boonkerz/roster/compare/v0.17.0...v0.17.1
[0.17.0]: https://github.com/boonkerz/roster/compare/v0.16.2...v0.17.0
[0.16.2]: https://github.com/boonkerz/roster/compare/v0.16.1...v0.16.2
[0.16.1]: https://github.com/boonkerz/roster/compare/v0.16.0...v0.16.1
[0.16.0]: https://github.com/boonkerz/roster/compare/v0.15.0...v0.16.0
[0.15.0]: https://github.com/boonkerz/roster/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/boonkerz/roster/releases/tag/v0.14.0
