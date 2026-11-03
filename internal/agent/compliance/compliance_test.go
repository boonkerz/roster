package compliance

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

// fakeEnv simuliert Dateien, Befehle und Binaries.
type fakeEnv struct {
	os      string
	files   map[string]string
	modes   map[string]fs.FileMode
	bins    map[string]bool
	cmds    map[string]string // "name arg1 arg2" (Präfix) -> Ausgabe
	autoUpd string
	reboot  bool
}

func (f *fakeEnv) OS() string { return f.os }
func (f *fakeEnv) ReadFile(p string) (string, error) {
	s, ok := f.files[p]
	if !ok {
		return "", fs.ErrNotExist
	}
	return s, nil
}
func (f *fakeEnv) Exists(p string) bool { _, ok := f.files[p]; return ok }
func (f *fakeEnv) Glob(pattern string) []string {
	var out []string
	dir := strings.TrimSuffix(pattern, "*")
	dir = strings.TrimSuffix(dir, "*.conf")
	for p := range f.files {
		if strings.HasPrefix(p, dir) && p != dir {
			out = append(out, p)
		}
	}
	return out
}
func (f *fakeEnv) Stat(p string) (fs.FileMode, int, int, bool) {
	m, ok := f.modes[p]
	return m, 0, 0, ok
}
func (f *fakeEnv) Run(name string, args ...string) (string, int, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	for k, v := range f.cmds {
		if strings.HasPrefix(key, k) {
			if strings.HasPrefix(v, "EXIT1:") {
				return strings.TrimPrefix(v, "EXIT1:"), 1, nil
			}
			return v, 0, nil
		}
	}
	return "", 127, fs.ErrNotExist
}
func (f *fakeEnv) HasBin(n string) bool        { return f.bins[n] }
func (f *fakeEnv) AutoUpdates() string         { return f.autoUpd }
func (f *fakeEnv) RebootPending() (bool, bool) { return f.reboot, true }

func results(rep shared.ComplianceReport) map[string]shared.ComplianceResult {
	m := map[string]shared.ComplianceResult{}
	for _, r := range rep.Results {
		m[r.RuleID] = r
	}
	return m
}

func TestLinuxRules(t *testing.T) {
	env := &fakeEnv{os: "linux",
		files: map[string]string{
			"/etc/ssh/sshd_config":                         "PermitRootLogin yes\nMaxAuthTries 3\nCiphers aes128-cbc,chacha20-poly1305@openssh.com\n",
			"/etc/shadow":                                  "root:$6$x:1::::::\nevil::1::::::\n",
			"/etc/passwd":                                  "root:x:0:0::/root:/bin/bash\ntoor:x:0:0::/root:/bin/bash\nbob:x:1000:1000::/home/bob:/bin/bash\n",
			"/etc/sudoers":                                 "root ALL=(ALL) ALL\n%wheel ALL=(ALL) NOPASSWD: ALL\n",
			"/etc/login.defs":                              "PASS_MAX_DAYS\t99999\n",
			"/proc/sys/kernel/randomize_va_space":          "2\n",
			"/proc/sys/net/ipv4/ip_forward":                "1\n",
			"/proc/sys/net/ipv4/tcp_syncookies":            "1\n",
			"/proc/sys/net/ipv4/conf/all/accept_redirects": "1\n",
			"/proc/sys/fs/suid_dumpable":                   "0\n",
		},
		modes: map[string]fs.FileMode{"/etc/shadow": 0o644, "/etc/passwd": 0o644},
		bins:  map[string]bool{"ufw": true, "systemctl": true, "timedatectl": true, "docker": true},
		cmds: map[string]string{
			"ufw status":                          "Status: inactive",
			"systemctl is-active firewalld":       "EXIT1:inactive",
			"systemctl is-active auditd":          "EXIT1:inactive",
			"systemctl is-active chronyd":         "active",
			"timedatectl show -p NTPSynchronized": "no",
			"find /etc -xdev -type f -perm -0002": "/etc/bad.conf",
		},
		autoUpd: "security", reboot: true,
	}
	rep := Run(context.Background(), &shared.ComplianceSpec{Enabled: true, Profile: "server"}, env)
	r := results(rep)
	want := map[string]string{
		"ssh_permit_root_login": "fail", "ssh_password_auth": "fail", "ssh_max_auth_tries": "pass", "ssh_x11_forwarding": "pass", "ssh_weak_ciphers": "fail",
		"shadow_perms": "fail", "passwd_perms": "pass", "no_empty_passwords": "fail", "single_uid0": "fail", "sudo_nopasswd_all": "fail", "pass_max_days": "fail",
		"firewall_active": "fail", "auto_updates_enabled": "pass", "auditd_running": "fail", "ntp_active": "pass", "mac_enforcing": "fail",
		"aslr": "pass", "ip_forward_off": "na", "icmp_redirects_off": "fail", "tcp_syncookies": "pass", "world_writable_etc": "fail",
		"legacy_services": "pass", "core_dumps_restricted": "pass", "reboot_not_pending": "fail",
	}
	for id, st := range want {
		got, ok := r[id]
		if !ok {
			t.Errorf("%s fehlt", id)
			continue
		}
		if got.Status != st {
			t.Errorf("%s: got %s (%s) want %s", id, got.Status, got.Evidence, st)
		}
	}
	if _, ok := r["uac_enabled"]; ok {
		t.Error("Windows-Regel darf unter Linux nicht laufen")
	}
	if r["no_empty_passwords"].Evidence != "leeres Passwort: evil" || r["single_uid0"].Evidence != "weitere UID-0-Konten: toor" {
		t.Errorf("evidence: %+v %+v", r["no_empty_passwords"], r["single_uid0"])
	}
	score, p, f := Score(rep.Results)
	if p+f == 0 || score > 50 {
		t.Errorf("score %d (%d/%d)", score, p, f)
	}
	// Profil workstation: Server-only-Regeln fehlen; Disabled greift.
	rep = Run(context.Background(), &shared.ComplianceSpec{Enabled: true, Profile: "workstation", Disabled: []string{"aslr"}}, env)
	r = results(rep)
	if _, ok := r["ssh_password_auth"]; ok {
		t.Error("server-only Regel im workstation-Profil")
	}
	if _, ok := r["aslr"]; ok {
		t.Error("abgeschaltete Regel wurde geprüft")
	}
	// Gehärtetes System: alles grün.
	good := &fakeEnv{os: "linux",
		files: map[string]string{
			"/etc/ssh/sshd_config": "PermitRootLogin no\nPasswordAuthentication no\nMaxAuthTries 4\nClientAliveInterval 300\nClientAliveCountMax 3\nLoginGraceTime 60\n",
			"/etc/shadow":          "root:!:1::::::\n", "/etc/passwd": "root:x:0:0::/root:/bin/bash\n", "/etc/sudoers": "root ALL=(ALL) ALL\n",
			"/etc/login.defs": "PASS_MAX_DAYS 365\nPASS_MIN_DAYS 1\nPASS_WARN_AGE 7\nUMASK 027\n", "/var/log/journal": "",
			"/proc/sys/kernel/randomize_va_space": "2", "/proc/sys/net/ipv4/ip_forward": "0", "/proc/sys/net/ipv4/tcp_syncookies": "1",
			"/proc/sys/net/ipv4/conf/all/accept_redirects": "0", "/proc/sys/fs/suid_dumpable": "0",
			"/sys/module/apparmor/parameters/enabled": "Y\n", "/sys/kernel/security/apparmor/profiles": "/usr/sbin/sshd (enforce)\n",
		},
		modes: map[string]fs.FileMode{"/etc/shadow": 0o640, "/etc/passwd": 0o644},
		bins:  map[string]bool{"nft": true, "systemctl": true, "timedatectl": true, "auditd": true},
		cmds: map[string]string{"nft list ruleset": "table inet filter {\n chain input { type filter hook input priority 0; policy drop; }\n}",
			"systemctl is-active auditd": "active", "timedatectl show -p NTPSynchronized": "yes", "find /etc": ""},
		autoUpd: "enabled",
	}
	rep = Run(context.Background(), &shared.ComplianceSpec{Enabled: true, Profile: "server"}, good)
	if score, _, f := Score(rep.Results); score != 100 || f != 0 {
		for _, x := range rep.Results {
			if x.Status == "fail" {
				t.Logf("fail: %s %s", x.RuleID, x.Evidence)
			}
		}
		t.Errorf("gehärtet: score %d, fails %d", score, f)
	}
}

func TestWindowsRules(t *testing.T) {
	env := &fakeEnv{os: "windows", bins: map[string]bool{}, cmds: map[string]string{
		"powershell -NoProfile -NonInteractive -Command $ProgressPreference='SilentlyContinue'; (Get-ItemProperty -Path 'HKLM:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Policies\\System' -Name 'EnableLUA'":                  "1",
		"powershell -NoProfile -NonInteractive -Command $ProgressPreference='SilentlyContinue'; (Get-ItemProperty -Path 'HKLM:\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Policies\\System' -Name 'ConsentPromptBehaviorAdmin'": "5",
		"powershell -NoProfile -NonInteractive -Command $ProgressPreference='SilentlyContinue'; (Get-SmbServerConfiguration).EnableSMB1Protocol":                                                                                    "True",
		"powershell -NoProfile -NonInteractive -Command $ProgressPreference='SilentlyContinue'; (Get-NetFirewallProfile":                                                                                                            "Domain=True;Private=True;Public=False",
		"powershell -NoProfile -NonInteractive -Command $ProgressPreference='SilentlyContinue'; net accounts":                                                                                                                       "Minimum password length:                              8\nLockout threshold:                                    Never\n",
		"powershell -NoProfile -NonInteractive -Command $ProgressPreference='SilentlyContinue'; (Get-ItemProperty -Path 'HKLM:\\SYSTEM\\CurrentControlSet\\Control\\SecurityProviders\\WDigest' -Name 'UseLogonCredential'":         "",
	}, autoUpd: "enabled"}
	rep := Run(context.Background(), &shared.ComplianceSpec{Enabled: true, Profile: "workstation"}, env)
	r := results(rep)
	want := map[string]string{"uac_enabled": "pass", "smb1_disabled": "fail", "firewall_all_profiles": "fail", "password_min_length": "fail", "lockout_threshold": "fail", "wdigest_disabled": "pass", "auto_updates_enabled": "pass", "rdp_disabled_workstation": "fail"}
	for id, st := range want {
		if got := r[id]; got.Status != st {
			t.Errorf("%s: got %s (%s) want %s", id, got.Status, got.Evidence, st)
		}
	}
	if _, ok := r["ssh_permit_root_login"]; ok {
		t.Error("Linux-Regel unter Windows")
	}
}
