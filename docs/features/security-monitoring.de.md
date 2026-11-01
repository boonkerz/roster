# Sicherheitsüberwachung

Roster ergänzt hostbasierte Sicherheitsüberwachung im Stil von Wazuh: Datei-Integrität,
Anmeldungs-Überwachung mit Vorfallserkennung, CIS-artige Compliance-Prüfung und automatische
Reaktionen. Konfiguriert wird alles im Bereich **Sicherheitsüberwachung** einer Richtlinie,
angezeigt auf der Seite **Sicherheit** und in der Tab-Gruppe „Sicherheit“ des Geräts.

!!! note "Voraussetzungen"
    Der Agent muss als **root** (Linux/macOS) bzw. **LocalSystem** (Windows) laufen – die
    Installationsskripte tun das; eine alte systemd-Unit mit `DynamicUser=yes` kann weder
    `/var/log/auth.log` und `/etc/shadow` lesen noch nftables bedienen. Die Anmeldungs-
    Überwachung unter Windows braucht aktivierte Anmeldeüberwachung (Erfolg/Fehler), auf Servern
    der Standard.

![Richtlinien-Bereich „Sicherheitsüberwachung“](../screenshots/policy-security.png){ .shadow }

## Datei-Integrität (FIM)

![Datei-Integritätsereignisse](../screenshots/security-fim.png){ .shadow }

Einer Richtlinie **überwachte Pfade** hinzufügen: Datei, Verzeichnis (optional rekursiv) oder
Glob wie `/home/*/.ssh/authorized_keys`, mit Ausschluss-Globs (`*.log`, `cache/`), optionaler
Plattform-Einschränkung und Alarm-Schalter. *Standardpfade einfügen* ergänzt die üblichen
Verdächtigen je Plattform (`/etc`, `/usr/local/bin`, `~/.ssh`, cron-Spools; Windows
`drivers\etc`, geplante Aufgaben, Autostart-Ordner; macOS LaunchDaemons/LaunchAgents).

Der Agent hält eine Baseline (SHA-256, Rechte, Besitzer, Änderungszeit) in seinem Zustands-
verzeichnis (`/var/lib/roster/fim-baseline.json`, `C:\ProgramData\Roster\fim-baseline.json`).
Der erste Lauf legt nur die Baseline an; danach meldet jeder Scan (Intervall je Richtlinie,
Standard 60 min) **neue**, **geänderte**, **gelöschte** Dateien und **Rechte-/Besitzerwechsel**.
Änderungen an Regeln mit Alarm-Schalter werden je Checkin gebündelt gemeldet und erzeugen einen
Vorfall `fim_change`. Dateien über 256 MB werden nur nach Größe und Änderungszeit verglichen; ein
Scan ist auf 50 000 Einträge begrenzt.

Geräte-Tab **Datei-Integrität**: Ereignisliste mit Bestätigen, *Jetzt prüfen* und *Baseline neu
setzen*. Sicherheitsseite, Tab **Datei-Integrität**: flottenweite Liste mit Filtern.

## Anmeldungs-Überwachung & Vorfälle

![Sicherheitsvorfälle](../screenshots/security-incidents.png){ .shadow }

Mit **Anmeldungen überwachen** liest der Agent neue Ereignisse inkrementell:

| Plattform | Quelle | Ereignisse |
|---|---|---|
| Linux | journald (Cursor) oder `/var/log/auth.log` / `secure` (Offset) | sshd fehlgeschlagen/akzeptiert/unbekannter Benutzer, sudo, su, useradd/userdel/usermod/gpasswd/passwd, Konsolen-Login |
| Windows | Sicherheitsprotokoll (RecordId-Cursor) | 4624/4625 (Anmeldetypen 2/3/7/10/11), 4740 Sperre, 4720/4726 Benutzer angelegt/gelöscht, 4722/4725/4767, 4728/4732/4756 Gruppenmitgliedschaft, 4723/4724 Passwort |
| macOS | Unified Log (`log show`) | sshd, sudo, su, login |

### Log-Quellen: nginx, SFTP-/FTP-Container, eigene Muster

Neben dem Anmeldeprotokoll des Systems kann eine Richtlinie **Log-Quellen** enthalten, die der
Agent jede Minute inkrementell liest: eine **Datei** (Glob erlaubt, z. B.
`/var/log/nginx/*access.log`) oder die Ausgabe eines **Docker-Containers** (exakter Name oder
Muster wie `*sftp*`; der Agent liest das json-file-Protokoll direkt, sonst `docker logs --since`).
Jede Quelle hat ein Format:

| Format | Erzeugt |
|---|---|
| Web-Zugriffsprotokoll (nginx, Apache, Traefik – Combined Log, auch mit vorangestelltem `$host`; nginx-proxy-Ausgabe mit forego-Präfix wird ausgepackt) | `401` → Fehlversuch; Scanner-Pfade (`wp-login.php`, `/.env`, `phpmyadmin`, `xmlrpc.php`, `../`, `union select`, `cmd=`, `${jndi`, `/proc/self` …), versteckte Dateien (`/.git`, `/.terraform`), Zugangsdaten-/Konfigurationsdateien, `400`/`444` und andere kaputte Anfragen, `404` oder Reverse-Proxy-`503` auf `.php/.sql/.bak/.zip/.tfvars/.pem …`, fehlgeschlagene Anfragen an die nackte Server-IP statt des Domainnamens → **verdächtige Anfrage** |
| Web-Fehlerprotokoll (nginx, Apache) | `password mismatch`, `authentication failure`, `user … not found` → Fehlversuch; `access forbidden by rule`, `client denied`, `Invalid URI`, ModSecurity, Rate-Limit, fehlende Scanner-Dateien → verdächtige Anfrage |
| sshd (SSH-/SFTP-Server oder -Container) | dieselben Ereignisse wie der sshd des Hosts: fehlgeschlagene/akzeptierte Anmeldungen, unbekannte Benutzer |
| FTP (vsftpd, proftpd, pure-ftpd) | fehlgeschlagene/erfolgreiche Anmeldungen, unbekannte Benutzer |
| Mail (Postfix SASL, Dovecot, Exim) | fehlgeschlagene/erfolgreiche IMAP-/POP3-/SMTP-Anmeldungen |
| eigenes Muster | regulärer Ausdruck mit `(?P<ip>…)` und optional `(?P<user>…)`; jeder Treffer zählt als Fehlversuch oder verdächtige Anfrage |

Die Ereignisse tragen den Namen der Quelle als Dienst (z. B. `web`, `sftp`, `mail`), sodass der
Tab **Anmeldungen**, die Vorfallstexte und die Sperren zeigen, wo ein Angreifer aufgefallen ist.
Vorlagen füllen das Formular für die üblichen Fälle: Webserver-Zugriffs-/Fehlerprotokoll als
Datei oder im Docker-Container, SSH-/SFTP-Container, FTP-Server, Mailserver, eigenes Muster.

**Testen** (neben jeder Quelle und im Formular, auch vor dem Speichern) lässt den Agent eines
wählbaren Geräts die letzten Zeilen der Quelle lesen und mit dem Format parsen: gefundene
Dateien bzw. Container, gelesene Zeilen, Treffer und eine Stichprobe der erkannten Ereignisse.
Passt kein Container, zeigt das Ergebnis die Namen der laufenden Container. Der Lesezeiger des
Agents bleibt dabei unberührt, Treffer werden nicht als Ereignisse gespeichert.

Ob eine Quelle auf einem Gerät tatsächlich gelesen wird, steht am Gerät im Tab **Anmeldungen**
unter **Überwachung auf diesem Gerät**: welche Richtlinie sie beisteuert, wie viele Dateien
oder Container der Agent gefunden hat, wie viele Ereignisse seit Agent-Start kamen, wann der
letzte Treffer war und ein etwaiger Fehler (fehlende Datei, kein passender Container, fehlende
Rechte). Dort steht auch, ob Anmeldungs-Überwachung, Datei-Integrität und Compliance wirken.

Daraus leitet der Server **Vorfälle** ab:

- **Brute-Force** – N Fehlversuche von einer Quell-IP im Zeitfenster (Standard 10 in 10 min)
  **oder** N Versuche mit nicht existierenden Benutzernamen innerhalb von 24 Stunden (Standard 5,
  0 = aus). Die zweite Regel fängt verteilte, langsame Angriffe, die den Schwellwert je Fenster von
  einer IP aus nie erreichen.
- **Angriffsmuster** – N verdächtige Anfragen von einer Quell-IP im Zeitfenster (Standard 20),
  z. B. ein Web-Scanner, der WordPress- und phpMyAdmin-Pfade auf nginx durchprobiert.
- **Anmeldung nach Brute-Force** – erfolgreiche Anmeldung von einer IP, die gerade Brute-Force
  gefahren hat (kritisch).
- **Neue Quell-IP** – erste erfolgreiche Anmeldung eines Benutzers von unbekannter Adresse, nach
  einer Lernphase (Standard 7 Tage Überwachung); private Adressen lassen sich ausnehmen.
- **Kontoänderung** – Benutzer angelegt/gelöscht, Gruppenmitgliedschaft geändert (Admin-Gruppen wie
  sudo/wheel/Administratoren als Warnung), Passwort geändert.
- **Kontosperre** – Konto gesperrt.

Warnungen und kritische Vorfälle laufen über die normalen Alarmkanäle (Wartungsfenster und
Mindest-Schweregrad je Kanal gelten). Welche Arten gemeldet werden, legt die Richtlinie im Tab
**Benachrichtigungen** fest (*Sicherheitsvorfälle melden*: eine Checkbox je Art); *nicht melden,
wenn die Quell-IP automatisch gesperrt wurde* hält die Kanäle ruhig, wenn Active Response oder
die automatische Sperre den Angreifer schon erledigt hat. Auf der Seite Sicherheit werden die
Vorfälle unabhängig davon immer gespeichert. Der Tab **Vorfälle** listet sie mit Bestätigen/Erledigen,
Filtern, den häufigsten Quell-IPs, den **Herkunftsländern** der Fehlversuche und verdächtigen Anfragen und den Ländern
der zuletzt gesperrten IPs; **Anmeldungen** zeigt die Rohereignisse. Quell-IPs tragen überall
eine Länderflagge. Die Zuordnung kommt aus einer in den Server eingebetteten
IP-zu-Land-Datenbank ([ip-location-db](https://github.com/sapics/ip-location-db),
Public Domain) – ohne externen Dienst; sie wird mit jedem Release erneuert. Für die Übersicht
gibt es die Kennzahlen **Gesperrte IPs nach Land (30 Tage)** und **Angriffsquellen nach Land
(7 Tage)** als Balken-, Ring- oder Verlaufsdiagramm (siehe [Dashboard](dashboard.md)). Schwellwerte,
Lernphase und Nie-sperren-Liste stehen unter Einstellungen → **Sicherheitsüberwachung**.

## Compliance-Prüfung (Baseline)

![Compliance-Werte über die Flotte](../screenshots/security-compliance.png){ .shadow }

**Compliance** in einer Richtlinie aktivieren – mit Profil (*Server* oder *Arbeitsplatz*),
Intervall (Standard 24 h) und optionaler Regelauswahl. Der Agent wertet einen CIS-artigen Katalog
aus und meldet bestanden/fehlgeschlagen/n/a samt Befund; der **Score** ist bestanden / (bestanden
+ fehlgeschlagen) · 100.

Regelfamilien: Linux – SSH-Härtung (Root-Login, Passwort-Anmeldung, MaxAuthTries, X11, schwache
Chiffren), Rechte von `/etc/shadow` und `/etc/passwd`, leere Passwörter, weitere UID 0, sudo
`NOPASSWD: ALL`, Passwortalter, aktive Firewall, auditd, NTP, SELinux/AppArmor, ASLR, IP-Forwarding,
ICMP-Redirects, SYN-Cookies, weltweit beschreibbare Dateien in `/etc`, telnet/rsh-Dienste, Core-
Dumps. Windows – UAC, SMBv1, RDP-NLA / RDP aus, Gast- und Administratorkonto, Firewall-Profile,
Defender-Echtzeitschutz, BitLocker-Systemlaufwerk, AutoPlay, LLMNR, Kennwortlänge, Sperrschwelle,
PowerShell 2.0, WDigest, LSA-Schutz. macOS – FileVault, Firewall und Tarnmodus, Gatekeeper, SIP,
Bildschirmschoner-Passwort, Gast, Remote Login. Alle Plattformen – automatische Updates aktiv, kein
Neustart ausstehend. Jede Regel bringt eine Empfehlung mit.

Ergebnisse stehen im Geräte-Tab **Compliance** und auf der Sicherheitsseite (nach Gerät und nach
Regel, mit den fehlgeschlagenen Geräten je Regel). Der Check-Typ **Compliance-Score** (Mindest-
Score, optional Fehler bei jeder Regel mit Schwere „hoch“) hängt das Ergebnis an Alarme und
Self-Healing.

## Active Response

Eine Richtlinie kann automatisch auf Vorfälle reagieren:

| Auslöser | Aktionen |
|---|---|
| Brute-Force, Anmeldung nach Brute-Force, neue Quell-IP, Angriffsmuster | Quell-IP sperren, Skript ausführen |
| Anmeldung nach Brute-Force, Kontoänderung, Kontosperre, neue Quell-IP | Konto sperren, Skript ausführen |
| Datei-Änderung | Skript ausführen |

- **IP sperren** – Linux: nftables-Sets `inet roster blocked4/6` (Einzeladressen) und `net4/6`
  (Netze) mit Timeout, verworfen im **input- und forward-Hook**, damit auch Verkehr zu
  Docker-Containern mit veröffentlichten Ports gesperrt ist (iptables-Fallback: `INPUT`, `FORWARD`
  und `DOCKER-USER`, Ablauf im Agent); Windows: eingehende Firewall-Regel `Roster-Block <ip>`;
  macOS: pf-Tabelle `roster_block` (`table <roster_block> persist` und
  `block in quick from <roster_block>` in `/etc/pf.conf` eintragen). Dauer in Minuten, 0 = dauerhaft.
  Ziel kann eine einzelne IP oder ein Netz in CIDR-Schreibweise sein.
- **Konto sperren** – `usermod -L`, `Disable-LocalUser`, `pwpolicy -disableuser`. root und
  Administrator werden nie gesperrt.
- **Skript ausführen** – ein Bibliotheks-Skript mit `ROSTER_TRIGGER`, `ROSTER_SOURCE_IP`,
  `ROSTER_USER`, `ROSTER_DEVICE`, `ROSTER_INCIDENT` und `ROSTER_TITLE` in der Umgebung.

### Automatische Sperre ohne Richtlinien-Regel

Bei vielen Servern sind die Regeln je Richtlinie optional: Einstellungen →
**Sicherheitsüberwachung** → *Automatische Sperre* sperrt die Quell-IP jedes Brute-Force-,
Anmeldung-nach-Brute-Force- und Angriffsmuster-Vorfalls auf dem meldenden Gerät, mit
einstellbarer Dauer (eine Stunde bis dauerhaft). Eine eigene Active-Response-Regel der Richtlinie
für denselben Auslöser hat Vorrang. Zwei Optionen erweitern die Wirkung:

- **Flotten-Sperre** – die Adresse wird auf allen Geräten gesperrt, deren Richtlinie Anmeldungen
  überwacht oder Log-Quellen liest, nicht nur dort, wo sie aufgefallen ist. Geräte, die sie schon
  sperren, werden übersprungen.
- **Netzbereichs-Sperre** – sind auf dem Gerät innerhalb von 24 Stunden mindestens N verschiedene
  auffällige IPs aus demselben IPv4-`/24` aufgetaucht (Standard aus), wird statt der einzelnen
  Adresse das ganze Netz gesperrt. Ein Netz, das eine Nie-sperren-Adresse enthält, wird nie als
  Ganzes gesperrt.

Im Tab **Vorfälle** lassen sich außerdem viele Vorfälle auf einmal auswählen und alle Quell-IPs
mit einem Klick sperren (Dauer und Flotten-Option wie oben); die Vorfälle werden dabei bestätigt.

Schranken: die **Nie-sperren-Liste** (IPs/CIDRs) und ein **Cooldown** je Gerät und Aktion unter
Einstellungen → Sicherheitsüberwachung; Loopback und die Adresse des Roster-Servers sind immer
geschützt. Jede Reaktion landet im Audit-Log und wird mit Ergebnis am Vorfall angezeigt. Der Tab
**Sperren** listet alle Sperren (Status, Ablauf, Aufheben) und erlaubt das manuelle Sperren
einer IP oder eines Netzes (Gerät, IP oder CIDR, Dauer von einer Stunde bis dauerhaft, Grund,
wahlweise auf allen überwachten Geräten); Vorfälle bieten *IP sperren*
mit derselben Dauer-Auswahl und *Konto sperren*. Eine bereits gesperrte IP wird nicht doppelt
angelegt, sondern mit der neuen Dauer erneuert – so lässt sich eine befristete Sperre an Ort
und Stelle dauerhaft machen.
