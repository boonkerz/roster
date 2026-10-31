# Changelog

All notable changes to Roster are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). German version: [CHANGELOG.de.md](https://github.com/boonkerz/roster/blob/master/CHANGELOG.de.md).

## [Unreleased]

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

[Unreleased]: https://github.com/boonkerz/roster/compare/v0.16.2...HEAD
[0.16.2]: https://github.com/boonkerz/roster/compare/v0.16.1...v0.16.2
[0.16.1]: https://github.com/boonkerz/roster/compare/v0.16.0...v0.16.1
[0.16.0]: https://github.com/boonkerz/roster/compare/v0.15.0...v0.16.0
[0.15.0]: https://github.com/boonkerz/roster/compare/v0.14.0...v0.15.0
[0.14.0]: https://github.com/boonkerz/roster/releases/tag/v0.14.0
