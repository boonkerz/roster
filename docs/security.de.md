# Sicherheit

Roster ist dafür gebaut, sicher im Internet erreichbar zu sein.

## Transport

- **TLS** für den gesamten Server – mit eigenem Zertifikat (der Agent kann die CA pinnen) –
  oder ein TLS-terminierender Reverse-Proxy.
- Agents arbeiten im **Pull-Modell**: Sie öffnen ausschließlich **ausgehende** Verbindungen.
  Auf den Client-Rechnern gibt es **keinen eingehenden Port**.

## Enrollment & Agent-Identität

- Ein Admin stellt ein **kurzlebiges Enrollment-Token** aus. Beim ersten Start tauscht der
  Agent es gegen ein eindeutiges **Agent-Token pro Gerät**.
- Tokens werden **gehasht** gespeichert (SHA-256); ein Agent-Token lässt sich pro Gerät
  widerrufen.

## Konten

- **Zwei-Faktor-Authentifizierung** (TOTP, standardmäßig verpflichtend) mit Backup-Codes und
  Admin-Reset.
- Passwörter werden mit **argon2id** gehasht; Session-Cookies sind HttpOnly, `SameSite=Lax`
  und in Produktion `Secure`.
- **Schutz gegen Passwort-Raten:** Nach 10 Fehlversuchen (Passwort oder 2FA-Code) sind
  Quell-IP und Konto 15 Minuten gesperrt (`429`); dasselbe gilt für ungültige
  Enrollment-Tokens. Unbekannte Nutzernamen dauern genauso lange wie falsche Passwörter, und
  höchstens vier Passwortprüfungen laufen gleichzeitig – eine Flut von Anmeldeversuchen kann
  den Speicher des Servers nicht erschöpfen. `X-Forwarded-For` zählt nur mit
  `ROSTER_BEHIND_PROXY=true`.

## Browser-Härtung

- **CSRF:** Ändernde Anfragen mit Cookie-Sitzung müssen von der eigenen Herkunft des Servers
  kommen (Prüfung von `Sec-Fetch-Site`/`Origin` zusätzlich zu `SameSite=Lax`). WebSockets für
  Terminal und Fernsteuerung akzeptieren nur die eigene Herkunft (und `ROSTER_EXTERNAL_URL`);
  native Clients weisen sich stattdessen mit Sitzungs-Tokens aus.
- Sicherheits-Header auf jeder Antwort: `X-Frame-Options: DENY` / `frame-ancestors 'none'`
  (kein Clickjacking), `nosniff`, `Referrer-Policy`, HSTS hinter TLS.

## Autorisierung

- **Rollenbasierte Zugriffskontrolle** mit eigenen Rollen und **Datenbereich pro Benutzer** –
  serverseitig durchgesetzt, nicht nur in der UI. Siehe
  **[Rollen & Berechtigungen](features/permissions.md)**.
- Die Verwaltung von Benutzern, Rollen und Enrollment-Tokens ist echten Admins vorbehalten;
  du kannst dich nicht selbst aussperren (keine Selbst-Degradierung, kein Löschen des letzten
  Admins).

## Audit

- Ein **Audit-Log** protokolliert jede ändernde Aktion (wer, was, wann, Quell-IP).
- Fernsitzungen werden auditiert und können eine **Zustimmung am Gerät** erfordern.

## Fernsitzungen

- Dateiübertragungen sind an ihr Gerät gebunden: Ein Agent kann nur die Datei seines eigenen
  offenen Lesebefehls hochladen, und nur das Zielgerät kann einen wartenden Upload abholen.

- Der eingebaute VNC-Server bindet nur an `127.0.0.1` und läuft **nur für die Dauer einer
  Sitzung** – nie im LAN erreichbar.

## Sicherheitsüberwachung der Hosts

Der Server lädt einmal täglich per HTTPS fünf öffentliche IP-Blocklisten (Spamhaus DROP, blocklist.de, Feodo Tracker, CINS Army, Tor-Exit-Nodes), wenn Bedrohungsdaten aktiv sind (Standard). Es werden keine Daten über Ihre Geräte gesendet; unter Einstellungen → Sicherheitsüberwachung lässt sich der Abruf abschalten.

Datei-Integritätsüberwachung, Anmeldungs-Überwachung mit Brute-Force-Erkennung, Compliance-Prüfung
und Active Response sind unter **[Sicherheitsüberwachung](features/security-monitoring.md)**
beschrieben.
