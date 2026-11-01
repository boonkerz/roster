# Changelog

All notable changes to Roster are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). German version: [CHANGELOG.de.md](https://github.com/boonkerz/roster/blob/master/CHANGELOG.de.md).

## [Unreleased]

### Changed
- Device → Logins: column is now “Events since agent start” (with explanation), as the counter only counts new lines while “Test” reads retroactively.

## [0.18.6] - 2026-11-01

### Changed
- Dashboard: country metrics show up to 40 countries as bars, sorted descending (instead of 12 + “Other”); the remainder is labelled “Other (n countries)”.

## [0.18.5] - 2026-11-01

### Fixed
- **Automatic blocking no longer blocks private/internal addresses** (10/8, 172.16/12, 192.168/16, 100.64/10, fd00::/7, link-local). Background: a log source behind a reverse proxy only saw the proxy's Docker-internal address; the “attack pattern” incident blocked it fleet-wide in DOCKER-USER and cut the proxy off from the backends. The incident is still recorded, the response reads *skipped: private address*. Settings → Security monitoring has an opt-in; manual blocks remain possible.

### Added
- Security → Blocks: **search** (IP/network, device, reason, creator) and **status filter** (active/queued, expired, released, failed) – server-side, up to 1,000 rows; the **everywhere** button releases an IP on all devices with one click (`POST /security/unblock-ip`).

## [0.18.4] - 2026-11-01

### Fixed
- Log sources: lines from **nginx-proxy** (forego prefix with colour codes) were not unwrapped cleanly, so the bare-IP detection did not apply. ANSI codes are now stripped. New attack patterns: command/code injection in path or query (`cmd=`, `=curl`, `${jndi`, `<script`, `/etc/passwd`, `/proc/self` …), hidden files (`/.git`, `/.terraform`, `/.env`), credential/config files (`server.key`, `credentials`, `.tfvars`, `.toml`, `.properties`, `.pem` …), a reverse proxy's **503** without a matching backend host counts like 404, and failed requests to the **IP address instead of the domain name** (default host `_`) count as scanner activity. On a real nginx-proxy log (5,000 lines) detection rises from 1,820 to 2,709 hits.

## [0.18.3] - 2026-11-01

### Added
- Policies → Security → log sources: **Test** – the agent of a device of your choice reads the last lines of the source (even before saving) and shows files/containers found, hits and a sample of recognised events; if no container matches, the names of the running containers.
- Device → Logins: card **Monitoring on this device** – effective login monitoring, file integrity, compliance and log sources with the read status reported by the agent (policy, targets found, events since agent start, last hit, error) and a test per source. The agent reports the status with every check-in.
- **Countries of source IPs**: embedded IP-to-country database (ip-location-db, public domain, no external service). Flags on incidents, login events and blocks, a *Country* column on blocks, tiles *Attack sources by country (24 h)* and *Blocked IPs by country (30 days)* on the Security page, and the dashboard metrics **Blocked IPs by country** and **Attack sources by country** (bar/donut/history; new users get the country chart by default). `make geoip` refreshes the database.

## [0.18.2] - 2026-11-01

### Added
- Policies → Notifications: **Report security incidents** – one checkbox per incident kind (brute force, login after brute force, attack pattern, new source IP, account change, lockout, file change) and the option *do not report when the source IP was blocked automatically*. Incidents are always recorded; this only controls the alert channels (push, e-mail …). Messages now include the reaction taken.
- Security log sources: format **Mail** (Postfix SASL, Dovecot, Exim) and Apache support in the web error log format; the templates are generic now (web server as file or Docker container, SSH/SFTP container, FTP, mail, custom pattern).

### Fixed
- Policies: the log-source form overflowed the card; the layout is rebuilt.

## [0.18.1] - 2026-11-01

### Changed
- Policies: the policy editor is split into tabs – **Checks, Tasks, Backups, Notifications, Security, Assignments** – with the number of entries per tab, instead of one long page. README and landing page now describe fleet patch management and security monitoring incl. log sources and automatic blocking.

## [0.18.0] - 2026-11-01

### Added
- Security: **automatic blocking without policy rules** – Settings → Security monitoring can block the source IP of every brute force, login after brute force and attack pattern on the reporting device (duration from one hour to permanent). Optional **fleet blocking** blocks the address on every device whose policy monitors logins or reads log sources, and **subnet blocking** blocks the whole IPv4 /24 once several suspicious IPs from the same network showed up within 24 hours.
- Security: **log sources** per policy – the agent reads additional logs every minute: files (globs) or the output of Docker containers (name or pattern). Formats: nginx access log (401 → failed login; scanner paths, malformed requests → suspicious request), nginx error log, sshd output of SSH/SFTP containers (e.g. atmoz/sftp), FTP servers (vsftpd, proftpd, pure-ftpd) and custom regular expressions. Templates for nginx-proxy and SFTP containers.
- Security: new incident **attack pattern** (N suspicious requests from one IP within the window, default 20) with the same responses as brute force, and a second brute-force rule for **distributed slow attacks**: N attempts with unknown user names within 24 hours (default 5) regardless of the per-window threshold.
- Security: **bulk blocking** in the Incidents tab – select many incidents, block all their source IPs with one click (duration, fleet option) and acknowledge them in the same step. Manual blocks accept networks in CIDR notation and can target all monitored devices.

### Changed
- Agent: IP blocks are now also enforced in the nftables **forward** hook (and `FORWARD`/`DOCKER-USER` with iptables), so blocked addresses can no longer reach Docker containers with published ports; networks are stored in interval sets. Agents older than this version only block in the input hook.

## [0.17.3] - 2026-11-01

### Added
- Security: manual IP blocks in the **Blocks** tab (device, IP, duration, reason) and a duration choice at incidents – one hour, 24 hours, 7 days, 30 days or **permanent**.

### Fixed
- Security: blocking an IP that is already blocked no longer creates a duplicate entry – the existing block is renewed with the new duration (also makes a temporary block permanent).
- Vulnerabilities: the **severity** column was empty for almost all findings. OSV details were only fetched for the first 300 entries per scan and CVSS vectors were not evaluated. The scanner now derives the level from distribution priorities (Ubuntu/Debian), computes CVSS v2/v3 base scores, inherits the level from linked CVEs (e.g. USN notices), reuses details already stored, fetches up to 1000 new ones in parallel and uses the long description when the summary is missing.

## [0.17.2] - 2026-11-01

### Fixed
- Agent: fixed a **check-in storm** – checks with frequency "every check-in" always produced fresh results and immediately triggered the next check-in, so agents checked in roughly every second instead of at the configured interval. An immediate check-in is now only requested when a check status changed or a task ran, at most once every 30 seconds.

## [0.17.1] - 2026-11-01

### Fixed
- Policies: the policy editor crashed ("Cannot read properties of null") when the compliance rule catalog was loaded – rules without a profile restriction were delivered as `null`; the server now sends empty lists and the editor tolerates missing fields.

## [0.17.0] - 2026-11-01

### Added
- Patches: new **Patches** page with all pending updates across the fleet – security updates
  first, filters (client, site, security only, approved only, search), multi-select with
  approve/revoke and install (approved / security / approved security / all, apt strategy).
  Summary tiles (devices with security updates, share without security updates, never scanned).
- Patches: the agent now reports **security updates** separately (apt `-security` suites, dnf
  security advisories, Windows Update categories/MSRC severity, macOS “Security”) plus new and
  installed version and source. Device list column and filter "Security updates", smart-group
  field `security_updates_count`, dashboard tile and statistics key `security_patches`.
- Patches: "Install security updates" per device, in the device list bulk bar and in bulk
  actions (scope: all / approved / security only, apt strategy).
- Patches: **patch history** – the agent reads what was installed, upgraded or removed from
  the package manager logs (apt `history.log` incl. rotated files, `dnf.rpm.log`, `yum.log`,
  `pacman.log`), the Windows Update history and `softwareupdate --history` (macOS). Fleet
  timeline on the Patches page ("History" tab with period/action/client filters), 90-day
  history per device in the Patches tab, kept for one year; the health report gains a
  column "Installed (7 days)".
- Patches: **package and repository inventory** – "Packages" tab (installed software across
  the fleet grouped by package and version with distribution bars and the devices per
  version) and "Repositories" tab (apt incl. deb822, dnf/yum, zypper, pacman, apk, winget,
  brew; filters for third-party repos, disabled sources and sources without signature
  check). Per device: package sources in the Patches tab, **automatic-update status**
  (unattended-upgrades, dnf-automatic, Windows Update, macOS), **running vs. installed
  kernel** (reboot hint; the reboot check now also uses `needs-restarting` and the kernel
  comparison) and OS details (codename, Windows build/edition, macOS build). New device
  list columns, filters and smart-group fields (`auto_updates`, `os_codename`,
  `os_edition`, `kernel_pending`).
- Patches: **OS lifecycle (EOL)** – the server matches each device's OS version (Ubuntu,
  Debian, RHEL family, Fedora, Alpine, openSUSE/SLES, Amazon/Oracle Linux, FreeBSD, macOS,
  Windows client incl. Enterprise/LTSC cycles, Windows Server) and Proxmox VE against
  endoflife.date (fetched daily, cached server-side). "Lifecycle" tab on the Patches page,
  "End of support" badge on device, list column, filter and smart-group field `eol_status`,
  dashboard tile, alert when the end of support is approaching (lead time configurable,
  default 90 days) or reached. Settings → Notifications: lead time, alert switch and
  "Re-evaluate now". Rolling-release systems are skipped.
- Security: **file integrity monitoring (FIM)** – policies get a "Security monitoring"
  section with monitored paths (file, directory or glob; recursive; exclusion globs;
  platform; alert on/off; "Insert default paths" for /etc, /usr/local/bin, ~/.ssh, cron,
  Windows hosts/Tasks/Startup, macOS LaunchDaemons) and a scan interval. The agent keeps a
  baseline (SHA-256, permissions, owner, mtime) in its state directory and reports created,
  modified, deleted and permission/owner changes; alerts are bundled per check-in. New
  "Security" page with the fleet-wide "File integrity" tab (filters, acknowledge), device
  tab "File integrity" (re-scan, reset baseline) in the new "Security" tab group, open-change
  counter on the device.
- Security: **login monitoring and incidents** – with "Monitor logins" enabled in a policy the
  agent reads SSH/console logins, sudo/su and account changes incrementally from journald or
  `/var/log/auth.log`/`secure`, the Windows Security log (4624/4625/4740/4720/4726/4728/4732 …)
  and the macOS unified log. The server derives **incidents**: brute force per source IP
  (threshold and window configurable), successful login after brute force (critical), first
  login from a new source IP (after a learning phase), account/group changes (admin groups as
  warning) and lockouts – each alerted through the usual channels. Security page tabs
  "Incidents" (acknowledge/resolve, filters, top source IPs) and "Logins", device tab
  "Logins", open-incident counter in the navigation, device list and dashboard. New settings
  area "Security monitoring" (thresholds, learning phase, never-block list, cooldown).
- Security: **compliance / baseline checks** – a CIS-style rule catalog (Linux: SSH settings,
  /etc/shadow and /etc/passwd permissions, empty passwords, extra UID 0, sudo NOPASSWD, password
  aging, firewall, auditd, NTP, SELinux/AppArmor, ASLR, IP forwarding, ICMP redirects, SYN
  cookies, world-writable /etc, telnet/rsh, core dumps; Windows: UAC, SMBv1, RDP NLA/off,
  guest and built-in admin, firewall profiles, Defender, BitLocker, AutoPlay, LLMNR, password
  policy, lockout, PowerShell 2.0, WDigest, LSA PPL; macOS: FileVault, firewall, stealth mode,
  Gatekeeper, SIP, screen-saver password, guest, remote login; all: automatic updates, reboot
  pending). Enabled per policy with profile (server/workstation), interval and per-rule
  selection; the agent evaluates them and reports a score. Device tab "Compliance" (findings,
  recommendations, re-check), fleet view by device and by rule on the Security page, device
  list column, filter and smart-group field `compliance_score`, dashboard tile, and the new
  check type **Compliance score** (minimum score, optional fail on any high-severity rule)
  for alerting and remediation.
- Security: **active response** – policies can react to incidents automatically: block the
  source IP for N minutes or permanently (Linux nftables with set timeouts, iptables fallback;
  Windows Firewall rule; macOS pf table `roster_block`), lock the local account (never
  root/Administrator) or run a library script with `ROSTER_TRIGGER`, `ROSTER_SOURCE_IP`,
  `ROSTER_USER`, `ROSTER_DEVICE` in the environment. Never-block list and cooldown in
  Settings → Security monitoring, loopback and the server address are always protected.
  Every reaction is audited and shown at the incident with its result; file-integrity alerts
  now also create a `fim_change` incident so they can trigger scripts. Security page tab
  "Blocks" (status, expiry, release), manual "Block IP" / "Lock account" buttons on
  incidents, active blocks in the device "Logins" tab. New agent commands `block_ip`,
  `unblock_ip`, `lock_user`, `unlock_user`, `list_blocks`.
- Policies: **Notifications** section – software-change alerts can be switched on/off per policy
  (device > site > company precedence), filtered by change type (installed/removed/updated) and
  by ignore patterns (e.g. `Microsoft Edge*`). The device setting is now three-way
  (*As policy / Notify / Don't notify*) and shows the effective value and its source.
- Checks: history chart per check on the device (value + status strip, 1 h–30 d),
  `check_samples` kept for 10 days.
- Dashboard: configurable charts (metrics, check status, field values) with hourly statistics
  history; device status and recent transitions as tiles; tile width and drag-and-drop.
- Device list: filter on custom fields (set/not set, type-aware comparison); selectable columns
  including custom fields, stored in the user account; export selected devices as CSV with a
  column picker.
- Taskbar app: restart button, SSH key for SFTP, light theme, settings; search also matches
  custom fields.
- Docs: bilingual website (EN/DE) with landing page and new screenshots on roster-rmm.dev.
- Changelog (this file) and automatic GitHub release notes.

### Changed
- API: `PUT /settings/general` now changes only the fields that are sent (timezone, EOL
  lead time, EOL alerts).
- API: `POST /bulk/install-updates` accepts `approved`, `security_only` and `apt_mode`;
  `POST /devices/{id}/install-updates` accepts `security_only`. Update items carry `version`,
  `current_version`, `source`, `security` and `kind`.
- Check alerts with auto-remediation are only sent when the remediation did not help.
- Device list: bulk action bar confirms inline instead of using a browser dialog.
- Web UI: `no-cache` for index.html and `immutable` for hashed assets.
- Dashboard charts fill the tile width and are centered.

### Removed
- Per-device checkbox "Do not notify on software changes" (replaced by the three-way setting;
  muted devices are migrated to *Don't notify*). API: `PUT /devices/{id}/mute-software`
  replaced by `PUT/GET /devices/{id}/software-alerts`.

## [0.16.2] - 2026-09-29

### Added
- Proxmox backups: retention "keep the last N" and cleanup of full storages.
- Taskbar app: detail panel with custom fields, crisp text rendering; available as a download
  in the settings.
- Device list: filter by failing check and handle the selection with a bulk action.
- Viewer and taskbar app: `--install` sets everything up in one step.

## [0.16.1] - 2026-09-22

### Fixed
- Backup check: a successful run counts even when the target device is asleep.

## [0.16.0] - 2026-09-18

### Added
- Central time zone for backup and agent schedules.
- Backup target storage per guest.

## [0.15.0] - 2026-09-18

### Added
- Backups section in policies (server-side scheduling, start condition, follow-up action on
  another device).
- Viewer downloads in the settings.

## [0.14.0] - 2026-09-13

### Added
- Taskbar app (tray + window with check/task status, terminal, viewer, SFTP).
- Proxmox guests via the agent on the PVE host (start/stop, backup status).
- Temperature sensors with history and `temperature` check.

[Unreleased]: https://github.com/boonkerz/roster/compare/v0.18.6...HEAD
[0.18.6]: https://github.com/boonkerz/roster/compare/v0.18.5...v0.18.6
[0.18.5]: https://github.com/boonkerz/roster/compare/v0.18.4...v0.18.5
[0.18.4]: https://github.com/boonkerz/roster/compare/v0.18.3...v0.18.4
[0.18.3]: https://github.com/boonkerz/roster/compare/v0.18.2...v0.18.3
[0.18.2]: https://github.com/boonkerz/roster/compare/v0.18.1...v0.18.2
[0.18.1]: https://github.com/boonkerz/roster/compare/v0.18.0...v0.18.1
[0.18.0]: https://github.com/boonkerz/roster/compare/v0.17.3...v0.18.0
[0.17.3]: https://github.com/boonkerz/roster/compare/v0.17.2...v0.17.3
[0.17.2]: https://github.com/boonkerz/roster/compare/v0.17.1...v0.17.2
[0.17.1]: https://github.com/boonkerz/roster/compare/v0.17.0...v0.17.1
[0.17.0]: https://github.com/boonkerz/roster/compare/v0.16.2...v0.17.0
[0.16.2]: https://github.com/boonkerz/roster/compare/v0.16.1...v0.16.2
[0.16.1]: https://github.com/boonkerz/roster/compare/v0.16.0...v0.16.1
[0.16.0]: https://github.com/boonkerz/roster/compare/v0.15.0...v0.16.0
[0.15.0]: https://github.com/boonkerz/roster/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/boonkerz/roster/releases/tag/v0.14.0
