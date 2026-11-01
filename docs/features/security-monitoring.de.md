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

Daraus leitet der Server **Vorfälle** ab:

- **Brute-Force** – N Fehlversuche von einer Quell-IP im Zeitfenster (Standard 10 in 10 min).
- **Anmeldung nach Brute-Force** – erfolgreiche Anmeldung von einer IP, die gerade Brute-Force
  gefahren hat (kritisch).
- **Neue Quell-IP** – erste erfolgreiche Anmeldung eines Benutzers von unbekannter Adresse, nach
  einer Lernphase (Standard 7 Tage Überwachung); private Adressen lassen sich ausnehmen.
- **Kontoänderung** – Benutzer angelegt/gelöscht, Gruppenmitgliedschaft geändert (Admin-Gruppen wie
  sudo/wheel/Administratoren als Warnung), Passwort geändert.
- **Kontosperre** – Konto gesperrt.

Warnungen und kritische Vorfälle laufen über die normalen Alarmkanäle (Wartungsfenster und
Mindest-Schweregrad je Kanal gelten). Der Tab **Vorfälle** listet sie mit Bestätigen/Erledigen,
Filtern und den häufigsten Quell-IPs; **Anmeldungen** zeigt die Rohereignisse. Schwellwerte,
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
| Brute-Force, Anmeldung nach Brute-Force, neue Quell-IP | Quell-IP sperren, Skript ausführen |
| Anmeldung nach Brute-Force, Kontoänderung, Kontosperre, neue Quell-IP | Konto sperren, Skript ausführen |
| Datei-Änderung | Skript ausführen |

- **IP sperren** – Linux: nftables-Set `inet roster blocked4/6` mit Timeout (iptables-Fallback mit
  Ablauf im Agent); Windows: eingehende Firewall-Regel `Roster-Block <ip>`; macOS: pf-Tabelle
  `roster_block` (`table <roster_block> persist` und `block in quick from <roster_block>` in
  `/etc/pf.conf` eintragen). Dauer in Minuten, 0 = dauerhaft.
- **Konto sperren** – `usermod -L`, `Disable-LocalUser`, `pwpolicy -disableuser`. root und
  Administrator werden nie gesperrt.
- **Skript ausführen** – ein Bibliotheks-Skript mit `ROSTER_TRIGGER`, `ROSTER_SOURCE_IP`,
  `ROSTER_USER`, `ROSTER_DEVICE`, `ROSTER_INCIDENT` und `ROSTER_TITLE` in der Umgebung.

Schranken: die **Nie-sperren-Liste** (IPs/CIDRs) und ein **Cooldown** je Gerät und Aktion unter
Einstellungen → Sicherheitsüberwachung; Loopback und die Adresse des Roster-Servers sind immer
geschützt. Jede Reaktion landet im Audit-Log und wird mit Ergebnis am Vorfall angezeigt. Der Tab
**Sperren** listet alle Sperren (Status, Ablauf, Aufheben) und erlaubt das manuelle Sperren
einer IP (Gerät, IP, Dauer von einer Stunde bis dauerhaft, Grund); Vorfälle bieten *IP sperren*
mit derselben Dauer-Auswahl und *Konto sperren*. Eine bereits gesperrte IP wird nicht doppelt
angelegt, sondern mit der neuen Dauer erneuert – so lässt sich eine befristete Sperre an Ort
und Stelle dauerhaft machen.
