# Dashboard

The overview page shows the fleet at a glance: device counts, failing checks and tasks,
pending patches, vulnerabilities, a device-status donut and the latest check status
changes. Below the tiles you can add **your own charts**.

## Custom charts

Click **+ Chart** and pick a source, a chart type and (for history) a time range:

| Source | What it counts |
|---|---|
| **Metric** | the dashboard numbers — devices total/online/offline, failing checks and tasks, pending patches, vulnerabilities — or the device-status split |
| **Check** | one policy check across all devices: how many are passing, warning, failing |
| **Field** | one custom device field: how many devices carry each value — e.g. the Docker image versions a collector task writes, see [Inventory → Custom fields](inventory.md#custom-fields). List fields count every entry |

Chart types:

- **Bars** — the current distribution, one bar per value (up to 12, the rest folded into
  *Other*). Best for many values such as versions.
- **Donut** — the current shares, up to 7 segments plus *Other*. Best for a handful of
  categories such as a check's status.
- **History** — a line per value over 24 hours up to a year, from **hourly snapshots** the
  server takes of every metric, check status and field value. The first snapshot exists
  about a minute after the server starts, then one at every full hour; snapshots are kept
  for a year.

Hover a bar, segment or point for exact numbers. Charts refresh every minute, and the
status colours (green/orange/red) are reserved for check and device status; other series
use a fixed, colour-blind-safe order.

Your charts are stored in your **user account**, like the device-list columns and saved
filters, so they follow you to any browser. Reorder them with the arrows that appear when
you hover a chart; the pencil edits, the cross removes.

Users with a restricted data scope (see [Roles & permissions](permissions.md)) see counts
for their sites only — snapshots are taken per site and summed within the user's scope.
