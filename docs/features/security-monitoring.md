# Security monitoring

Roster adds host-based security monitoring in the spirit of Wazuh: file integrity, login
monitoring with incident detection, CIS-style compliance checks and automatic responses. All
of it is configured in the **Security monitoring** section of a policy and shown on the
**Security** page and in the device's "Security" tab group.

!!! note "Prerequisites"
    The agent must run as **root** (Linux/macOS) or **LocalSystem** (Windows) – the install
    scripts do that; a legacy systemd unit with `DynamicUser=yes` cannot read `/var/log/auth.log`,
    `/etc/shadow` or manage nftables. Login monitoring on Windows needs logon auditing
    (success/failure) enabled, which is the default on servers.

![Policy section “Security monitoring”](../screenshots/policy-security.png){ .shadow }

## File integrity (FIM)

![File integrity events](../screenshots/security-fim.png){ .shadow }

Add **monitored paths** to a policy: a file, a directory (optionally recursive) or a glob such
as `/home/*/.ssh/authorized_keys`, with exclusion globs (`*.log`, `cache/`), an optional platform
restriction and an "alert" switch. *Insert default paths* adds the usual suspects per platform
(`/etc`, `/usr/local/bin`, `~/.ssh`, cron spools; Windows `drivers\etc`, scheduled tasks,
Startup folder; macOS LaunchDaemons/LaunchAgents).

The agent keeps a baseline (SHA-256, permissions, owner, mtime) in its state directory
(`/var/lib/roster/fim-baseline.json`, `C:\ProgramData\Roster\fim-baseline.json`). The first
run only creates the baseline; afterwards every scan (interval per policy, default 60 min)
reports **created**, **modified**, **deleted** and **permission/owner** changes. Changes of
rules with the alert switch are sent as one bundled alert per check-in and create a
`fim_change` incident. Files larger than 256 MB are compared by size and mtime only; a scan is
capped at 50 000 entries.

Device tab **File integrity**: event list with acknowledge, *Scan now* and *Reset baseline*.
Security page tab **File integrity**: fleet-wide list with filters.

## Login monitoring & incidents

![Security incidents](../screenshots/security-incidents.png){ .shadow }

With **Monitor logins** enabled the agent reads new events incrementally:

| Platform | Source | Events |
|---|---|---|
| Linux | journald (cursor) or `/var/log/auth.log` / `secure` (offset) | sshd failed/accepted/invalid user, sudo, su, useradd/userdel/usermod/gpasswd/passwd, console login |
| Windows | Security log (RecordId cursor) | 4624/4625 (logon types 2/3/7/10/11), 4740 lockout, 4720/4726 user created/deleted, 4722/4725/4767, 4728/4732/4756 group membership, 4723/4724 password |
| macOS | unified log (`log show`) | sshd, sudo, su, login |

The server derives **incidents** from these events:

- **Brute force** – N failed logins from one source IP within the window (default 10 in 10 min).
- **Login after brute force** – a successful login from an IP that just brute-forced (critical).
- **New source IP** – a user's first successful login from an unknown address, after a learning
  phase (default 7 days of monitoring); private addresses can be ignored.
- **Account change** – user created/deleted, group membership changed (admin groups such as
  sudo/wheel/Administrators as warning), password changed.
- **Lockout** – account locked out.

Warnings and critical incidents go through the normal alert channels (maintenance windows
and per-channel severity apply). The **Incidents** tab lists them with acknowledge/resolve,
filters and the top source IPs; **Logins** shows the raw events. Thresholds, the learning
phase and the never-block list live in Settings → **Security monitoring**.

## Compliance (baseline) checks

![Compliance scores across the fleet](../screenshots/security-compliance.png){ .shadow }

Enable **Compliance** in a policy with a profile (*server* or *workstation*), an interval
(default 24 h) and an optional rule selection. The agent evaluates a CIS-style catalog and
reports pass/fail/n/a with evidence; the **score** is passed / (passed + failed) · 100.

Rule families: Linux – SSH hardening (root login, password auth, MaxAuthTries, X11, weak
ciphers), `/etc/shadow` and `/etc/passwd` permissions, empty passwords, extra UID 0, sudo
`NOPASSWD: ALL`, password aging, firewall active, auditd, NTP, SELinux/AppArmor, ASLR, IP
forwarding, ICMP redirects, SYN cookies, world-writable files in `/etc`, telnet/rsh services,
core dumps. Windows – UAC, SMBv1, RDP NLA / RDP disabled, guest and built-in Administrator,
firewall profiles, Defender real-time, BitLocker system drive, AutoPlay, LLMNR, password
length, lockout threshold, PowerShell 2.0, WDigest, LSA protection. macOS – FileVault,
firewall and stealth mode, Gatekeeper, SIP, screen-saver password, guest, remote login. All
platforms – automatic updates enabled, no reboot pending. Each rule carries a remediation hint.

Results appear in the device tab **Compliance** and on the Security page (by device and by
rule, with the failing devices per rule). The check type **Compliance score** (minimum score,
optionally fail on any high-severity rule) plugs the result into alerting and self-healing.

## Active response

A policy can react to incidents automatically:

| Trigger | Actions |
|---|---|
| Brute force, login after brute force, new source IP | block source IP, run script |
| Login after brute force, account change, lockout, new source IP | lock account, run script |
| File change | run script |

- **Block IP** – Linux: nftables set `inet roster blocked4/6` with a timeout (iptables
  fallback with agent-side expiry); Windows: inbound firewall rule `Roster-Block <ip>`; macOS:
  pf table `roster_block` (add `table <roster_block> persist` and
  `block in quick from <roster_block>` to `/etc/pf.conf`). Duration in minutes, 0 = permanent.
- **Lock account** – `usermod -L`, `Disable-LocalUser`, `pwpolicy -disableuser`. root and
  Administrator are never locked.
- **Run script** – a library script with `ROSTER_TRIGGER`, `ROSTER_SOURCE_IP`, `ROSTER_USER`,
  `ROSTER_DEVICE`, `ROSTER_INCIDENT` and `ROSTER_TITLE` in the environment.

Safeguards: the **never-block list** (IPs/CIDRs) and a **cooldown** per device and action in
Settings → Security monitoring; loopback and the Roster server address are always protected.
Every reaction is written to the audit log and shown at the incident with its result. The
**Blocks** tab lists all blocks (status, expiry, release) and lets you block an IP by hand
(device, IP, duration from one hour to permanent, reason); incidents offer *Block IP* with the
same duration choice and *Lock account*. Blocking an IP that is already blocked does not create
a duplicate: the existing block is renewed with the new duration, so a temporary block can be
made permanent in place.
