# Konfiguration

Die Konfiguration kommt aus einer YAML-Datei (`--config`) oder aus Umgebungsvariablen (Env
schlägt Datei, Datei schlägt Defaults). Die wichtigsten Einstellungen:

| Env | Bedeutung |
| --- | --- |
| `ROSTER_ADDR` | Listen-Adresse (z. B. `:8443` oder `:80`) |
| `ROSTER_DB` | `sqlite://./inventory.db` oder `postgres://user:pw@host/db` |
| `ROSTER_BEHIND_PROXY` | `true`, wenn ein TLS-terminierender Reverse-Proxy davor steht |
| `ROSTER_SECURE_COOKIE` | `true`, um das Session-Cookie als Secure zu markieren (HTTPS) |
| `ROSTER_EXTERNAL_URL` | Öffentliche URL, wird in generierten Installationsbefehlen verwendet |
| `ROSTER_REQUIRE_2FA` | TOTP für alle Benutzer erzwingen (Standard `true`) |
| `ROSTER_RESULT_RETENTION_DAYS` | Aufbewahrung der Historie (Standard `30`; `0` = unbegrenzt) |
| `ROSTER_CHECK_HISTORY_DAYS` | Aufbewahrung des Check-Verlaufs (ein Messwert pro Checkin) (Standard `10`; `0` = unbegrenzt) |
| `ROSTER_TLS_CERT` / `ROSTER_TLS_KEY` | TLS direkt ausliefern (statt über einen Proxy) |

!!! info "Alte Env-Namen"
    Ältere `PCINV_*`-Variablen (aus der Zeit vor der Umbenennung in Roster) werden weiterhin
    akzeptiert und auf ihre `ROSTER_*`-Gegenstücke abgebildet – bestehende Installationen
    laufen also weiter.

## Datenbank

Standardmäßig SQLite – kein Setup, eine Datei. Für größere Flotten oder HA zeigst du mit
`ROSTER_DB` auf **PostgreSQL**; Schema und Migrationen sind für beide portabel.

## Hinter einem Reverse-Proxy

Terminiere TLS an deinem Proxy und setze `ROSTER_BEHIND_PROXY=true` sowie
`ROSTER_SECURE_COOKIE=true`. Leite **WebSocket-Upgrades** weiter und verwende großzügige
Timeouts (≥ 60 s) für den Long-Poll `/agent/wait` sowie die Terminal-/Fernsteuerungs-Endpunkte.

## Hinweise für die Produktion

- Setze in Produktion **kein** festes Seed-Enrollment-Token – lege Tokens in der UI an.
- Lass 2FA aktiviert (`ROSTER_REQUIRE_2FA=true`).

## Konto-Wiederherstellung (CLI)

Falls du dich ausgesperrt hast, bietet das Binary Offline-Befehle zur Wiederherstellung:

```bash
roster-server list-users
roster-server reset-password <user> [new-password]
roster-server disable-2fa <user>
```
