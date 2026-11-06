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
# DOCKER_HOST ins Leere: der Demo-Agent soll keine lokalen Container/Images melden
# (Screenshots zeigen nur die geseedeten Demo-Daten).
DOCKER_HOST=unix:///nonexistent.sock "$DD/agent" -config "$DD/agent.yaml" run &
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

# Demo-Geräte mit Patch- und Sicherheitsdaten (Flottenseiten „Patches“ und „Sicherheit“):
# ausstehende/Sicherheits-Updates, Patch-Verlauf, Repositories, Support-Ende, Vorfälle,
# Anmeldungen, Datei-Änderungen, Compliance-Ergebnisse und eine aktive IP-Sperre.
echo "→ seed patch/security demo devices…"
python3 - "$DD/demo.db" "$DD/ids.json" <<'PY'
import json, random, sqlite3, sys, uuid
from datetime import datetime, timedelta, timezone
db = sqlite3.connect(sys.argv[1]); c = db.cursor()
ids = json.load(open(sys.argv[2])); site = ids.get("siteId", ""); pol = ids.get("policyId", "")
random.seed(11)
now = datetime.now(timezone.utc).replace(microsecond=0)
def ts(**kw): return (now - timedelta(**kw)).strftime("%Y-%m-%dT%H:%M:%SZ")
def uid(): return uuid.uuid4().hex
NOW = ts()
devs = [
  dict(id=uid(), hostname="web-01", os="ubuntu", os_version="22.04", vendor="Dell Inc.", model="PowerEdge R450", cpu="Intel Xeon Silver 4310", cores=12, threads=24, mem=64, codename="jammy", kr="5.15.0-122-generic", ki="5.15.0-124-generic", auto="security", upd=14, sec=6, score=82, profile="server"),
  dict(id=uid(), hostname="db-01", os="debian", os_version="12", vendor="Hewlett Packard Enterprise", model="ProLiant DL360 Gen10", cpu="Intel Xeon Gold 6226R", cores=16, threads=32, mem=128, codename="bookworm", kr="6.1.0-25-amd64", ki="6.1.0-25-amd64", auto="enabled", upd=3, sec=1, score=91, profile="server"),
  dict(id=uid(), hostname="win-01", os="Microsoft Windows 11 Pro", os_version="10.0.22631", vendor="LENOVO", model="ThinkPad T14 Gen 4", cpu="AMD Ryzen 7 PRO 7840U", cores=8, threads=16, mem=32, codename="", kr="", ki="", auto="notify", upd=5, sec=2, score=64, profile="workstation", build="23H2 (22631.4317)", edition="Pro"),
  dict(id=uid(), hostname="files-02", os="ubuntu", os_version="20.04", vendor="Supermicro", model="SYS-5019C", cpu="Intel Xeon E-2236", cores=6, threads=12, mem=32, codename="focal", kr="5.4.0-195-generic", ki="5.4.0-195-generic", auto="disabled", upd=41, sec=17, score=58, profile="server"),
  dict(id=uid(), hostname="mac-studio", os="darwin", os_version="13.6.9", vendor="Apple", model="Mac Studio (2022)", cpu="Apple M1 Max", cores=10, threads=10, mem=64, codename="Ventura", kr="22.6.0", ki="", auto="enabled", upd=1, sec=1, score=88, profile="workstation"),
]
for d in devs:
    c.execute("""INSERT INTO devices (id, hostname, os, os_version, vendor, model, serial, cpu_model, cpu_cores, cpu_sockets, cpu_threads, memory_bytes,
        first_seen, last_seen, enrolled_at, agent_token_hash, revoked, agent_version, logged_in_users, updates_count, updates_checked_at, security_updates_count,
        site_id, public_ip, disks, physical_disks, gpus, notes, managed, proxmox_version, proxmox_storages, software_alerts, auto_updates, kernel_running,
        kernel_installed, os_build, os_codename, os_edition, auth_monitor_since, compliance_score, compliance_checked_at, compliance_profile)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""",
        (d["id"], d["hostname"], d["os"], d["os_version"], d["vendor"], d["model"], "SN" + d["id"][:8].upper(), d["cpu"], d["cores"], 1, d["threads"], d["mem"] * 1024**3,
         ts(days=200), NOW, ts(days=200), uid(), 0, "0.17.2", "", d["upd"], ts(hours=2), d["sec"],
         site, "", "[]", "[]", "[]", "", 1, "", "[]", 0, d["auto"], d["kr"], d["ki"], d.get("build", ""), d["codename"], d.get("edition", ""),
         ts(days=30), d["score"], ts(hours=1), d["profile"]))
D = {d["hostname"]: d["id"] for d in devs}

# Ausstehende Updates (Paket, Schwere, neue/alte Version, Quelle, Sicherheit).
upd = {
  "web-01": [("openssl", "Critical", "3.0.2-0ubuntu1.18", "3.0.2-0ubuntu1.17", "jammy-security", 1), ("libssl3", "Critical", "3.0.2-0ubuntu1.18", "3.0.2-0ubuntu1.17", "jammy-security", 1),
             ("linux-image-generic", "Important", "5.15.0.124.121", "5.15.0.122.119", "jammy-security", 1), ("bash", "Important", "5.1-6ubuntu1.1", "5.1-6ubuntu1", "jammy-updates,jammy-security", 1),
             ("sudo", "Important", "1.9.9-1ubuntu2.5", "1.9.9-1ubuntu2.4", "jammy-security", 1), ("nginx", "Moderate", "1.18.0-6ubuntu14.6", "1.18.0-6ubuntu14.5", "jammy-security", 1),
             ("curl", "Other", "7.81.0-1ubuntu1.18", "7.81.0-1ubuntu1.17", "jammy-updates", 0), ("vim", "Other", "2:8.2.3995-1ubuntu2.21", "2:8.2.3995-1ubuntu2.20", "jammy-updates", 0),
             ("python3-urllib3", "Other", "1.26.5-1~exp1ubuntu0.2", "1.26.5-1~exp1ubuntu0.1", "jammy-updates", 0), ("tzdata", "Other", "2024b-0ubuntu0.22.04", "2024a-0ubuntu0.22.04.1", "jammy-updates", 0),
             ("git", "Other", "1:2.34.1-1ubuntu1.12", "1:2.34.1-1ubuntu1.11", "jammy-updates", 0), ("rsync", "Other", "3.2.7-0ubuntu0.22.04.3", "3.2.7-0ubuntu0.22.04.2", "jammy-updates", 0),
             ("ca-certificates", "Other", "20240203~22.04.1", "20230311ubuntu0.22.04.1", "jammy-updates", 0), ("snapd", "Other", "2.65.3+22.04", "2.63+22.04", "jammy-updates", 0)],
  "db-01": [("postgresql-15", "Important", "15.8-0+deb12u1", "15.7-0+deb12u1", "bookworm-security", 1), ("libc6", "Other", "2.36-9+deb12u8", "2.36-9+deb12u7", "bookworm", 0), ("systemd", "Other", "252.30-1~deb12u2", "252.26-1~deb12u2", "bookworm", 0)],
  "win-01": [("2024-10 Cumulative Update for Windows 11 Version 23H2 (KB5044285)", "Critical", "", "", "Security Updates KB5044285", 1), ("Security Intelligence Update for Microsoft Defender Antivirus (KB2267602)", "Important", "", "", "Definition Updates KB2267602", 1),
             ("2024-10 .NET 8.0.10 Update for x64 (KB5045934)", "Other", "", "", "Updates KB5045934", 0), ("Intel Corporation - Display - 31.0.101.5522", "Other", "", "", "Drivers", 0), ("Microsoft Edge 130.0.2849.56", "Other", "", "", "Updates", 0)],
  "mac-studio": [("macOS Ventura 13.7.1", "Important", "13.7.1", "13.6.9", "Security", 1)],
}
focal = [("openssl", "Critical", "1.1.1f-1ubuntu2.23", "1.1.1f-1ubuntu2.22", "focal-security", 1), ("linux-image-generic", "Important", "5.4.0.200.198", "5.4.0.195.193", "focal-security", 1), ("samba", "Critical", "2:4.15.13+dfsg-0ubuntu0.20.04.8", "2:4.15.13+dfsg-0ubuntu0.20.04.7", "focal-security", 1)]
focal += [("pkg-%02d" % i, "Important" if i < 15 else "Other", "1.%d" % (i + 1), "1.%d" % i, "focal-security" if i < 15 else "focal-updates", 1 if i < 15 else 0) for i in range(1, 39)]
upd["files-02"] = focal
for h, rows in upd.items():
    for name, sev, nv, ov, src, secf in rows:
        kind = "definition" if "Defender" in name else "driver" if "Display" in name else ""
        c.execute("INSERT INTO os_updates (device_id, package, severity, url, version, current_version, source, security, kind) VALUES (?,?,?,?,?,?,?,?,?)", (D[h], name, sev, "", nv, ov, src, secf, kind))
for h, name in [("web-01", "openssl"), ("web-01", "libssl3"), ("web-01", "sudo"), ("db-01", "postgresql-15")]:
    c.execute("INSERT INTO patch_approvals (device_id, name) VALUES (?,?)", (D[h], name))

# Patch-Verlauf der letzten Wochen.
hist = [("web-01", "apt", [("openssh-server", "1:8.9p1-3ubuntu0.10", "1:8.9p1-3ubuntu0.11"), ("libexpat1", "2.4.7-1ubuntu0.3", "2.4.7-1ubuntu0.5"), ("nginx", "1.18.0-6ubuntu14.4", "1.18.0-6ubuntu14.5"), ("libpam0g", "1.4.0-11ubuntu2.4", "1.4.0-11ubuntu2.5"), ("python3", "3.10.6-1~22.04", "3.10.6-1~22.04.1"), ("apt", "2.4.12", "2.4.13"), ("libglib2.0-0", "2.72.4-0ubuntu2.3", "2.72.4-0ubuntu2.4")]),
        ("db-01", "apt", [("postgresql-15", "15.6-0+deb12u1", "15.7-0+deb12u1"), ("openssl", "3.0.13-1~deb12u1", "3.0.14-1~deb12u2"), ("curl", "7.88.1-10+deb12u7", "7.88.1-10+deb12u8"), ("bind9-libs", "1:9.18.24-1", "1:9.18.28-1~deb12u2")]),
        ("win-01", "wua", [("2024-09 Cumulative Update for Windows 11 Version 23H2 (KB5043076)", "", ""), ("Security Intelligence Update for Microsoft Defender Antivirus (KB2267602)", "", ""), ("2024-09 .NET 8.0.8 Update for x64 (KB5044030)", "", ""), ("Windows Malicious Software Removal Tool x64 (KB890830)", "", "")]),
        ("mac-studio", "softwareupdate", [("macOS Ventura 13.6.9", "", "13.6.9"), ("Safari", "", "17.6"), ("XProtectPlistConfigData", "", "5273")])]
for h, src, pk in hist:
    for i, (name, ov, nv) in enumerate(pk):
        when = now - timedelta(days=random.choice([1, 2, 5, 9, 14, 21, 30]), hours=random.randint(0, 23))
        c.execute("INSERT INTO patch_history (id, device_id, ts, action, name, old_version, new_version, source, detail) VALUES (?,?,?,?,?,?,?,?,?)",
                  (uid(), D[h], when.strftime("%Y-%m-%dT%H:%M:%SZ"), "upgrade" if ov or src != "wua" else "install", name, ov, nv, src, ""))

# Repositories (inkl. Fremd-Repos) und Support-Ende.
repos = {"web-01": [("ubuntu-jammy", "jammy main restricted universe", "http://archive.ubuntu.com/ubuntu", "apt", 1, 1, "jammy", 0), ("ubuntu-jammy-security", "jammy-security main", "http://security.ubuntu.com/ubuntu", "apt", 1, 1, "jammy-security", 0),
                    ("docker", "docker", "https://download.docker.com/linux/ubuntu", "apt", 1, 1, "jammy", 1), ("nodesource", "nodesource", "https://deb.nodesource.com/node_20.x", "apt", 1, 0, "nodistro", 1)],
         "db-01": [("debian-bookworm", "bookworm main", "http://deb.debian.org/debian", "apt", 1, 1, "bookworm", 0), ("debian-security", "bookworm-security main", "http://security.debian.org/debian-security", "apt", 1, 1, "bookworm-security", 0), ("pgdg", "pgdg", "https://apt.postgresql.org/pub/repos/apt", "apt", 1, 1, "bookworm-pgdg", 1)],
         "files-02": [("ubuntu-focal", "focal main universe", "http://archive.ubuntu.com/ubuntu", "apt", 1, 1, "focal", 0), ("ubuntu-focal-security", "focal-security main", "http://security.ubuntu.com/ubuntu", "apt", 0, 1, "focal-security", 0)],
         "win-01": [("winget", "winget", "https://cdn.winget.microsoft.com/cache", "winget", 1, 1, "", 0), ("msstore", "msstore", "https://storeedgefd.dsx.mp.microsoft.com/v9.0", "winget", 1, 1, "", 0)],
         "mac-studio": [("homebrew/core", "homebrew/core", "https://github.com/Homebrew/homebrew-core", "brew", 1, 1, "", 1)]}
for h, rows in repos.items():
    for rid, name, url, typ, en, gpg, suite, third in rows:
        c.execute("INSERT INTO package_repos (device_id, repo_id, name, url, type, enabled, gpg_check, suite, third_party) VALUES (?,?,?,?,?,?,?,?,?)", (D[h], rid, name, url, typ, en, gpg, suite, third))
eol = [("web-01", "ubuntu", "22.04", "22.04 'Jammy Jellyfish' (LTS)", "2027-06-01", "2024-09-30", "ok"), ("db-01", "debian", "12", "12 (Bookworm)", "2028-06-30", "2026-07-11", "ok"),
       ("win-01", "windows", "11-23h2-w", "11 23H2 (W)", "2025-11-11", "2025-11-11", "eol"), ("files-02", "ubuntu", "20.04", "20.04 'Focal Fossa' (LTS)", "2025-05-31", "2022-10-01", "eol"),
       ("mac-studio", "macos", "13", "13 (Ventura)", (now + timedelta(days=41)).strftime("%Y-%m-%d"), (now + timedelta(days=41)).strftime("%Y-%m-%d"), "soon")]
for h, prod, cyc, label, e, sup, st in eol:
    c.execute("INSERT INTO device_eol (device_id, kind, product, cycle, label, eol_date, support_date, status, checked_at, alerted_status) VALUES (?,?,?,?,?,?,?,?,?,?)", (D[h], "os", prod, cyc, label, e + "T00:00:00Z", sup + "T00:00:00Z", st, NOW, st if st != "ok" else ""))

# Anmeldungen und daraus abgeleitete Vorfälle.
auth = []
for i in range(18):
    auth.append((D["web-01"], ts(minutes=42 - i * 2), "login_failed", random.choice(["root", "admin", "ubuntu", "test"]), "203.0.113.45", "sshd", ""))
auth.append((D["web-01"], ts(minutes=5), "login_ok", "deploy", "203.0.113.45", "sshd", "publickey"))
auth += [(D["web-01"], ts(hours=3), "login_ok", "thomas", "10.20.0.15", "sshd", "publickey"), (D["web-01"], ts(hours=3), "sudo", "thomas", "", "sudo", "/usr/bin/systemctl restart nginx"),
         (D["db-01"], ts(hours=1, minutes=12), "login_ok", "dba", "10.20.0.77", "sshd", "publickey"), (D["db-01"], ts(days=1), "login_ok", "dba", "10.20.0.15", "sshd", "publickey"),
         (D["win-01"], ts(minutes=25), "lockout", "m.mueller", "10.20.1.33", "rdp", "Account locked out after 10 failed attempts"), (D["win-01"], ts(hours=2), "login_ok", "m.mueller", "", "console", ""),
         (D["files-02"], ts(hours=6), "user_added", "svc-backup", "", "useradd", "new user: name=svc-backup, UID=1004"), (D["files-02"], ts(hours=6), "group_changed", "svc-backup", "", "usermod", "add 'svc-backup' to group 'sudo'")]
for dev, t, kind, user, ip, svc, detail in auth:
    c.execute("INSERT INTO auth_events (id, device_id, ts, kind, username, source_ip, service, detail, record_id) VALUES (?,?,?,?,?,?,?,?,?)", (uid(), dev, t, kind, user, ip, svc, detail, ""))
inc_bf = uid()
incidents = [(inc_bf, D["web-01"], ts(minutes=20), "brute_force", "warning", "203.0.113.45", "", "Brute-Force von 203.0.113.45", "18 fehlgeschlagene Anmeldungen innerhalb von 10m0s über sshd (Benutzer: root, admin, ubuntu, test)", "open", "block_ip", "done"),
             (uid(), D["web-01"], ts(minutes=5), "brute_force_success", "critical", "203.0.113.45", "deploy", "Erfolgreiche Anmeldung nach Brute-Force: deploy von 203.0.113.45", "Anmeldung als deploy über sshd nach 18 Fehlversuchen derselben IP", "open", "block_ip", "done"),
             (uid(), D["db-01"], ts(hours=1, minutes=12), "new_source_ip", "info", "10.20.0.77", "dba", "Neue Quell-IP für dba: 10.20.0.77", "Erste Anmeldung von dieser Adresse für dieses Konto", "acknowledged", "", ""),
             (uid(), D["win-01"], ts(minutes=25), "lockout", "warning", "10.20.1.33", "m.mueller", "Konto gesperrt: m.mueller", "Account locked out after 10 failed attempts (RDP, 10.20.1.33)", "open", "", ""),
             (uid(), D["files-02"], ts(hours=6), "account_change", "warning", "", "svc-backup", "Konto svc-backup zur Gruppe sudo hinzugefügt", "add 'svc-backup' to group 'sudo'", "open", "", ""),
             (uid(), D["web-01"], ts(days=2), "brute_force", "warning", "198.51.100.9", "", "Brute-Force von 198.51.100.9", "12 fehlgeschlagene Anmeldungen innerhalb von 10m0s über sshd", "resolved", "block_ip", "done")]
for iid, dev, t, kind, sev, ip, user, title, detail, status, resp, rstat in incidents:
    c.execute("INSERT INTO security_incidents (id, device_id, ts, kind, severity, source_ip, username, title, detail, status, response, response_status, response_command_id, acknowledged_by, acknowledged_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
              (iid, dev, t, kind, sev, ip, user, title, detail, status, resp, rstat, "", "admin" if status != "open" else "", ts(minutes=1) if status != "open" else None))
c.execute("INSERT INTO ip_blocks (id, device_id, ip, reason, incident_id, until_at, status, detail, command_id, created_by, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
          (uid(), D["web-01"], "203.0.113.45", "Brute-Force von 203.0.113.45", inc_bf, (now + timedelta(minutes=40)).strftime("%Y-%m-%dT%H:%M:%SZ"), "active", "nftables: inet roster blocked4", "", "system", ts(minutes=20)))
c.execute("INSERT INTO ip_blocks (id, device_id, ip, reason, incident_id, until_at, status, detail, command_id, created_by, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
          (uid(), D["web-01"], "198.51.100.9", "Brute-Force von 198.51.100.9", "", ts(days=1, hours=23), "expired", "", "", "system", ts(days=2)))

# Datei-Änderungen (FIM).
fim = [(D["web-01"], ts(minutes=18), "/etc/ssh/sshd_config", "modified", "9f2c…", "4b7e…", "-rw-r--r--", "-rw-r--r--", "root:root", "root:root", 3312),
       (D["web-01"], ts(minutes=17), "/root/.ssh/authorized_keys", "modified", "a1d0…", "c77f…", "-rw-------", "-rw-------", "root:root", "root:root", 1140),
       (D["web-01"], ts(minutes=15), "/etc/cron.d/sync", "created", "", "e019…", "", "-rw-r--r--", "", "root:root", 88),
       (D["db-01"], ts(hours=5), "/etc/postgresql/15/main/pg_hba.conf", "modified", "7d31…", "b2aa…", "-rw-r-----", "-rw-r-----", "postgres:postgres", "postgres:postgres", 5021),
       (D["files-02"], ts(hours=6), "/etc/sudoers.d/svc-backup", "created", "", "f4c2…", "", "-r--r-----", "", "root:root", 46),
       (D["files-02"], ts(hours=6), "/etc/shadow", "attrs", "", "", "-rw-r-----", "-rw-r--r--", "root:shadow", "root:shadow", 1623),
       (D["win-01"], ts(days=1), "C:\\Windows\\System32\\drivers\\etc\\hosts", "modified", "02ee…", "91b3…", "", "", "", "", 1024)]
for dev, t, path, ch, oh, nh, om, nm, oo, no_, size in fim:
    c.execute("INSERT INTO fim_events (id, device_id, ts, rule_id, path, change, old_hash, new_hash, old_mode, new_mode, old_owner, new_owner, size, alert, acknowledged) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
              (uid(), dev, t, "", path, ch, oh, nh, om, nm, oo, no_, size, 1, 1 if dev == D["db-01"] else 0))

# Compliance-Ergebnisse je Plattform.
lin = ["ssh_permit_root_login", "ssh_password_auth", "ssh_max_auth_tries", "ssh_x11_forwarding", "ssh_weak_ciphers", "shadow_perms", "passwd_perms", "no_empty_passwords", "single_uid0", "sudo_nopasswd_all", "pass_max_days", "firewall_active", "auto_updates_enabled", "auditd_running", "ntp_active", "mac_enforcing", "aslr", "ip_forward_off", "icmp_redirects_off", "tcp_syncookies", "world_writable_etc", "legacy_services", "core_dumps_restricted", "reboot_not_pending"]
win = ["uac_enabled", "smb1_disabled", "rdp_nla", "rdp_disabled_workstation", "guest_disabled", "builtin_admin_disabled", "firewall_all_profiles", "defender_realtime", "bitlocker_system_drive", "autoplay_disabled", "llmnr_disabled", "password_min_length", "lockout_threshold", "powershell_v2_disabled", "wdigest_disabled", "lsa_ppl", "auto_updates_enabled", "reboot_not_pending"]
mac = ["filevault_on", "firewall_on", "gatekeeper_on", "sip_enabled", "auto_updates_enabled", "screensaver_password", "guest_disabled", "remote_login_off", "stealth_mode", "reboot_not_pending"]
fails = {"web-01": {"ssh_password_auth": "PasswordAuthentication yes", "auditd_running": "auditd.service inactive", "sudo_nopasswd_all": "deploy ALL=(ALL) NOPASSWD: ALL", "reboot_not_pending": "Kernel 5.15.0-124 installiert, 5.15.0-122 läuft"},
         "db-01": {"ssh_x11_forwarding": "X11Forwarding yes", "auditd_running": "auditd.service inactive"},
         "files-02": {"ssh_permit_root_login": "PermitRootLogin yes", "ssh_password_auth": "PasswordAuthentication yes", "firewall_active": "keine aktive Firewall (ufw inactive)", "auto_updates_enabled": "unattended-upgrades deaktiviert", "ssh_weak_ciphers": "aes128-cbc, 3des-cbc", "pass_max_days": "PASS_MAX_DAYS 99999", "legacy_services": "telnet.socket aktiv", "ntp_active": "NTPSynchronized=no", "mac_enforcing": "AppArmor: 0 Profile", "world_writable_etc": "/etc/backup.conf"},
         "win-01": {"smb1_disabled": "SMB1Protocol: Enabled", "bitlocker_system_drive": "C: FullyDecrypted", "llmnr_disabled": "EnableMulticast fehlt", "password_min_length": "Minimum password length: 8", "lockout_threshold": "Lockout threshold: Never", "wdigest_disabled": "UseLogonCredential=1", "lsa_ppl": "RunAsPPL fehlt"},
         "mac-studio": {"stealth_mode": "Stealth mode disabled"}}
na = {"web-01": ["mac_enforcing"], "db-01": ["mac_enforcing", "ip_forward_off"], "files-02": [], "win-01": ["rdp_disabled_workstation"], "mac-studio": ["remote_login_off"]}
ok_ev = {"ssh_permit_root_login": "PermitRootLogin prohibit-password", "firewall_active": "ufw active", "aslr": "kernel.randomize_va_space = 2", "uac_enabled": "EnableLUA=1", "defender_realtime": "RealTimeProtectionEnabled: True", "filevault_on": "FileVault is On."}
for h, rules in [("web-01", lin), ("db-01", lin), ("files-02", lin), ("win-01", win), ("mac-studio", mac)]:
    for r in rules:
        st = "fail" if r in fails[h] else "na" if r in na[h] else "pass"
        c.execute("INSERT INTO compliance_results (device_id, rule_id, status, evidence, checked_at) VALUES (?,?,?,?,?)", (D[h], r, st, fails[h].get(r, ok_ev.get(r, "")), ts(hours=1)))

# Container-Sicherheit: gescannte Images (Basis-OS, Pakete, Dienste), OSV-Treffer und
# Datenbank-Bestand in DB-Containern (Seite Sicherheit → Container).
images = [  # (Gerät, Image-ID, Image, os, version, Name, pkg, Pakete, Dienste, Fehler)
  ("web-01", "a1b2c3d4e5f6", "registry.example.com/shop/php-fpm:2.4", "debian", "13", "Debian GNU/Linux 13 (trixie)", "dpkg", 214, "", ""),
  ("db-01", "a1b2c3d4e5f6", "registry.example.com/shop/php-fpm:2.4", "debian", "13", "Debian GNU/Linux 13 (trixie)", "dpkg", 214, "", ""),
  ("web-01", "b2c3d4e5f6a1", "nginxproxy/nginx-proxy:1.6", "debian", "12", "Debian GNU/Linux 12 (bookworm)", "dpkg", 131, "", ""),
  ("files-02", "c3d4e5f6a1b2", "atmoz/sftp:alpine", "alpine", "3.20.1", "Alpine Linux v3.20", "apk", 38, "sftp,sshd", ""),
  ("db-01", "d4e5f6a1b2c3", "mongo:7", "ubuntu", "22.04", "Ubuntu 22.04.5 LTS", "dpkg", 130, "", ""),
  ("db-01", "e5f6a1b2c3d4", "mariadb:11.4", "ubuntu", "24.04", "Ubuntu 24.04.1 LTS", "dpkg", 149, "", ""),
  ("web-01", "f6a1b2c3d4e5", "redis:7-alpine", "alpine", "3.21.2", "Alpine Linux v3.21", "apk", 19, "redis", ""),
  ("web-01", "0a1b2c3d4e5f", "ghcr.io/example/webhook:2", "", "", "", "", 0, "", "keine Shell im Image (distroless/scratch) – nicht prüfbar"),
]
for h, iid, img, os_, ver, name, pkg, n, svc, err in images:
    c.execute("INSERT INTO docker_image_scans (device_id, image_id, image, os, os_version, os_name, pkg, packages, package_count, services, error, scanned_at, vuln_scanned_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
              (D[h], iid, img, os_, ver, name, pkg, "[]", n, svc, err, ts(hours=3), None if err else ts(hours=2)))
# Treffer: für das SFTP-Image echte Datensätze (Detailansicht), sonst nur Zählwerte.
real = [("files-02", "c3d4e5f6a1b2", "atmoz/sftp:alpine", "openssh-server", "9.7_p1-r3", "CVE-2024-6387", "HIGH", "regreSSHion: signal handler race condition in sshd allows unauthenticated remote code execution", "9.7_p1-r4"),
        ("files-02", "c3d4e5f6a1b2", "atmoz/sftp:alpine", "openssh-sftp-server", "9.7_p1-r3", "CVE-2024-6387", "HIGH", "regreSSHion: signal handler race condition in sshd allows unauthenticated remote code execution", "9.7_p1-r4"),
        ("files-02", "c3d4e5f6a1b2", "atmoz/sftp:alpine", "libcrypto3", "3.3.1-r3", "CVE-2024-6119", "HIGH", "OpenSSL: possible denial of service in X.509 name checks", "3.3.2-r0"),
        ("files-02", "c3d4e5f6a1b2", "atmoz/sftp:alpine", "libssl3", "3.3.1-r3", "CVE-2024-6119", "HIGH", "OpenSSL: possible denial of service in X.509 name checks", "3.3.2-r0")]
for h, iid, img, pk, ver, vid, sev, summ, fixed in real:
    c.execute("INSERT INTO container_vulnerabilities (device_id, image_id, image, package, version, vuln_id, severity, summary, fixed, url, detected_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
              (D[h], iid, img, pk, ver, vid, sev, summ, fixed, "https://osv.dev/vulnerability/" + vid, ts(hours=2)))
counts = {("web-01", "a1b2c3d4e5f6"): (2, 11, 64), ("db-01", "a1b2c3d4e5f6"): (2, 11, 64), ("web-01", "b2c3d4e5f6a1"): (0, 7, 41),
          ("db-01", "d4e5f6a1b2c3"): (0, 3, 27), ("db-01", "e5f6a1b2c3d4"): (1, 6, 52), ("web-01", "f6a1b2c3d4e5"): (0, 0, 0)}
img_of = {(h, iid): img for h, iid, img, *_ in images}
for (h, iid), (crit, high, total) in counts.items():
    for i in range(total):
        sev = "CRITICAL" if i < crit else "HIGH" if i < crit + high else random.choice(["MEDIUM", "MEDIUM", "LOW", ""])
        c.execute("INSERT INTO container_vulnerabilities (device_id, image_id, image, package, version, vuln_id, severity, summary, fixed, url, detected_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
                  (D[h], iid, img_of[(h, iid)], "pkg-%02d" % i, "1.0", "DEMO-%s-%03d" % (iid[:4], i), sev, "", "", "", ts(hours=2)))
dbs = [("db-01", "c0ffee000001", "shop-mongo", "mongo", [("shop", 14), ("admin", 1), ("config", 1), ("local", 1)], ""),
       ("db-01", "c0ffee000002", "shop-mariadb", "mysql", [("shop", 38), ("matomo", 52)], ""),
       ("web-01", "c0ffee000003", "cms-postgres", "postgres", [("cms", 61), ("postgres", 0)], ""),
       ("files-02", "c0ffee000004", "legacy-mysql", "mysql", [], "Zugriff verweigert – keine passenden Zugangsdaten in der Container-Umgebung")]
for h, cid, cname, eng, rows, err in dbs:
    if err:
        c.execute("INSERT INTO docker_dbs (device_id, container_id, container, engine, name, collections, error, updated_at) VALUES (?,?,?,?,?,?,?,?)", (D[h], cid, cname, eng, "", 0, err, NOW))
    for name, n in rows:
        c.execute("INSERT INTO docker_dbs (device_id, container_id, container, engine, name, collections, error, updated_at) VALUES (?,?,?,?,?,?,?,?)", (D[h], cid, cname, eng, name, n, "", NOW))
db.commit(); print("demo devices:", list(D))
PY

for lang in en de; do
  echo "→ capture ($lang) → $OUT …"
  ( cd web && node ../scripts/capture-docs.mjs "http://127.0.0.1:$PORT" "../$OUT" "$lang" )
done
echo "✓ done → $OUT"
