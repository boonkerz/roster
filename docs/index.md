---
title: Roster
hide:
  - navigation
  - toc
---

<div class="rs-hero" markdown>

<div class="rs-hero-text" markdown>

# Your fleet, one binary. { .rs-title }

**Roster** is a self-hosted, TacticalRMM-style **inventory & remote-management**
platform for Windows, Linux and macOS. One Go server with the web UI and all agents
embedded, a lightweight agent per machine, nothing else to install.

[Get started](getting-started.md){ .md-button .md-button--primary }
[GitHub](https://github.com/boonkerz/roster){ .md-button }

```bash
curl -LO https://github.com/boonkerz/roster/releases/latest/download/roster-server-linux-amd64
chmod +x roster-server-linux-amd64 && ./roster-server-linux-amd64 run
```

<p class="rs-hero-note">MIT-licensed · SQLite out of the box, PostgreSQL optional · UI in English and German</p>

</div>

<div class="rs-hero-shot" markdown>
![Roster dashboard](screenshots/dashboard.png){ .shadow }
</div>

</div>

<div class="rs-pillars" markdown>

- :material-cube-outline: **One binary, self-hosted.** No external services, no second daemon. Put it behind your reverse proxy or let it serve TLS itself.
- :material-monitor-cellphone: **Cross-platform agents** for Windows, Linux, macOS and FreeBSD, with auto-update on every new server release.
- :material-remote-desktop: **Real remote control built in.** Browser or native viewer, interactive terminal and file browser over one on-demand tunnel.
- :material-shield-account: **Fine-grained access.** Custom roles plus per-user company and site scope, two-factor login, audit log.

</div>

## Everything an RMM needs { .rs-center }

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Device list with custom columns and filter](screenshots/devices-filter.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Inventory that answers questions
Hardware, OS, network, disks, software, printers, temperatures, Docker containers:
collected automatically and kept current on every check-in. Add your own fields, pick
the columns you need and build filters on any of them, saved per user.
[Inventory](features/inventory.md){ .rs-more }
</div>
</section>

<section class="rs-feature rs-reverse" markdown>
<div class="rs-feature-shot" markdown>
![Check history dialog](screenshots/check-history.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Checks, tasks and self-healing
Disk, memory, CPU, updates, ping, TCP, HTTP, open ports, services, certificates,
temperatures and your own scripts. Every result is kept as history, a click opens the
chart. Failing checks can run a remediation script before anyone is alerted.
[Checks & tasks](features/checks-tasks.md){ .rs-more }
</div>
</section>

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Interactive terminal in the browser](screenshots/terminal.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Remote desktop, terminal and files
Take over a screen in the browser or in the native viewer, with live resolution switch
and a real Ctrl+Alt+Del. Open an interactive shell or browse the file system over the
same tunnel. No third-party VNC software, nothing exposed on the device.
[Remote desktop & terminal](features/remote.md){ .rs-more }
</div>
</section>

<section class="rs-feature rs-reverse" markdown>
<div class="rs-feature-shot" markdown>
![Dashboard with configurable charts](screenshots/dashboard-dark.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### A dashboard you configure
Donuts, bars and history lines over any metric, check status or custom field value.
Drag tiles into place, pick their width, and watch hourly snapshots build up a year of
history. Light and dark theme included.
[Dashboard](features/dashboard.md){ .rs-more }
</div>
</section>

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Roles and data scope](screenshots/data-scope.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Roles and data scope
Build custom roles from individual permissions and limit any user to specific companies
or sites. Two-factor authentication, API tokens for the iOS app and an audit log are
built in.
[Roles & permissions](features/permissions.md){ .rs-more }
</div>
</section>

<section class="rs-feature rs-reverse" markdown>
<div class="rs-feature-shot" markdown>
![Policies with checks and tasks](screenshots/policy-detail.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Policies, patching and software
Assign checks, tasks and backups to companies, sites or single devices. Scan, approve
and install updates, deploy packages, track CVEs of installed software.
[Patching & software](features/patching.md){ .rs-more }
</div>
</section>

<section class="rs-feature" markdown>
<div class="rs-feature-shot" markdown>
![Network scan](screenshots/network-scan.png){ .shadow }
</div>
<div class="rs-feature-text" markdown>
### Network discovery and more
Scan a CIDR range, read printer details via SNMP, adopt unmanaged hosts. Proxmox VE
hosts report their guests and backup state, scheduled backups run with start
conditions and follow-up actions, and a taskbar app keeps the fleet one click away.
[Network & discovery](features/network.md){ .rs-more } · [Proxmox VE](features/proxmox.md){ .rs-more } · [Taskbar app](features/tray-app.md){ .rs-more }
</div>
</section>

<div class="rs-cta" markdown>

## Ready in a couple of minutes { .rs-center }

Download the server binary, run it, enroll the first agent with the generated one-liner.
The **[Getting started](getting-started.md)** guide walks you through build, run,
enrollment and organizing your fleet.

[Get started](getting-started.md){ .md-button .md-button--primary }
[Configuration](configuration.md){ .md-button }

</div>
