package compliance

import (
	"context"
	"io/fs"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

const psPrefix = "powershell -NoProfile -NonInteractive -Command $ProgressPreference='SilentlyContinue'; "

func TestLinuxRulesExtended(t *testing.T) {
	bad := &fakeEnv{os: "linux",
		files: map[string]string{
			"/etc/ssh/sshd_config":                  "PermitEmptyPasswords yes\nIgnoreRhosts no\nMaxSessions 20\nLogLevel QUIET\nLoginGraceTime 2m\n",
			"/etc/security/pwquality.conf":          "minlen = 8\n",
			"/etc/pam.d/common-auth":                "auth required pam_unix.so\n",
			"/etc/login.defs":                       "PASS_MIN_DAYS 0\nPASS_WARN_AGE 3\nUMASK 022\n",
			"/etc/default/useradd":                  "INACTIVE=-1\n",
			"/etc/crontab":                          "",
			"/etc/cron.deny":                        "",
			"/proc/mounts":                          "tmpfs /tmp tmpfs rw,nosuid 0 0\ntmpfs /dev/shm tmpfs rw,nosuid,nodev,noexec 0 0\n",
			"/etc/passwd":                           "root:x:0:0::/root:/bin/bash\nthomas:x:1000:1000::/home/thomas:/bin/bash\n",
			"/proc/sys/net/ipv4/conf/all/rp_filter": "0",
			"/proc/sys/kernel/kptr_restrict":        "0",
			"/proc/modules":                         "cramfs 16384 0 - Live 0x0\next4 1 0 - Live 0x0\n",
			"/etc/systemd/journald.conf":            "[Journal]\n#Storage=auto\n",
		},
		modes: map[string]fs.FileMode{"/etc/crontab": 0o644, "/home/thomas": 0o755},
		bins:  map[string]bool{"find": true},
		cmds:  map[string]string{"find /var/log": "/var/log/syslog\n", "find / -xdev": "/opt/shared\n", "systemctl is-active rsyslog": "EXIT1:inactive"},
	}
	rep := Run(context.Background(), &shared.ComplianceSpec{Enabled: true, Profile: "server"}, bad)
	r := results(rep)
	for _, id := range []string{"ssh_permit_empty_pw", "ssh_ignore_rhosts", "ssh_max_sessions", "ssh_log_level", "ssh_login_grace", "pam_pwquality", "pam_faillock",
		"pass_min_days", "pass_warn_age", "umask_default", "inactive_lock", "cron_perms", "cron_restricted", "log_perms", "sticky_world_writable", "tmp_noexec",
		"home_perms", "sysctl_rp_filter", "sysctl_kptr_restrict", "fs_unused_modules", "journald_persistent"} {
		if r[id].Status != "fail" {
			t.Errorf("%s: %s %s (fail erwartet)", id, r[id].Status, r[id].Evidence)
		}
	}
	if r["dev_shm_noexec"].Status != "pass" {
		t.Errorf("dev_shm_noexec: %+v", r["dev_shm_noexec"])
	}
	if r["sysctl_ipv6_redirects"].Status != "na" {
		t.Errorf("ohne IPv6 n/a erwartet: %+v", r["sysctl_ipv6_redirects"])
	}
	good := &fakeEnv{os: "linux",
		files: map[string]string{
			"/etc/ssh/sshd_config":         "LoginGraceTime 30\nMaxSessions 4\nLogLevel VERBOSE\n",
			"/etc/security/pwquality.conf": "minlen = 14\n",
			"/etc/pam.d/common-auth":       "auth required pam_faillock.so preauth\n",
			"/etc/default/useradd":         "INACTIVE=30\n",
			"/etc/crontab":                 "", "/etc/cron.allow": "",
			"/proc/mounts":       "tmpfs /tmp tmpfs rw,nosuid,nodev,noexec 0 0\n",
			"/etc/passwd":        "thomas:x:1000:1000::/home/thomas:/bin/bash\n",
			"/proc/sys/net/ipv6": "", "/proc/sys/net/ipv6/conf/all/accept_redirects": "0",
			"/proc/modules": "ext4 1 0 - Live 0x0\n",
		},
		modes: map[string]fs.FileMode{"/etc/crontab": 0o600, "/home/thomas": 0o750},
		bins:  map[string]bool{"find": true},
		cmds:  map[string]string{"find /var/log": "", "find / -xdev": "", "systemctl is-active rsyslog": "active"},
	}
	r = results(Run(context.Background(), &shared.ComplianceSpec{Enabled: true, Profile: "server"}, good))
	for _, id := range []string{"ssh_login_grace", "ssh_max_sessions", "ssh_log_level", "ssh_permit_empty_pw", "pam_pwquality", "pam_faillock", "inactive_lock",
		"cron_perms", "cron_restricted", "log_perms", "sticky_world_writable", "tmp_noexec", "home_perms", "sysctl_ipv6_redirects", "fs_unused_modules", "journald_persistent"} {
		if r[id].Status != "pass" {
			t.Errorf("%s: %s %s (pass erwartet)", id, r[id].Status, r[id].Evidence)
		}
	}
}

func TestWindowsRulesExtended(t *testing.T) {
	reg := func(key, name, val string) string {
		return psPrefix + "(Get-ItemProperty -Path '" + key + "' -Name '" + name + "' -ErrorAction SilentlyContinue).'" + name + "'"
	}
	const lsa = `HKLM:\SYSTEM\CurrentControlSet\Control\Lsa`
	bad := &fakeEnv{os: "windows", cmds: map[string]string{
		psPrefix + "net accounts":            "Length of password history maintained:      None\nMaximum password age (days):                42\nMinimum password age (days):                0\nLockout duration (minutes):                 5\n",
		reg(lsa, "RestrictAnonymous", ""):    "0",
		reg(lsa, "LmCompatibilityLevel", ""): "3",
		reg(`HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon`, "AutoAdminLogon", ""):               "1",
		reg(`HKLM:\SYSTEM\CurrentControlSet\Services\LanmanServer\Parameters`, "RequireSecuritySignature", ""): "0",
		psPrefix + `(Get-WmiObject Win32_Service -Filter "Name='RemoteRegistry'").StartMode`:                   "Manual",
		psPrefix + "(Get-NetFirewallProfile":                             "Domain=True;Private=False;Public=True",
		"auditpol /get /subcategory:" + auditLogon:                       "Machine Name,Policy Target,Subcategory,Subcategory GUID,Inclusion Setting,Exclusion Setting\r\nSRV,System,Logon," + auditLogon + ",Success,\r\n",
		"bcdedit /enum {current}":                                        "Windows Boot Loader\n-------------------\nidentifier {current}\nnx OptIn\n",
		psPrefix + "(Get-WinEvent -ListLog Security).MaximumSizeInBytes": "20971520",
	}}
	r := results(Run(context.Background(), &shared.ComplianceSpec{Enabled: true, Profile: "server"}, bad))
	for _, id := range []string{"pw_history", "pw_min_age", "lockout_duration", "lsa_anonymous", "lm_compat_level", "autologon_off", "smb_signing_server", "remote_registry_disabled",
		"firewall_logging", "audit_logon", "dep_optout", "eventlog_size_security", "powershell_logging"} {
		if r[id].Status != "fail" {
			t.Errorf("%s: %s %s (fail erwartet)", id, r[id].Status, r[id].Evidence)
		}
	}
	if r["pw_max_age"].Status != "pass" {
		t.Errorf("pw_max_age 42: %+v", r["pw_max_age"])
	}
	good := &fakeEnv{os: "windows", cmds: map[string]string{
		psPrefix + "net accounts":                 "Kennwortchronik:                               24\nMaximales Kennwortalter (Tage):                90\nMinimales Kennwortalter (Tage):                1\nSperrdauer (Minuten):                          30\n",
		reg(lsa, "RestrictAnonymous", ""):         "1",
		reg(lsa, "RestrictAnonymousSAM", ""):      "1",
		reg(lsa, "EveryoneIncludesAnonymous", ""): "0",
		reg(lsa, "LmCompatibilityLevel", ""):      "5",
		reg(`HKLM:\SYSTEM\CurrentControlSet\Services\LanmanServer\Parameters`, "RequireSecuritySignature", ""):                    "1",
		reg(`HKLM:\SOFTWARE\Policies\Microsoft\Windows\PowerShell\ScriptBlockLogging`, "EnableScriptBlockLogging", ""):            "1",
		reg(`HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System\Audit`, "ProcessCreationIncludeCmdLine_Enabled", ""): "1",
		psPrefix + `(Get-WmiObject Win32_Service -Filter "Name='RemoteRegistry'").StartMode`:                                      "Disabled",
		psPrefix + "(Get-NetFirewallProfile":                             "Domain=True;Private=True;Public=True",
		"auditpol /get /subcategory:" + auditLogon:                       "Computername,Richtlinienziel,Unterkategorie,Unterkategorie-GUID,Einschlusseinstellung,Ausschlusseinstellung\r\nSRV,System,Anmelden," + auditLogon + ",Erfolg und Fehler,\r\n",
		"auditpol /get /subcategory:" + auditProcessCreate:               "a,b,c," + auditProcessCreate + ",Success,\r\n",
		"bcdedit /enum {current}":                                        "nx OptOut\n",
		psPrefix + "(Get-WinEvent -ListLog Security).MaximumSizeInBytes": "201326592",
	}}
	r = results(Run(context.Background(), &shared.ComplianceSpec{Enabled: true, Profile: "server"}, good))
	for _, id := range []string{"pw_history", "pw_max_age", "pw_min_age", "lockout_duration", "lsa_anonymous", "lm_compat_level", "smb_signing_server", "remote_registry_disabled",
		"firewall_logging", "audit_logon", "audit_process_creation", "dep_optout", "eventlog_size_security", "powershell_logging", "autologon_off", "winrm_basic_auth", "sehop_enabled"} {
		if r[id].Status != "pass" {
			t.Errorf("%s: %s %s (pass erwartet)", id, r[id].Status, r[id].Evidence)
		}
	}
}
