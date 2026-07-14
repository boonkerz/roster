# Checks & tasks

Policies bundle **checks** (monitoring) and **tasks** (scheduled automation) and are
assigned to a company, site or device with inheritance (device → site → company).

![Policies](../screenshots/policies.png){ .shadow }

## Checks

Each check has its own frequency, severity, output comparison and platform targeting:

- **Thresholds**: `disk`, `memory`, `cpu`, pending `updates`.
- **Script**: run a shell/PowerShell script and evaluate its result.
- **Network**: native `ping`, `tcp` port, `http` status.
- **Open ports**: alert when a device exposes a public port not on an allow-list.

Failing checks surface as a health badge in the device list and drive **alerting** and
**self-healing**.

## Tasks

Scheduled scripts with a frequency (interval / daily / weekly / …), keeping the last run
per task plus a full run history. You can also **re-run** any check or task on demand with
the ↻ button.

## Self-healing

Automatic remediation: run a script or restart a service **when a check fails** — so common
problems fix themselves before anyone gets paged.

## Scripts

A reusable **script library** (shell / PowerShell, with platform targeting) powers script
checks, tasks, self-healing and ad-hoc "run script" actions.

!!! warning "Script checks/tasks are remote code"
    Anything that runs scripts on endpoints is powerful. Grant the `Scripts` and
    `Operate devices` permissions deliberately — see
    **[Roles & permissions](permissions.md)**.
