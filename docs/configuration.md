# Configuration

Configuration comes from a YAML file (`--config`) or environment variables (env wins over
file wins over defaults). The most common settings:

| Env | Meaning |
| --- | --- |
| `ROSTER_ADDR` | Listen address (e.g. `:8443` or `:80`) |
| `ROSTER_DB` | `sqlite://./inventory.db` or `postgres://user:pw@host/db` |
| `ROSTER_BEHIND_PROXY` | `true` when behind a TLS-terminating reverse proxy |
| `ROSTER_SECURE_COOKIE` | `true` to mark the session cookie Secure (HTTPS) |
| `ROSTER_EXTERNAL_URL` | Public URL, used in generated install commands |
| `ROSTER_REQUIRE_2FA` | Enforce TOTP for all users (default `true`) |
| `ROSTER_RESULT_RETENTION_DAYS` | History retention (default `30`; `0` = keep forever) |
| `ROSTER_TLS_CERT` / `ROSTER_TLS_KEY` | Serve TLS directly (instead of a proxy) |

!!! info "Legacy env names"
    Older `PCINV_*` variables (from before the rename to Roster) are still accepted and
    mapped to their `ROSTER_*` equivalents, so existing installs keep working.

## Database

SQLite by default — zero setup, one file. For larger fleets or HA, point `ROSTER_DB` at
**PostgreSQL**; the schema and migrations are portable across both.

## Behind a reverse proxy

Terminate TLS at your proxy and set `ROSTER_BEHIND_PROXY=true` and
`ROSTER_SECURE_COOKIE=true`. Forward **WebSocket upgrades** and use generous timeouts
(≥ 60 s) for the `/agent/wait` long-poll and the terminal/remote endpoints.

## Production notes

- **Do not** set a fixed seed enrollment token in production — create tokens in the UI.
- Keep 2FA enabled (`ROSTER_REQUIRE_2FA=true`).

## Account recovery (CLI)

If you get locked out, the binary has offline recovery commands:

```bash
roster-server list-users
roster-server reset-password <user> [new-password]
roster-server disable-2fa <user>
```
