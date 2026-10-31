package compliance

import (
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

// sshdEffective liefert die wirksame sshd-Konfiguration als Map (kleingeschriebene
// Schlüssel): bevorzugt `sshd -T` (löst Includes/Match auf), sonst Dateien.
func sshdEffective(e Env) (map[string]string, bool) {
	cfg := map[string]string{}
	if e.HasBin("sshd") {
		if out, exit, err := e.Run("sshd", "-T"); err == nil && exit == 0 && out != "" {
			for _, l := range lines(out) {
				k, v, _ := strings.Cut(l, " ")
				cfg[strings.ToLower(k)] = strings.TrimSpace(v)
			}
			return cfg, true
		}
	}
	files := append([]string{}, e.Glob("/etc/ssh/sshd_config.d/*.conf")...)
	files = append(files, "/etc/ssh/sshd_config")
	found := false
	for _, f := range files { // erste Nennung gewinnt (Drop-ins vor Hauptdatei)
		txt, err := e.ReadFile(f)
		if err != nil {
			continue
		}
		found = true
		for _, l := range lines(txt) {
			if strings.HasPrefix(strings.ToLower(l), "match ") {
				break // Match-Blöcke nicht global werten
			}
			fs := strings.Fields(l)
			if len(fs) < 2 {
				continue
			}
			k := strings.ToLower(fs[0])
			if _, ok := cfg[k]; !ok {
				cfg[k] = strings.Join(fs[1:], " ")
			}
		}
	}
	return cfg, found
}

func sshRule(key string, check func(val string, set bool) Result) evaluator {
	return func(e Env) Result {
		if !e.Exists("/etc/ssh/sshd_config") && !e.HasBin("sshd") {
			return na("kein OpenSSH-Server")
		}
		cfg, ok := sshdEffective(e)
		if !ok {
			return errRes("sshd-Konfiguration nicht lesbar")
		}
		v, set := cfg[key]
		return check(strings.ToLower(v), set)
	}
}

func sysctl(e Env, key string) (string, bool) {
	v, err := e.ReadFile("/proc/sys/" + strings.ReplaceAll(key, ".", "/"))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(v), true
}

func sysctlRule(key, want, why string) evaluator {
	return func(e Env) Result {
		v, ok := sysctl(e, key)
		if !ok {
			return na(key + " nicht lesbar")
		}
		if v == want {
			return pass(key + " = " + v)
		}
		return fail(key + " = " + v + " (erwartet " + want + why + ")")
	}
}

func init() {
	evaluators["ssh_permit_root_login"] = sshRule("permitrootlogin", func(v string, set bool) Result {
		switch v {
		case "no", "prohibit-password", "without-password", "forced-commands-only":
			return pass("PermitRootLogin " + v)
		case "":
			return pass("PermitRootLogin nicht gesetzt (Standard prohibit-password)")
		}
		return fail("PermitRootLogin " + v)
	})
	evaluators["ssh_password_auth"] = sshRule("passwordauthentication", func(v string, set bool) Result {
		if v == "no" {
			return pass("PasswordAuthentication no")
		}
		if !set {
			return fail("PasswordAuthentication nicht gesetzt (Standard yes)")
		}
		return fail("PasswordAuthentication " + v)
	})
	evaluators["ssh_max_auth_tries"] = sshRule("maxauthtries", func(v string, set bool) Result {
		n, err := strconv.Atoi(v)
		if !set || err != nil {
			return fail("MaxAuthTries nicht gesetzt (Standard 6)")
		}
		if n <= 4 {
			return pass("MaxAuthTries " + v)
		}
		return fail("MaxAuthTries " + v)
	})
	evaluators["ssh_x11_forwarding"] = sshRule("x11forwarding", func(v string, set bool) Result {
		if v == "yes" {
			return fail("X11Forwarding yes")
		}
		return pass("X11Forwarding " + firstNonEmpty(v, "nicht gesetzt (Standard no)"))
	})
	evaluators["ssh_weak_ciphers"] = sshRule("ciphers", func(v string, set bool) Result {
		if !set {
			return pass("Ciphers: OpenSSH-Standard")
		}
		var weak []string
		for _, c := range strings.Split(v, ",") {
			c = strings.TrimSpace(c)
			if strings.Contains(c, "cbc") || strings.Contains(c, "arcfour") || strings.Contains(c, "3des") || strings.Contains(c, "blowfish") {
				weak = append(weak, c)
			}
		}
		if len(weak) > 0 {
			return fail("schwache Chiffren: " + strings.Join(weak, ", "))
		}
		return pass("Ciphers: " + v)
	})
	evaluators["shadow_perms"] = func(e Env) Result {
		mode, uid, _, ok := e.Stat("/etc/shadow")
		if !ok {
			return na("/etc/shadow nicht vorhanden")
		}
		perm := mode.Perm()
		if perm&0o007 != 0 || perm&0o020 != 0 || (uid != 0 && uid != -1) {
			return fail(fmt.Sprintf("/etc/shadow %04o uid=%d", perm, uid))
		}
		return pass(fmt.Sprintf("/etc/shadow %04o", perm))
	}
	evaluators["passwd_perms"] = func(e Env) Result {
		mode, uid, _, ok := e.Stat("/etc/passwd")
		if !ok {
			return na("/etc/passwd nicht vorhanden")
		}
		perm := mode.Perm()
		if perm&0o022 != 0 || (uid != 0 && uid != -1) {
			return fail(fmt.Sprintf("/etc/passwd %04o uid=%d", perm, uid))
		}
		return pass(fmt.Sprintf("/etc/passwd %04o", perm))
	}
	evaluators["no_empty_passwords"] = func(e Env) Result {
		txt, err := e.ReadFile("/etc/shadow")
		if err != nil {
			return na("/etc/shadow nicht lesbar (root nötig)")
		}
		var empty []string
		for _, l := range lines(txt) {
			f := strings.Split(l, ":")
			if len(f) > 1 && f[1] == "" {
				empty = append(empty, f[0])
			}
		}
		if len(empty) > 0 {
			return fail("leeres Passwort: " + strings.Join(empty, ", "))
		}
		return pass("alle Konten haben ein Passwort oder sind gesperrt")
	}
	evaluators["single_uid0"] = func(e Env) Result {
		txt, err := e.ReadFile("/etc/passwd")
		if err != nil {
			return errRes("/etc/passwd nicht lesbar")
		}
		var extra []string
		for _, l := range lines(txt) {
			f := strings.Split(l, ":")
			if len(f) > 2 && f[2] == "0" && f[0] != "root" {
				extra = append(extra, f[0])
			}
		}
		if len(extra) > 0 {
			return fail("weitere UID-0-Konten: " + strings.Join(extra, ", "))
		}
		return pass("nur root hat UID 0")
	}
	evaluators["sudo_nopasswd_all"] = func(e Env) Result {
		files := append([]string{"/etc/sudoers"}, e.Glob("/etc/sudoers.d/*")...)
		var hits []string
		readable := false
		for _, f := range files {
			txt, err := e.ReadFile(f)
			if err != nil {
				continue
			}
			readable = true
			for _, l := range lines(txt) {
				u := strings.ToUpper(l)
				if strings.Contains(u, "NOPASSWD") && strings.HasSuffix(strings.TrimSpace(u), "ALL") && !strings.HasPrefix(u, "DEFAULTS") {
					hits = append(hits, f+": "+l)
				}
			}
		}
		if !readable {
			return na("sudoers nicht lesbar (root nötig)")
		}
		if len(hits) > 0 {
			return fail(strings.Join(hits, "; "))
		}
		return pass("keine NOPASSWD: ALL-Regel")
	}
	evaluators["pass_max_days"] = func(e Env) Result {
		txt, err := e.ReadFile("/etc/login.defs")
		if err != nil {
			return na("/etc/login.defs fehlt")
		}
		for _, l := range lines(txt) {
			f := strings.Fields(l)
			if len(f) >= 2 && f[0] == "PASS_MAX_DAYS" {
				n, err := strconv.Atoi(f[1])
				if err == nil && n <= 365 {
					return pass("PASS_MAX_DAYS " + f[1])
				}
				return fail("PASS_MAX_DAYS " + f[1])
			}
		}
		return fail("PASS_MAX_DAYS nicht gesetzt (Standard 99999)")
	}
	evaluators["firewall_active"] = func(e Env) Result {
		if e.HasBin("ufw") {
			if out, _, _ := e.Run("ufw", "status"); strings.Contains(out, "Status: active") {
				return pass("ufw aktiv")
			}
		}
		if e.HasBin("firewall-cmd") || e.HasBin("systemctl") {
			if out, exit, _ := e.Run("systemctl", "is-active", "firewalld"); exit == 0 && strings.TrimSpace(out) == "active" {
				return pass("firewalld aktiv")
			}
		}
		if e.HasBin("nft") {
			if out, exit, _ := e.Run("nft", "list", "ruleset"); exit == 0 && strings.Contains(out, "hook input") {
				return pass("nftables-Regelwerk mit input-Hook")
			}
		}
		if e.HasBin("iptables") {
			if out, exit, _ := e.Run("iptables", "-S", "INPUT"); exit == 0 {
				if strings.Contains(out, "-P INPUT DROP") || strings.Contains(out, "-P INPUT REJECT") || len(lines(out)) > 3 {
					return pass("iptables-Regeln für INPUT")
				}
			}
		}
		if !e.HasBin("ufw") && !e.HasBin("firewall-cmd") && !e.HasBin("nft") && !e.HasBin("iptables") {
			return fail("keine Firewall installiert (ufw/firewalld/nftables/iptables)")
		}
		return fail("keine aktive Firewall-Regel für eingehenden Verkehr")
	}
	evaluators["auditd_running"] = func(e Env) Result {
		if !e.HasBin("auditd") && !e.Exists("/sbin/auditd") && !e.Exists("/usr/sbin/auditd") {
			return fail("auditd nicht installiert")
		}
		if out, exit, _ := e.Run("systemctl", "is-active", "auditd"); exit == 0 && strings.TrimSpace(out) == "active" {
			return pass("auditd aktiv")
		}
		return fail("auditd nicht aktiv")
	}
	evaluators["ntp_active"] = func(e Env) Result {
		if e.HasBin("timedatectl") {
			if out, _, _ := e.Run("timedatectl", "show", "-p", "NTPSynchronized", "--value"); strings.TrimSpace(out) == "yes" {
				return pass("NTP synchronisiert (timedatectl)")
			}
		}
		for _, unit := range []string{"chronyd", "chrony", "systemd-timesyncd", "ntpd", "ntp"} {
			if out, exit, _ := e.Run("systemctl", "is-active", unit); exit == 0 && strings.TrimSpace(out) == "active" {
				return pass(unit + " aktiv")
			}
		}
		return fail("kein aktiver Zeitdienst")
	}
	evaluators["mac_enforcing"] = func(e Env) Result {
		if e.HasBin("getenforce") {
			if out, _, _ := e.Run("getenforce"); strings.EqualFold(strings.TrimSpace(out), "Enforcing") {
				return pass("SELinux enforcing")
			} else if out != "" {
				return fail("SELinux " + strings.TrimSpace(out))
			}
		}
		if v, err := e.ReadFile("/sys/module/apparmor/parameters/enabled"); err == nil && strings.TrimSpace(v) == "Y" {
			if p, err := e.ReadFile("/sys/kernel/security/apparmor/profiles"); err == nil && strings.Contains(p, "enforce") {
				return pass("AppArmor mit Enforce-Profilen")
			}
			return fail("AppArmor aktiv, aber keine Enforce-Profile geladen")
		}
		return fail("weder SELinux noch AppArmor aktiv")
	}
	evaluators["aslr"] = sysctlRule("kernel.randomize_va_space", "2", "")
	evaluators["ip_forward_off"] = func(e Env) Result {
		v, ok := sysctl(e, "net.ipv4.ip_forward")
		if !ok {
			return na("nicht lesbar")
		}
		if v == "0" {
			return pass("net.ipv4.ip_forward = 0")
		}
		if e.HasBin("docker") || e.HasBin("pvesh") || e.HasBin("libvirtd") || e.HasBin("podman") || e.Exists("/etc/wireguard") {
			return na("ip_forward = 1, aber Container-/Virtualisierungs-/VPN-Host")
		}
		return fail("net.ipv4.ip_forward = 1")
	}
	evaluators["icmp_redirects_off"] = sysctlRule("net.ipv4.conf.all.accept_redirects", "0", "")
	evaluators["tcp_syncookies"] = sysctlRule("net.ipv4.tcp_syncookies", "1", "")
	evaluators["world_writable_etc"] = func(e Env) Result {
		out, _, err := e.Run("find", "/etc", "-xdev", "-type", "f", "-perm", "-0002")
		if err != nil {
			return errRes("find nicht ausführbar")
		}
		if ls := lines(out); len(ls) > 0 {
			if len(ls) > 5 {
				ls = append(ls[:5], fmt.Sprintf("… (+%d)", len(ls)-5))
			}
			return fail(strings.Join(ls, ", "))
		}
		return pass("keine weltweit beschreibbaren Dateien in /etc")
	}
	evaluators["legacy_services"] = func(e Env) Result {
		// Binaries allein sind kein Befund (inetutils liefert sie auf Arch mit) – entscheidend
		// ist ein aktiver Dienst/Socket.
		var active []string
		for _, unit := range []string{"telnet.socket", "telnet.service", "telnetd.service", "rsh.socket", "rlogin.socket", "rexec.socket", "xinetd.service", "inetd.service", "ypserv.service"} {
			if out, exit, _ := e.Run("systemctl", "is-active", unit); exit == 0 && strings.TrimSpace(out) == "active" {
				active = append(active, unit)
			}
		}
		if len(active) > 0 {
			return fail("aktiv: " + strings.Join(active, ", "))
		}
		var present []string
		for _, b := range []string{"in.telnetd", "telnetd", "in.rshd", "rshd", "in.rlogind", "rlogind"} {
			if e.HasBin(b) {
				present = append(present, b)
			}
		}
		if len(present) > 0 {
			return pass("installiert, aber kein aktiver Dienst: " + strings.Join(present, ", "))
		}
		return pass("keine telnet/rsh/rlogin-Dienste")
	}
	evaluators["core_dumps_restricted"] = sysctlRule("fs.suid_dumpable", "0", "")
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var _ fs.FileMode // Import für Stat-Signatur
