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
- Passwörter werden mit **argon2id** gehasht; Session-Cookies sind HttpOnly.

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

- Der eingebaute VNC-Server bindet nur an `127.0.0.1` und läuft **nur für die Dauer einer
  Sitzung** – nie im LAN erreichbar.
