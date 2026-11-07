# Changelog

Alle nennenswerten Änderungen an Roster stehen hier. Das Format folgt
[Keep a Changelog](https://keepachangelog.com/de/1.1.0/), die Versionen
[Semantic Versioning](https://semver.org/lang/de/). Englische Fassung: [CHANGELOG.md](https://github.com/boonkerz/roster/blob/master/CHANGELOG.md).

## [Unreleased]

## [0.21.4] - 2026-11-07

### Behoben
- Abläufe: Beim Öffnen eines gespeicherten Laufs aus dem Verlauf zeigt der Assistent jetzt die Parameter und den Vorschau-Server dieses Laufs statt der Standardwerte des Ablaufs.

## [0.21.3] - 2026-11-07

### Geändert
- Abläufe: *Dateien finden* trifft nie die eigenen Backups (`*.roster-bak-*`, `*.roster-tmp`) – bisher erfasste ein Muster wie `verzeichnis/*` sie beim zweiten Lauf. *Text ersetzen* ohne Treffer **beendet den Ablauf jetzt ohne Fehler** (Status „nicht betroffen“: Gerät schon umgestellt), pull/up/Neustart entfallen; die Option „Weitermachen, auch wenn nichts ersetzt wurde“ behält das alte Verhalten.

## [0.21.2] - 2026-11-07

### Neu
- Abläufe: Ablauf **kopieren** (Liste und Editor, „Name (Kopie)“; im Editor wird der aktuelle, auch ungespeicherte Stand kopiert) und **einzelne Läufe löschen** im Lauf-Verlauf. Route `POST /flows/{id}/copy`, `DELETE /flow-runs/{id}`.

## [0.21.1] - 2026-11-07

### Behoben
- Abläufe: Der Baustein *Docker Compose* funktioniert jetzt auch nach *Dateien finden* im Pfad-Modus – Compose-Dateien unter den Treffern (docker-compose*.yml, compose*.yaml) werden zum Projekt (Name aus den Labels laufender Container im Verzeichnis, sonst der Ordnername). Bisher brach die Vorschau mit „kein Compose-Projekt bekannt“ ab.

## [0.21.0] - 2026-11-07

### Neu
- **Abläufe (Flow-Designer).** Neue Seite *Abläufe*: Bausteine (Dateien finden, Text ersetzen, Docker Compose, Befehl/Skript, API abfragen, Prüfen, Zurückrollen, Warten) auf einer Zeichenfläche (React Flow) über die Ausgänge ok/Fehler verbinden, mit Parametern und Platzhaltern (`{{param.X}}`, `{{steps.ID.value|out|json.pfad}}`, Gerätefelder). Der Agent führt den Graphen nativ aus (Befehl `run_flow`) – Vorschau als Trockenlauf mit Diffs auf einem Server, Testlauf auf diesem Server, danach Rollout auf Firma/Standort/Tag/alle oder die betroffenen Geräte. Backup je Datei, automatisches Zurückrollen bei unverbundenem Fehler-Ausgang, Lauf-Verlauf. Vorlagen: *Image in Compose ersetzen* (auch aus *Sicherheit → Container*: „Image ersetzen …“), *Wert in Datei ersetzen und Dienste neu starten*, *API abfragen und Wert eintragen*. Migration 0078.

## [0.20.2] - 2026-11-06

### Neu
- **OPNsense: Agent übersteht Firmware-Upgrades.** Der Agent registriert sich beim Start als OPNsense-Plugin `os-roster-agent`: lokales Paket, gebaut mit dem `pkg` der Firewall (ABI `FreeBSD:*:amd64`, sonst Host-ABI), lokales Repo `/var/lib/roster/pkg` (+ `/usr/local/etc/pkg/repos/roster.conf`), Eintrag in der `config.xml` per `register.php`, Dienst über `/etc/rc.conf.d/rosteragent`. Läuft nach jedem Selbst-Update erneut. Fehlen nach einem Upgrade Dateien, unter System → Firmware → Plugins neu installieren. Das Installationsskript schreibt zusätzlich `/etc/rc.conf.d/rosteragent`.

## [0.20.1] - 2026-11-06

### Neu
- **Sicherheit → Container:** Flottenübersicht aller Container-Images (über Geräte gebündelt, Filter kritisch/hoch und SSH/FTP/Telnet, aufklappbar mit Geräten und Treffern) und Datenbank-Bestand aller DB-Container mit Link zu den Datenbank-Abweichungen. Route `GET /security/containers`.

## [0.20.0] - 2026-11-06

### Neu
- **Container-Sicherheit.** Der Agent liest je Image eines laufenden Containers einmal täglich per `docker exec` os-release und Paketliste (apk/dpkg/rpm); der Server gleicht sie mit dem versionierten Ökosystem (Alpine 3.x, Debian 12, Ubuntu 24.04, Rocky/Alma) gegen OSV.dev ab. Ergebnis je Image im Docker-Tab (Basis-OS, Pakete, kritisch/hoch, erkannte Dienste wie sshd/ftpd, „nicht prüfbar“ bei distroless), vollständige Liste im Tab Schwachstellen und in der Flotten-Schwachstellenansicht („Container-Images“). Button „Images neu scannen“, Befehl `scan_images`. Migration 0077.
- **Datenbank-Abweichung (`db_change`).** In MongoDB-, MySQL/MariaDB- und PostgreSQL-Containern listet der Agent je Checkin Datenbanken samt Collection-/Tabellenzahl (Zugangsdaten aus der Container-Umgebung, werden nie übertragen). Der Server vergleicht mit dem vorigen Checkin und meldet den Vorfall „Datenbank-Abweichung“: neue Datenbank = Warnung; gelöschte oder geleerte Datenbank, Erpresser-Namen (README/WARNING/RECOVER/…) oder „Daten weg + Hinweis-Datenbank“ = kritisch. MITRE T1485/T1486, Active-Response-Auslöser (Skript). Docker-Tab zeigt den Bestand je Container.
- **Docker-Checks:** Modi „Datenbanken im Container“ (Erwartungsliste, optional Container-Filter) und „Kein Image älter als N Tage“ (nutzt den neuen Image-Erstellzeitpunkt).

### Behoben
- CVE-Scan: „Behoben in“ nimmt jetzt die Version des abgefragten Ökosystems/Releases (Ubuntu/Debian-Datensätze führen je Release einen Eintrag; bisher stand oft die Version eines älteren Releases da).

## [0.19.1] - 2026-11-05

### Neu
- Ubuntu **Phased Updates**: Der Agent fragt per `apt-get -s dist-upgrade` ab, welche Updates apt wegen Phasing zurückstellt, und markiert sie in der Update-Liste (Badge „zurückgestellt (Phasing)“). Die Installationsaktionen am Gerät, in der Sammelaktion und auf der Patches-Seite bekommen die Option **Phased Updates sofort installieren** (`APT::Get::Always-Include-Phased-Updates=true`). Ohne sie überspringt apt diese Pakete – das sah aus, als würde dist-upgrade nicht alles installieren. Migration 0076.

## [0.19.0] - 2026-11-03

### Neu
- Datei-Integrität **Who-Data**: Mit dem Richtlinienschalter „Verursacher von Dateiänderungen ermitteln“ pflegt der Agent unter Linux auditd-Watch-Regeln (`-w <Pfad> -p wa -k roster_fim`, gelesen per `ausearch --checkpoint`) und setzt unter Windows Erfolgs-SACLs auf die überwachten Pfade und liest die Ereignisse 4663/4660. Änderungen tragen dann Benutzer und Prozess in der Ereignisliste (Spalte „Wer“) und in Benachrichtigungen. Migration 0075 ergänzt auch die Richtlinienschalter für Systemzustand und Rootkit-Prüfung.

### Neu
- Compliance-Katalog von 48 auf 106 Regeln in Richtung CIS Level 1 ausgebaut: Linux (SSH-Leerlauf/Grace/leere Passwörter/rhosts/Sessions/LogLevel, pam_pwquality und pam_faillock, Passwortalterung und umask, Cron-Rechte und cron.allow, Logdatei-Rechte, Sticky-Bit, Mount-Optionen von /tmp und /dev/shm, Home-Rechte, sysctl rp_filter/log_martians/Redirects/Source-Route/ICMP-Broadcasts/kptr_restrict, persistentes journald, unnötige Dateisystemmodule) und Windows Server (Kennwortchronik/-alter, Sperrdauer, anonyme Aufzählung, SMB-Signierung, nur NTLMv2, RDP-Leerlauf, Bildschirmsperre, Firewall-Protokoll, Remoteregistrierung, Druckwarteschlange, WinRM Basic/unverschlüsselt, NetBIOS, Defender Cloud/PUA, UAC sicherer Desktop, SEHOP, DEP, PowerShell-Skriptblock-Protokollierung, Überwachungsrichtlinien für Anmeldung/Kontenverwaltung/Prozesserstellung/Richtlinienänderung, Protokollgrößen, zwischengespeicherte Anmeldungen, Autologon) – PowerShell-3-tauglich (`net accounts`, WMI, `auditpol` per GUID, `bcdedit`). Jede Regel trägt **Framework-Referenzen** (CIS, PCI DSS, ISO 27001) als Badges mit Rahmenwerk-Filter in Geräte-, Flotten- und Richtlinienansicht, dazu englische Beschreibungen und Empfehlungen.

### Neu
- **Rootkit- und Malware-Prüfung** (Richtlinienschalter): Einmal täglich sucht der Linux-Agent nach versteckten Prozessen (antworten auf `kill(pid,0)`, fehlen aber in `/proc`), einem gefüllten `/etc/ld.so.preload`, versteckten Kernelmodulen, Schnittstellen im Promiscuous-Modus, ausführbaren/setuid-/ELF-Dateien in `/tmp`, `/var/tmp`, `/dev/shm`, regulären Dateien in `/dev` und bekannten Rootkit-Pfaden (Reptile, Diamorphine, Jynx, BEURK). Befunde werden zum kritischen Vorfall **Rootkit-Hinweis**. Laut Datei-Integritätsprüfung neue oder geänderte Dateien werden mit ClamAV (`clamdscan`/`clamscan`, Linux) bzw. Defender (`MpCmdRun`, Windows) geprüft, falls vorhanden → kritischer Vorfall **Malware**. Knopf „Rootkit-Prüfung jetzt“ im Geräte-Tab Datei-Integrität.

### Neu
- **Systemzustand überwachen** (Richtlinienschalter): Der Agent vergleicht stündlich lauschende Ports, Kernelmodule, Cron-Einträge, UID-0- und Administratorkonten, aktivierte systemd-Units/Timer (Linux), Autostart-Dienste und geplante Aufgaben (Windows, über `net localgroup`/WMI/`schtasks`, Server-2012-tauglich) sowie launchd-Jobs (macOS) mit seiner Baseline. Jede Änderung wird zum Vorfall **Systemänderung** (kritisch bei neuem UID-0- oder Administratorkonto), aufgelistet als „+ neu, - weg“.

### Neu
- Datei-Integritätsprüfung umfasst die **Windows-Registry**: Regelpfade wie `HKLM\SOFTWARE\…`, `HKU\*\…` (alle geladenen Benutzerprofile; `HKCU` wird darauf abgebildet) oder `HKCR\…`. Schlüssel zählen wie Verzeichnisse, Werte wie Dateien (Hash über Typ und Daten), neue oder entfernte Unterschlüssel werden gemeldet (z. B. ein neuer Dienst). Die Richtlinie bekommt einen Knopf mit 18 Registry-Standardpfaden: Run/RunOnce (Maschine, WOW6432Node, je Benutzer), Winlogon, Image File Execution Options, AppInit_DLLs, Dienste, LSA, Session Manager, Browser Helper Objects, Aufgaben-Cache, UAC-Richtlinien, Terminal Server, Defender-Ausnahmen.

### Neu
- Log-Quellen: neue Formate **HAProxy** (HTTP-Log), **Traefik** (JSON oder Common Log), **MySQL/MariaDB**-Fehlerprotokoll (Access denied, Host not allowed) und **Firewall-Drops** (iptables/nft/ufw-Kernel-Log, Windows `pfirewall.log`), dazu `apache_access` als Alias des Combined Log. Neue Quelltypen **systemd-Journal** (Ziel `kernel` oder eine SYSLOG_IDENTIFIER-Kennung – nft-/iptables-Protokolle auf Hosts ohne rsyslog) und **Docker-Ereignisse** (`exec` im Container, Start/Stop/Die). Vorlagen für alle in der Richtlinie.
- Neue Vorfälle **Portscan** (≥ N verschiedene Zielports einer IP im Brute-Force-Fenster, Standard 10, Einstellungen → Sicherheitsüberwachung; automatische Sperre wie bei Brute-Force) und **Befehl im Container** (`docker exec`).

### Neu
- Windows-Anmeldeüberwachung liest neben dem Sicherheitsprotokoll jetzt auch **System** (7045 Dienst installiert) und **PowerShell/Operational** (4104 Skriptblock), mit eigenem Cursor je Protokoll, und wertet 4698/4702 (geplante Aufgabe angelegt/geändert), 1102 (Sicherheitsprotokoll gelöscht) und 4688 (Prozessstart – nur verdächtige Muster: certutil-/bitsadmin-Downloads, kodiertes oder verstecktes PowerShell, mimikatz/procdump/psexec, vssadmin delete shadows, wevtutil cl, schtasks /create, net user, Office startet Shell, …) aus. Neue Vorfallsarten **Persistenz** (Warnung), **Protokoll gelöscht** (kritisch) und **Verdächtiger Prozess** (Warnung). Voraussetzungen: Überwachungsrichtlinie „Prozesserstellung“ mit Befehlszeile, „Andere Objektzugriffsereignisse“, Skriptblock-Protokollierung (fehlende Protokolle werden übersprungen).

### Neu
- **Bedrohungsdaten**: Der Server lädt täglich fünf öffentliche Blocklisten (Spamhaus DROP, blocklist.de, Feodo Tracker, CINS Army, Tor-Exit-Nodes; in der Datenbank zwischengespeichert, Feeds einzeln wählbar). Quell-IPs von Vorfällen, Anmeldungen und Sperren werden mit dem Listennamen markiert, beide Listen lassen sich auf gelistete IPs filtern. Eine gelistete IP erzeugt beim ersten Fehlversuch, Scan-Treffer oder Firewall-Drop den Vorfall **Bekannte Angriffsquelle** und wird sofort flottenweit gesperrt – unabhängig von Schwellwerten und vom allgemeinen Flottensperren-Schalter (Einstellungen → Sicherheitsüberwachung; Nie-sperren-Liste und private Adressen bleiben ausgenommen). Neuer Tab Sicherheit → **Bedrohungsdaten** mit Feed-Status, Aktualisieren und IP-Nachschlagen; Migration 0074.

### Neu
- Sicherheit → Vorfälle: Jeder Vorfall trägt seine **MITRE-ATT&CK-Techniken** (z. B. T1110 Brute Force, T1070.001 Clear Windows Event Logs) als Links in die ATT&CK-Matrix, die Liste lässt sich nach Technik filtern. Der Katalog der Vorfallsarten (`GET /security/kinds`) ist jetzt die eine Quelle für Server, Richtlinien und Oberfläche.
- Einstellungen → Sicherheitsüberwachung: **Schweregrad je Vorfallsart** überschreibt den Standard (Info unterdrückt die Benachrichtigung, die automatische Reaktion läuft trotzdem).

## [0.18.19] - 2026-11-03

### Geändert
- Agent: Ergebnisse von `block_ip`/`unblock_ip` werden nur noch mit dem nächsten regulären Checkin gemeldet (kein zusätzlicher Checkin mehr). Die Minuten-Bündelung aus 0.18.18 feuerte trotzdem jede Minute je Gerät, weil mit Flottensperre etwa alle 30 s ein Sperrbefehl ankommt.

## [0.18.18] - 2026-11-03

### Geändert
- Agent: Ergebnisse von `block_ip`/`unblock_ip` werden gebündelt gemeldet, höchstens ein Checkin je Minute statt sofort nach jedem Befehl. Mit Flottensperre bekam jedes Gerät etwa jede Minute einen Sperrbefehl und checkte alle 30 s ein (das Sechsfache der regulären Rate). Interaktive Befehle melden weiter sofort.

## [0.18.17] - 2026-11-03

### Behoben
- Geräteliste (alle 15 s von Dashboard und Geräteseite geladen) war je Gerät teuer: Die Unterabfrage „letztes Ergebnis je Task“ lief quadratisch über die Task-Ergebnisse des Geräts, die Zählung offener Vorfälle las Zeilen statt eines Index. Migration 0073 ergänzt `task_results(device_id, task_id, ran_at)` und `security_incidents(device_id, status)`.

### Neu
- Regressionstest für den Windows-Agent: schlägt fehl, wenn die eingebettete EXE nicht mehr auf Windows Server 2012 starten würde (PE-Mindestversion über 6.1 oder statische Importe von Windows-10-APIs).

## [0.18.16] - 2026-11-03

### Behoben
- Dashboard brauchte Sekunden und trieb die CPU hoch: Sicherheitsübersicht und Länder-Widgets verknüpften die großen Ereignis-/Sperrtabellen mit der Gerätetabelle, und sobald SQLite Planer-Statistiken hatte, ging es je Gerät durch (11 s für die Zählung gesperrter IPs, 1,5–2,3 s je Anmelde-Abfrage). Diese Abfragen lesen jetzt nur noch abdeckende Zeitindizes ohne Join (neuer Index `auth_events(ts, kind, source_ip, device_id)`, Migration 0072) und filtern die Geräte in Go.
- SQLite lief über eine einzige Verbindung, jede Oberflächen-Anfrage wartete hinter jedem Checkin-Schreibzugriff und umgekehrt. Der Server nutzt jetzt einen kleinen Pool (WAL-Leser laufen parallel; Transaktionen starten mit `BEGIN IMMEDIATE`, Busy-Timeout 15 s, `synchronous=NORMAL`).
- Erledigte `block_ip`-Befehle werden nach zwei Tagen gelöscht, unabhängig von der Ergebnis-Aufbewahrung – mit Flottensperre waren das 155 MB Massenzeilen.

### Neu
- Anfragen über 5 s werden mit Pfad, Status und Dauer protokolliert (Long-Polls ausgenommen).
- `ROSTER_PPROF_ADDR` (z. B. `127.0.0.1:6060`) schaltet die pprof-Endpunkte von Go zur Diagnose frei.

## [0.18.15] - 2026-11-03

### Behoben
- Die Flottensperre weckte für jede kopierte Sperre sofort alle Geräte, sodass bei tausenden Vorfällen am Tag die ganze Flotte etwa alle 20 s eincheckte (das Zwölffache, ~1.500 Checkins je 10 Minuten) und jeder Checkin die tausenden Sperrzeilen des Geräts nach verfallenen Befehlen durchsuchte. Die Kopien kommen jetzt mit dem nächsten regulären Checkin an, und die Verfallsprüfung geht über die wenigen offenen Befehle des Geräts (dazu Index `ip_blocks(status, device_id)`, Migration 0071).

## [0.18.14] - 2026-11-03

### Behoben
- Dashboard langsam: Jedes Widget berechnete bei jedem Abruf alle aktuellen Kennzahlen neu (alle Geräte, Check-Status, Länder gesperrter IPs und Angriffsquellen – zwei Durchläufe über die Sperrtabelle mit 380.000 Zeilen). Die Kennzahlen werden jetzt 30 s je Sichtbarkeitsbereich zwischengespeichert, die Zählung gesperrter IPs nutzt den Zeitstempel-Index statt die ganze Tabelle zu lesen (2,6 s → 0,3 s), und die Planer-Statistik von SQLite wird mit jedem Aufräumlauf aufgefrischt (`PRAGMA optimize`).

## [0.18.13] - 2026-11-03

### Behoben
- Server-CPU bei 100 % eines Kerns und weiterhin Checkin-Timeouts: Jede Befehlsrückmeldung suchte ihren Vorfall mit einem vollen Durchlauf von `security_incidents` (mit Flottensperre mehrmals pro Sekunde), und das Dashboard las alle 15 s die komplette `ip_blocks`-Tabelle für die Zählung gesperrter IPs. Migration 0070 ergänzt Indizes auf `security_incidents(response_command_id)` und `ip_blocks(created_at, ip, device_id)`; die Sicherheitsübersicht wird 60 s zwischengespeichert.

## [0.18.12] - 2026-11-03

### Behoben
- Agent: Ein hängender Ad-hoc-Befehl blockiert die Checkin-Schleife nicht mehr (das Gerät galt dann als offline, obwohl der Viewer noch ging). Synchrone Befehle sind auf 10 Minuten begrenzt, **Updates suchen** läuft im Hintergrund mit 30-Minuten-Obergrenze – die Windows-Update-Suche auf einem alten Windows Server 2012 ohne aktuelle Wartungsstack-Updates kann Stunden dauern.
- Server: Sperrbefehle, die ein Gerät erst nach Ablauf der Sperre abholen könnte (Gerät war offline), werden beim Checkin verworfen statt einzeln abgearbeitet.

### Neu
- Windows-Agent-Dienst schreibt sein Protokoll nach `C:\ProgramData\Roster\agent.log` (Rotation bei 5 MB), weil der Service Control Manager stderr verwirft.

## [0.18.11] - 2026-11-02

### Behoben
- Active Response bremst die Agent-Checkins nicht mehr aus: Bei eingeschalteter Flottensperre wurde jede Brute-Force-Sperre auf ~65 Geräten einzeln je Transaktion eingereiht, mitten im Checkin, mit zwei Suchen ohne Index je Gerät. Bei mehreren tausend Vorfällen am Tag liefen die Checkins in den 30-s-Timeout (`context deadline exceeded` beim Speichern von Check-Ergebnissen, Richtlinie und Befehlen). Reaktionen laufen jetzt in einem seriellen Hintergrund-Worker, die Flottensperre ist eine einzige Transaktion, und `ip_blocks` bekommt einen Index auf die Adresse (Migration 0069).

### Geändert
- Aufbewahrung der Massen-Tabellen der Sicherheitsüberwachung: rohe Anmelde-/Log-Ereignisse 30 Tage (vorher 90), beendete IP-Sperren 14 Tage (vorher 90). Vorfälle bleiben weiter ein Jahr. Mit Web-Scannern kommen über 100.000 rohe Ereignisse am Tag zusammen, und die Flottensperre legt je Adresse einen Sperreintrag pro Gerät an.

## [0.18.10] - 2026-11-02

### Behoben
- Windows-Agent auf **Server 2012/2012 R2, Windows 7/8.1**: Die EXE wurde als „keine gültige Win32-Anwendung“ abgelehnt, weil der Go-Linker Windows 10 als Mindestversion in den PE-Kopf schreibt. Der Build senkt den Wert jetzt auf 6.1 (`tools/pepatch`, auch für Tray, Viewer und das Windows-Server-Binary); die Laufzeit selbst braucht nur Windows-7-APIs. Unter Wine mit gemeldetem Windows 6.2.9200 geprüft.
- Windows-Installationsbefehl: erzwingt TLS 1.2 und nutzt `-UseBasicParsing`, damit er mit PowerShell 3/4 auf älteren Servern funktioniert (keine Internet-Explorer-Engine, TLS 1.0 als Standard).

## [0.18.9] - 2026-11-02

### Neu
- Log-Quellen: **SQL-Injektion** in Pfad oder Query zählt als verdächtige Anfrage – `union select`, `' or 1=1`, `sleep(`, `benchmark(`, `waitfor delay`, `information_schema`, `load_file(`, `into outfile`, `extractvalue(`, `admin'--` u. a.; geprüft wird zusätzlich die URL-dekodierte Form (`%27%20or%201%3d1`, `union/**/select`). POST-Bodies stehen nicht im Zugriffsprotokoll und bleiben unsichtbar; dafür braucht es eine WAF (ModSecurity-Meldungen werden im Web-Fehlerprotokoll erkannt) oder ein eigenes Anwendungsprotokoll als Log-Quelle „eigenes Muster“.

### Geändert
- Aufräumen: beendete IP-Sperren (aufgehoben, abgelaufen, fehlgeschlagen) werden nach 90 Tagen gelöscht; bisher wuchs die Tabelle unbegrenzt (Flotten-Sperre erzeugt je Vorfall einen Eintrag pro Gerät).

## [0.18.8] - 2026-11-01

### Neu
- Sicherheit → Vorfälle: **Filter nach Land** der Quell-IP (Auswahl mit Flagge, „Unbekannt“ für Adressen ohne Zuordnung); Klick auf ein Land in der Kachel *Angriffsquellen nach Land* setzt den Filter. API: `GET /security/incidents?country=DE` (`?` = unbekannt).

## [0.18.7] - 2026-11-01

### Geändert
- Gerät → Sicherheit: eigener Tab **Vorfälle** (Sicherheitsvorfälle, aktive IP-Sperren, Zähler offener Vorfälle); **Anmeldungen** zeigt nur noch die Überwachungskarte und die Anmeldeereignisse – wie auf der Flotten-Seite getrennt.
- Gerät → Anmeldungen: Spalte heißt jetzt „Ereignisse seit Agent-Start“ (mit Erklärung), da der Zähler nur neue Zeilen zählt, „Testen“ aber rückwirkend liest.

## [0.18.6] - 2026-11-01

### Geändert
- Dashboard: Länder-Kennzahlen zeigen als Balken bis zu 40 Länder absteigend sortiert (statt 12 + „Andere“); der Rest heißt „Andere (n Länder)“.

## [0.18.5] - 2026-11-01

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

[Unreleased]: https://github.com/boonkerz/roster/compare/v0.21.4...HEAD
[0.21.4]: https://github.com/boonkerz/roster/compare/v0.21.3...v0.21.4
[0.21.3]: https://github.com/boonkerz/roster/compare/v0.21.2...v0.21.3
[0.21.2]: https://github.com/boonkerz/roster/compare/v0.21.1...v0.21.2
[0.21.1]: https://github.com/boonkerz/roster/compare/v0.21.0...v0.21.1
[0.21.0]: https://github.com/boonkerz/roster/compare/v0.20.2...v0.21.0
[0.20.2]: https://github.com/boonkerz/roster/compare/v0.20.1...v0.20.2
[0.20.1]: https://github.com/boonkerz/roster/compare/v0.20.0...v0.20.1
[0.20.0]: https://github.com/boonkerz/roster/compare/v0.19.1...v0.20.0
[0.19.1]: https://github.com/boonkerz/roster/compare/v0.19.0...v0.19.1
[0.19.0]: https://github.com/boonkerz/roster/compare/v0.18.19...v0.19.0
[0.18.19]: https://github.com/boonkerz/roster/compare/v0.18.18...v0.18.19
[0.18.18]: https://github.com/boonkerz/roster/compare/v0.18.17...v0.18.18
[0.18.17]: https://github.com/boonkerz/roster/compare/v0.18.16...v0.18.17
[0.18.16]: https://github.com/boonkerz/roster/compare/v0.18.15...v0.18.16
[0.18.15]: https://github.com/boonkerz/roster/compare/v0.18.14...v0.18.15
[0.18.14]: https://github.com/boonkerz/roster/compare/v0.18.13...v0.18.14
[0.18.13]: https://github.com/boonkerz/roster/compare/v0.18.12...v0.18.13
[0.18.12]: https://github.com/boonkerz/roster/compare/v0.18.11...v0.18.12
[0.18.11]: https://github.com/boonkerz/roster/compare/v0.18.10...v0.18.11
[0.18.10]: https://github.com/boonkerz/roster/compare/v0.18.9...v0.18.10
[0.18.9]: https://github.com/boonkerz/roster/compare/v0.18.8...v0.18.9
[0.18.8]: https://github.com/boonkerz/roster/compare/v0.18.7...v0.18.8
[0.18.7]: https://github.com/boonkerz/roster/compare/v0.18.6...v0.18.7
[0.18.6]: https://github.com/boonkerz/roster/compare/v0.18.5...v0.18.6
[0.18.5]: https://github.com/boonkerz/roster/compare/v0.18.4...v0.18.5
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
