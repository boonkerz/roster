#!/usr/bin/env bash
# Erzeugt die Dokumentations-Screenshots (Englisch) via Wegwerf-Demo-Server + Agent
# und capture-docs.mjs. Ausgabe: docs/screenshots/. Braucht Go + Node.
set -euo pipefail
cd "$(dirname "$0")/.."

DD="$(mktemp -d)"
OUT="docs/screenshots"; mkdir -p "$OUT"
PORT="${PORT:-18085}"
trap 'kill $(jobs -p) 2>/dev/null || true; rm -rf "$DD"; git checkout -- web/dist/index.html 2>/dev/null || true' EXIT

echo "→ web deps + Playwright/Chromium…"
( cd web && npm install >/dev/null 2>&1 )
( cd web && npm ls playwright >/dev/null 2>&1 || npm install --no-save playwright >/dev/null 2>&1 )
( cd web && npx playwright install chromium >/dev/null 2>&1 || true )

echo "→ build web + server + agent…"
( cd web && npm run build >/dev/null 2>&1 )
CGO_ENABLED=0 go build -o "$DD/server" ./cmd/server
CGO_ENABLED=0 go build -o "$DD/agent"  ./cmd/agent

cat > "$DD/agent.yaml" <<YML
server_url: http://127.0.0.1:$PORT
enrollment_token: demotoken
state_path: $DD/state.json
disable_public_ip: true
disable_update_check: true
disable_auto_update: true
YML

echo "→ start demo server + agent…"
ROSTER_DB="sqlite://$DD/demo.db" ROSTER_ADDR="127.0.0.1:$PORT" \
  ROSTER_SEED_ADMIN_USER=admin ROSTER_SEED_ADMIN_PASSWORD=demo1234 \
  ROSTER_REQUIRE_2FA=false ROSTER_SEED_ENROLL_TOKEN=demotoken ROSTER_CHECKIN_INTERVAL=5s \
  "$DD/server" run &
sleep 3
"$DD/agent" -config "$DD/agent.yaml" run &
sleep 14  # enroll + ein paar Checkins

echo "→ capture → $OUT …"
( cd web && node ../scripts/capture-docs.mjs "http://127.0.0.1:$PORT" "../$OUT" )
echo "✓ done → $OUT"
