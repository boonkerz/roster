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
- Passwords hashed with **argon2id**; session cookies are HttpOnly.

## Authorization

- **Role-based access control** with custom roles and **per-user data scope** — enforced
  server-side, not just in the UI. See **[Roles & permissions](features/permissions.md)**.
- User, role and enrollment-token management is restricted to real admins; you cannot lock
  yourself out (no self-demotion, no deleting the last admin).

## Auditing

- An **audit log** records every changing action (who, what, when, source IP).
- Remote sessions are audited and can require **consent at the device**.

## Remote sessions

- The built-in VNC server binds to `127.0.0.1` only and runs **only for the duration of a
  session** — never exposed on the LAN.
