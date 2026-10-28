# Changelog

All notable changes to Roster are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/). German version: [CHANGELOG.de.md](https://github.com/boonkerz/roster/blob/master/CHANGELOG.de.md).

## [Unreleased]

### Added
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
