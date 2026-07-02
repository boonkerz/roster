# Screenshots

These images are used by the top-level `README.md`. They are **generated**, not
committed by hand, so they always reflect the current UI.

Generate / refresh them (needs Go + Node.js; Playwright's Chromium is installed
automatically on first run):

```bash
./scripts/screenshots.sh
```

The script starts a throwaway demo server (SQLite, 2FA off) plus a real agent on the
local machine, logs in, and captures both German and English variants:
`dashboard-{de,en}.png`, `devices-{de,en}.png`, `live-{de,en}.png`,
`services-{de,en}.png`. Then embed the ones you like in `README.md`.
