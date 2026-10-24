#!/usr/bin/env bash
# Erzeugt die Dokumentations-Screenshots (Englisch + Deutsch) via Wegwerf-Demo-Server
# + lokalem Agent und capture-docs.mjs. Ausgabe: docs/screenshots/ (name.png, name.de.png).
# Braucht Go, Node und sqlite3 (für die Verlaufsdaten der Diagramme).
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

echo "→ seed demo data…"
( cd web && node ../scripts/capture-docs.mjs "http://127.0.0.1:$PORT" seed "$DD/ids.json" )
sleep 12  # Checks laufen mit dem nächsten Checkin

# Verlaufsdaten direkt in die Demo-DB: Check-Samples (10 Tage), Auslastung (7 Tage),
# Dashboard-Statistik (30 Tage) – sonst wären die Diagramme leer.
echo "→ seed history…"
python3 - "$DD/demo.db" "$DD/ids.json" <<'PY'
import json, math, random, sqlite3, sys, time
db = sqlite3.connect(sys.argv[1]); c = db.cursor()
ids = json.load(open(sys.argv[2])); dev = ids["devId"]; chk = ids["checks"]; fld = ids["fields"]
now = int(time.time() * 1000); H = 3600_000; M5 = 300_000
random.seed(7)
rows = []
for i in range(10 * 24 * 12):                      # alle 5 min, 10 Tage rückwärts
    ts = now - i * M5; back = i / (10 * 24 * 12)
    disk = 62 + (1 - back) * 16 + random.uniform(-0.6, 0.6)
    if 40 < i < 52: disk = 96                       # kurzer Ausschlag vor ~3,5 h
    rows.append((dev, chk.get("disk"), ts, "failing" if disk >= 95 else "warning" if disk >= 85 else "passing", disk))
    mem = 48 + 14 * math.sin(i / 60) + random.uniform(-3, 3)
    rows.append((dev, chk.get("memory"), ts, "warning" if mem >= 90 else "passing", mem))
    cpu = max(2, 18 + 15 * math.sin(i / 23) + random.uniform(-8, 8) + (45 if i % 173 == 0 else 0))
    rows.append((dev, chk.get("cpu"), ts, "warning" if cpu >= 90 else "passing", cpu))
    ping = 3.5 + random.uniform(0, 2.5) + (60 if i % 211 == 0 else 0)
    rows.append((dev, chk.get("ping"), ts, "warning" if ping >= 50 else "passing", ping))
    rows.append((dev, chk.get("http"), ts, "passing", 200))
    rows.append((dev, chk.get("updates"), ts, "passing", 3 if i > 24 * 12 * 2 else 0))
    temp = 52 + 10 * math.sin(i / 40) + random.uniform(-2, 2)
    rows.append((dev, chk.get("temperature"), ts, "warning" if temp >= 80 else "passing", temp))
    rows.append((dev, chk.get("service"), ts, "failing" if i < 14 or 300 < i < 310 else "passing", 0))
c.executemany("INSERT INTO check_samples (device_id, check_id, ts, status, value) VALUES (?,?,?,?,?)", [r for r in rows if r[1]])
mrows = []
for i in range(7 * 24 * 12):
    ts = now - i * M5
    mrows.append((dev, ts, max(2, 20 + 15 * math.sin(i / 23) + random.uniform(-8, 8)), 50 + 12 * math.sin(i / 60) + random.uniform(-3, 3), 70 + random.uniform(-0.5, 0.5), 50 + 8 * math.sin(i / 40) + random.uniform(-2, 2)))
c.executemany("INSERT INTO metrics_samples (device_id, ts, cpu, mem, disk, temp) VALUES (?,?,?,?,?,?)", mrows)
srows = []
site = ids.get("siteId", "")
for h in range(30 * 24):
    ts = (now // H) * H - h * H
    online = 1 if (h % 97) not in (3, 4) else 0
    srows += [(ts, site, "summary", "devices_total", "", 1), (ts, site, "summary", "devices_online", "", online),
              (ts, site, "summary", "devices_offline", "", 1 - online), (ts, site, "summary", "failing_checks", "", 1 if h < 3 or 60 < h < 63 else 0),
              (ts, site, "summary", "device_status", "online" if online else "offline", 1)]
    if chk.get("disk"): srows.append((ts, site, "check", chk["disk"], "failing" if 40 < h * 12 < 52 else "passing", 1))
    if chk.get("service"): srows.append((ts, site, "check", chk["service"], "failing" if h < 3 or 60 < h < 63 else "passing", 1))
    if fld.get("Stage"): srows.append((ts, site, "field", fld["Stage"], "prod", 1))
c.executemany("INSERT INTO stat_samples (ts, site_id, kind, key, label, value) VALUES (?,?,?,?,?,?)", srows)
db.commit(); print("history rows:", len(rows), len(mrows), len(srows))
PY

for lang in en de; do
  echo "→ capture ($lang) → $OUT …"
  ( cd web && node ../scripts/capture-docs.mjs "http://127.0.0.1:$PORT" "../$OUT" "$lang" )
done
echo "✓ done → $OUT"
