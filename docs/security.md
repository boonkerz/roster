# Security

Roster is built to be exposed to the internet safely.

## Transport

- **TLS** for the whole server — bring your own certificate (the agent can pin the CA) — or
  a TLS-terminating reverse proxy.
- Agents use a **pull model**: they open only **outbound** connections. There is **no
  inbound port** on client machines.

## Enrollment & agent identity

- An admin issues a **short-lived enrollment token**. On first start the agent exchanges it
  for a unique, **per-device agent token**.
- Tokens are stored **hashed** (SHA-256); an agent token can be revoked per device.

## Accounts

- **Two-factor authentication** (mandatory TOTP by default), with backup codes and an
  admin reset.
- Passwords hashed with **argon2id**; session cookies are HttpOnly, `SameSite=Lax` and
  `Secure` in production.
- **Brute-force protection:** after 10 failed sign-ins (password or 2FA code) the source IP
  and the account are blocked for 15 minutes (`429`); the same applies to invalid enrollment
  tokens. Unknown user names take as long as wrong passwords, and at most four password
  checks run at a time – a flood of sign-in attempts cannot exhaust the server's memory.
  `X-Forwarded-For` is trusted only with `ROSTER_BEHIND_PROXY=true`.

## Browser hardening

- **CSRF:** changing requests with a cookie session must come from the server's own origin
  (`Sec-Fetch-Site`/`Origin` check in addition to `SameSite=Lax`). WebSockets for terminal
  and remote desktop accept only the server's own origin (and `ROSTER_EXTERNAL_URL`);
  native clients authenticate with per-session tokens instead.
- Security headers on every response: `X-Frame-Options: DENY` / `frame-ancestors 'none'`
  (no clickjacking), `nosniff`, `Referrer-Policy`, HSTS behind TLS.

## Authorization

- **Role-based access control** with custom roles and **per-user data scope** — enforced
  server-side, not just in the UI. See **[Roles & permissions](features/permissions.md)**.
- User, role and enrollment-token management is restricted to real admins; you cannot lock
  yourself out (no self-demotion, no deleting the last admin).

## Auditing

- An **audit log** records every changing action (who, what, when, source IP).
- Remote sessions are audited and can require **consent at the device**.

## Remote sessions

- File transfers are bound to their device: an agent can only upload the file of its own
  pending read command, and only the target device can fetch a pending upload.

- The built-in VNC server binds to `127.0.0.1` only and runs **only for the duration of a
  session** — never exposed on the LAN.

## Host security monitoring

The server fetches five public IP block lists once a day over HTTPS (Spamhaus DROP, blocklist.de, Feodo Tracker, CINS Army, Tor exit nodes) when threat intelligence is enabled (default). No data about your devices is sent; switch it off under Settings → Security monitoring if outbound requests are not wanted.

File integrity monitoring, login monitoring with brute-force detection, compliance checks and
active response are described under **[Security monitoring](features/security-monitoring.md)**.
