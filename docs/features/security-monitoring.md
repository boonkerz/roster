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

### Log sources: nginx, SFTP and FTP containers, custom patterns

Besides the system's own authentication log a policy can list **log sources** the agent reads
incrementally every minute: a **file** (globs allowed, e.g. `/var/log/nginx/*access.log`) or the
output of a **Docker container** (exact name or pattern such as `*sftp*`; the agent reads the
json-file log directly and falls back to `docker logs --since`). Each source has a format:

| Format | Produces |
|---|---|
| Web access log (nginx, Apache, Traefik – combined log, also with a leading `$host`) | `401` → failed login; scanner paths (`wp-login.php`, `/.env`, `phpmyadmin`, `xmlrpc.php`, `../`, `union select` …), `400`/`444` and other malformed requests, `404` on `.php/.sql/.bak/.zip` → **suspicious request** |
| Web error log (nginx, Apache) | `password mismatch`, `authentication failure`, `user … not found` → failed login; `access forbidden by rule`, `client denied`, `Invalid URI`, ModSecurity denials, rate limiting, missing scanner files → suspicious request |
| sshd (SSH/SFTP servers or containers) | the same events as the host's sshd: failed/accepted logins, invalid users |
| FTP (vsftpd, proftpd, pure-ftpd) | failed/successful logins, unknown users |
| Mail (Postfix SASL, Dovecot, Exim) | failed/successful IMAP/POP3/SMTP logins |
| custom pattern | a regular expression with `(?P<ip>…)` and optionally `(?P<user>…)`; each match counts as a failed login or a suspicious request |

Events carry the source's name as service (e.g. `web`, `sftp`, `mail`), so the **Logins** tab,
the incident texts and the blocks show where an attacker was seen. Templates fill the form for
the common cases: web server access/error log as a file or in a Docker container, SSH/SFTP
container, FTP server, mail server, custom pattern.

The server derives **incidents** from these events:

- **Brute force** – N failed logins from one source IP within the window (default 10 in 10 min),
  **or** N attempts with non-existent user names within 24 hours (default 5, 0 = off). The second
  rule catches distributed, slow attacks that never reach the per-window threshold from one IP.
- **Attack pattern** – N suspicious requests from one source IP within the window (default 20),
  e.g. a web scanner walking through WordPress and phpMyAdmin paths on nginx.
- **Login after brute force** – a successful login from an IP that just brute-forced (critical).
- **New source IP** – a user's first successful login from an unknown address, after a learning
  phase (default 7 days of monitoring); private addresses can be ignored.
- **Account change** – user created/deleted, group membership changed (admin groups such as
  sudo/wheel/Administrators as warning), password changed.
- **Lockout** – account locked out.

Warnings and critical incidents go through the normal alert channels (maintenance windows
and per-channel severity apply). Which kinds are sent is a per-policy choice in the
**Notifications** tab (*Report security incidents*: one checkbox per kind), and *do not report
when the source IP was blocked automatically* keeps the channels quiet when active response or
automatic blocking already handled the attacker. Incidents are always recorded on the Security
page regardless of these switches. The **Incidents** tab lists them with acknowledge/resolve,
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
| Brute force, login after brute force, new source IP, attack pattern | block source IP, run script |
| Login after brute force, account change, lockout, new source IP | lock account, run script |
| File change | run script |

- **Block IP** – Linux: nftables sets `inet roster blocked4/6` (single addresses) and `net4/6`
  (networks) with a timeout, dropped in the **input and forward** hooks so that traffic to Docker
  containers with published ports is blocked as well (iptables fallback: `INPUT`, `FORWARD` and
  `DOCKER-USER`, expiry handled by the agent); Windows: inbound firewall rule `Roster-Block <ip>`;
  macOS: pf table `roster_block` (add `table <roster_block> persist` and
  `block in quick from <roster_block>` to `/etc/pf.conf`). Duration in minutes, 0 = permanent.
  A target can be a single IP or a network in CIDR notation.
- **Lock account** – `usermod -L`, `Disable-LocalUser`, `pwpolicy -disableuser`. root and
  Administrator are never locked.
- **Run script** – a library script with `ROSTER_TRIGGER`, `ROSTER_SOURCE_IP`, `ROSTER_USER`,
  `ROSTER_DEVICE`, `ROSTER_INCIDENT` and `ROSTER_TITLE` in the environment.

### Automatic blocking without policy rules

For fleets with many servers the per-policy rules are optional: Settings → **Security
monitoring** → *Automatic blocking* blocks the source IP of every brute force, login after brute
force and attack pattern on the device that reported it, with a configurable duration (one hour
to permanent). A policy's own active-response rule for the same trigger takes precedence. Two
options widen the effect:

- **Fleet blocking** – the address is blocked on every device whose policy monitors logins or
  reads log sources, not only where it was seen. Devices that already block it are skipped.
- **Subnet blocking** – if at least N different suspicious IPs from the same IPv4 `/24` showed up
  on the device within 24 hours (default off), the whole network is blocked instead of the single
  address. A network that contains a never-block address is never blocked as a whole.

In the **Incidents** tab you can also select many incidents at once and block all their source
IPs with one click (duration and fleet option as above); the incidents are acknowledged in the
same step.

Safeguards: the **never-block list** (IPs/CIDRs) and a **cooldown** per device and action in
Settings → Security monitoring; loopback and the Roster server address are always protected.
Every reaction is written to the audit log and shown at the incident with its result. The
**Blocks** tab lists all blocks (status, expiry, release) and lets you block an IP or a network
by hand (device, IP or CIDR, duration from one hour to permanent, reason, optionally on all
monitored devices); incidents offer *Block IP* with the
same duration choice and *Lock account*. Blocking an IP that is already blocked does not create
a duplicate: the existing block is renewed with the new duration, so a temporary block can be
made permanent in place.
