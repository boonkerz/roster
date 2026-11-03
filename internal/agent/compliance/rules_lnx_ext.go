package compliance

import (
	"fmt"
	"strconv"
	"strings"
)

// Erweiterte Linux-Regeln (0.19): SSH-Details, PAM, Passwort-Alterung, Dateisystem,
// sysctl, Protokollierung, Kernelmodule. Fehlende Dateien → n/a (hardened Testsystem
// bleibt bei 100 %), fehlende Pflichtwerte in vorhandenen Dateien → fail.

// loginDefs liest einen Schlüssel aus /etc/login.defs ("" = nicht gesetzt).
func loginDefs(e Env, key string) (string, bool, error) {
	txt, err := e.ReadFile("/etc/login.defs")
	if err != nil {
		return "", false, err
	}
	for _, l := range lines(txt) {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == key {
			return f[1], true, nil
		}
	}
	return "", false, nil
}

// mountOpts liefert die Optionen eines Einhängepunkts aus /proc/mounts.
func mountOpts(e Env, mountpoint string) (string, bool) {
	txt, err := e.ReadFile("/proc/mounts")
	if err != nil {
		return "", false
	}
	for _, l := range strings.Split(txt, "\n") {
		f := strings.Fields(l)
		if len(f) >= 4 && f[1] == mountpoint {
			return f[3], true
		}
	}
	return "", false
}

func hasOpts(opts string, want ...string) []string {
	set := map[string]bool{}
	for _, o := range strings.Split(opts, ",") {
		set[o] = true
	}
	var missing []string
	for _, w := range want {
		if !set[w] {
			missing = append(missing, w)
		}
	}
	return missing
}

func loadedModules(e Env) map[string]bool {
	txt, err := e.ReadFile("/proc/modules")
	out := map[string]bool{}
	if err != nil {
		return out
	}
	for _, l := range strings.Split(txt, "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			out[f[0]] = true
		}
	}
	return out
}

// loginDefsMin prüft einen numerischen login.defs-Wert gegen ein Minimum.
func loginDefsMin(key string, min int) evaluator {
	return func(e Env) Result {
		v, set, err := loginDefs(e, key)
		if err != nil {
			return na("/etc/login.defs fehlt")
		}
		if !set {
			return fail(key + " nicht gesetzt")
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < min {
			return fail(key + " " + v)
		}
		return pass(key + " " + v)
	}
}

func init() {
	// --- SSH ---
	evaluators["ssh_client_alive"] = func(e Env) Result {
		if !e.Exists("/etc/ssh/sshd_config") && !e.HasBin("sshd") {
			return na("kein OpenSSH-Server")
		}
		cfg, ok := sshdEffective(e)
		if !ok {
			return errRes("sshd-Konfiguration nicht lesbar")
		}
		iv, _ := strconv.Atoi(cfg["clientaliveinterval"])
		cm, set := cfg["clientalivecountmax"]
		cmn := 3
		if set {
			cmn, _ = strconv.Atoi(cm)
		}
		if iv < 1 || iv > 300 || cmn > 3 {
			return fail(fmt.Sprintf("ClientAliveInterval %d, ClientAliveCountMax %d", iv, cmn))
		}
		return pass(fmt.Sprintf("ClientAliveInterval %d, ClientAliveCountMax %d", iv, cmn))
	}
	evaluators["ssh_login_grace"] = sshRule("logingracetime", func(v string, set bool) Result {
		if !set {
			return fail("LoginGraceTime nicht gesetzt (Standard 120 s)")
		}
		n, err := strconv.Atoi(strings.TrimSuffix(v, "s"))
		if strings.HasSuffix(v, "m") {
			if m, err2 := strconv.Atoi(strings.TrimSuffix(v, "m")); err2 == nil {
				n, err = m*60, nil
			}
		}
		if err != nil || n > 60 || n <= 0 {
			return fail("LoginGraceTime " + v)
		}
		return pass("LoginGraceTime " + v)
	})
	evaluators["ssh_permit_empty_pw"] = sshRule("permitemptypasswords", func(v string, set bool) Result {
		if set && v == "yes" {
			return fail("PermitEmptyPasswords yes")
		}
		return pass("PermitEmptyPasswords no")
	})
	evaluators["ssh_ignore_rhosts"] = sshRule("ignorerhosts", func(v string, set bool) Result {
		if set && v == "no" {
			return fail("IgnoreRhosts no")
		}
		return pass("IgnoreRhosts yes")
	})
	evaluators["ssh_max_sessions"] = sshRule("maxsessions", func(v string, set bool) Result {
		if !set {
			return pass("MaxSessions 10 (Standard)")
		}
		if n, err := strconv.Atoi(v); err != nil || n > 10 {
			return fail("MaxSessions " + v)
		}
		return pass("MaxSessions " + v)
	})
	evaluators["ssh_log_level"] = sshRule("loglevel", func(v string, set bool) Result {
		if !set || v == "info" || v == "verbose" {
			return pass("LogLevel " + firstNonEmpty(strings.ToUpper(v), "INFO"))
		}
		return fail("LogLevel " + v)
	})
	// --- PAM / Passwörter ---
	evaluators["pam_pwquality"] = func(e Env) Result {
		if txt, err := e.ReadFile("/etc/security/pwquality.conf"); err == nil {
			for _, l := range lines(txt) {
				k, v, ok := strings.Cut(l, "=")
				if ok && strings.TrimSpace(k) == "minlen" {
					n, _ := strconv.Atoi(strings.TrimSpace(v))
					if n >= 14 {
						return pass("minlen = " + strings.TrimSpace(v))
					}
					return fail("minlen = " + strings.TrimSpace(v))
				}
			}
		}
		for _, p := range []string{"/etc/pam.d/common-password", "/etc/pam.d/system-auth", "/etc/pam.d/password-auth"} {
			txt, err := e.ReadFile(p)
			if err != nil {
				continue
			}
			for _, l := range lines(txt) {
				if strings.Contains(l, "pam_pwquality") || strings.Contains(l, "pam_cracklib") {
					if i := strings.Index(l, "minlen="); i >= 0 {
						n, _ := strconv.Atoi(strings.Fields(l[i+7:] + " ")[0])
						if n >= 14 {
							return pass(p + ": minlen=" + strconv.Itoa(n))
						}
						return fail(p + ": minlen=" + strconv.Itoa(n))
					}
					return fail(p + ": pam_pwquality ohne minlen (Standard 8)")
				}
			}
			return fail(p + ": pam_pwquality nicht eingebunden")
		}
		return na("keine PAM-Passwortkonfiguration gefunden")
	}
	evaluators["pam_faillock"] = func(e Env) Result {
		found := false
		for _, p := range []string{"/etc/pam.d/common-auth", "/etc/pam.d/system-auth", "/etc/pam.d/password-auth"} {
			txt, err := e.ReadFile(p)
			if err != nil {
				continue
			}
			found = true
			if strings.Contains(txt, "pam_faillock") || strings.Contains(txt, "pam_tally2") {
				return pass(p + ": pam_faillock")
			}
		}
		if !found {
			return na("keine PAM-Anmeldekonfiguration gefunden")
		}
		return fail("pam_faillock/pam_tally2 nicht eingebunden")
	}
	evaluators["pass_min_days"] = loginDefsMin("PASS_MIN_DAYS", 1)
	evaluators["pass_warn_age"] = loginDefsMin("PASS_WARN_AGE", 7)
	evaluators["inactive_lock"] = func(e Env) Result {
		txt, err := e.ReadFile("/etc/default/useradd")
		if err != nil {
			return na("/etc/default/useradd fehlt")
		}
		for _, l := range lines(txt) {
			if k, v, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == "INACTIVE" {
				n, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil || n < 1 || n > 30 {
					return fail("INACTIVE=" + strings.TrimSpace(v))
				}
				return pass("INACTIVE=" + strings.TrimSpace(v))
			}
		}
		return fail("INACTIVE nicht gesetzt (-1 = nie)")
	}
	evaluators["umask_default"] = func(e Env) Result {
		v, set, err := loginDefs(e, "UMASK")
		if err != nil {
			return na("/etc/login.defs fehlt")
		}
		if !set {
			return fail("UMASK nicht gesetzt")
		}
		n, err := strconv.ParseInt(v, 8, 32)
		if err != nil || n&0o027 != 0o027 {
			return fail("UMASK " + v)
		}
		return pass("UMASK " + v)
	}
	// --- Dateisystem ---
	evaluators["cron_perms"] = func(e Env) Result {
		var bad []string
		checked := 0
		for _, p := range []string{"/etc/crontab", "/etc/cron.d", "/etc/cron.hourly", "/etc/cron.daily", "/etc/cron.weekly", "/etc/cron.monthly"} {
			mode, uid, _, ok := e.Stat(p)
			if !ok {
				continue
			}
			checked++
			if mode.Perm()&0o077 != 0 || (uid != 0 && uid != -1) {
				bad = append(bad, fmt.Sprintf("%s %04o", p, mode.Perm()))
			}
		}
		if checked == 0 {
			return na("kein cron")
		}
		if len(bad) > 0 {
			return fail(strings.Join(bad, ", "))
		}
		return pass(fmt.Sprintf("%d Pfade geschützt", checked))
	}
	evaluators["cron_restricted"] = func(e Env) Result {
		if !e.Exists("/etc/crontab") && !e.Exists("/etc/cron.d") {
			return na("kein cron")
		}
		if e.Exists("/etc/cron.deny") {
			return fail("/etc/cron.deny vorhanden")
		}
		if !e.Exists("/etc/cron.allow") {
			return fail("/etc/cron.allow fehlt")
		}
		return pass("/etc/cron.allow vorhanden, kein cron.deny")
	}
	evaluators["log_perms"] = func(e Env) Result {
		if !e.HasBin("find") {
			return na("find fehlt")
		}
		out, exit, err := e.Run("find", "/var/log", "-type", "f", "-perm", "/o+r", "-not", "-path", "*/journal/*")
		if err != nil || exit != 0 {
			return na("find nicht ausführbar")
		}
		files := lines(out)
		if len(files) > 0 {
			if len(files) > 5 {
				files = append(files[:5], "…")
			}
			return fail("weltlesbar: " + strings.Join(files, ", "))
		}
		return pass("keine weltlesbaren Dateien unter /var/log")
	}
	evaluators["sticky_world_writable"] = func(e Env) Result {
		if !e.HasBin("find") {
			return na("find fehlt")
		}
		out, exit, err := e.Run("find", "/", "-xdev", "-type", "d", "-perm", "-0002", "!", "-perm", "-1000")
		if err != nil || exit != 0 {
			return na("find nicht ausführbar")
		}
		dirs := lines(out)
		if len(dirs) > 0 {
			if len(dirs) > 5 {
				dirs = append(dirs[:5], "…")
			}
			return fail("ohne Sticky-Bit: " + strings.Join(dirs, ", "))
		}
		return pass("alle weltbeschreibbaren Verzeichnisse mit Sticky-Bit")
	}
	mountRule := func(mp string) evaluator {
		return func(e Env) Result {
			opts, ok := mountOpts(e, mp)
			if !ok {
				return na(mp + " ist kein eigener Einhängepunkt")
			}
			if missing := hasOpts(opts, "nosuid", "nodev", "noexec"); len(missing) > 0 {
				return fail(mp + " ohne " + strings.Join(missing, ","))
			}
			return pass(mp + " " + opts)
		}
	}
	evaluators["tmp_noexec"] = mountRule("/tmp")
	evaluators["dev_shm_noexec"] = mountRule("/dev/shm")
	evaluators["home_perms"] = func(e Env) Result {
		txt, err := e.ReadFile("/etc/passwd")
		if err != nil {
			return na("/etc/passwd fehlt")
		}
		var bad []string
		checked := 0
		for _, l := range lines(txt) {
			f := strings.Split(l, ":")
			if len(f) < 7 {
				continue
			}
			uid, _ := strconv.Atoi(f[2])
			if uid < 1000 || uid >= 65534 || !strings.HasPrefix(f[5], "/home/") {
				continue
			}
			mode, _, _, ok := e.Stat(f[5])
			if !ok {
				continue
			}
			checked++
			if mode.Perm()&0o027 != 0 {
				bad = append(bad, fmt.Sprintf("%s %04o", f[5], mode.Perm()))
			}
		}
		if checked == 0 {
			return na("keine Benutzer-Home-Verzeichnisse")
		}
		if len(bad) > 0 {
			return fail(strings.Join(bad, ", "))
		}
		return pass(fmt.Sprintf("%d Home-Verzeichnisse ≤ 750", checked))
	}
	// --- sysctl ---
	evaluators["sysctl_rp_filter"] = sysctlRule("net.ipv4.conf.all.rp_filter", "1", "")
	evaluators["sysctl_log_martians"] = sysctlRule("net.ipv4.conf.all.log_martians", "1", "")
	evaluators["sysctl_send_redirects"] = sysctlRule("net.ipv4.conf.all.send_redirects", "0", "")
	evaluators["sysctl_source_route"] = sysctlRule("net.ipv4.conf.all.accept_source_route", "0", "")
	evaluators["sysctl_ipv6_redirects"] = func(e Env) Result {
		if !e.Exists("/proc/sys/net/ipv6") {
			return na("kein IPv6")
		}
		return sysctlRule("net.ipv6.conf.all.accept_redirects", "0", "")(e)
	}
	evaluators["sysctl_icmp_broadcast"] = sysctlRule("net.ipv4.icmp_echo_ignore_broadcasts", "1", "")
	evaluators["sysctl_kptr_restrict"] = func(e Env) Result {
		v, ok := sysctl(e, "kernel.kptr_restrict")
		if !ok {
			return na("kernel.kptr_restrict nicht lesbar")
		}
		if v == "0" {
			return fail("kernel.kptr_restrict = 0")
		}
		return pass("kernel.kptr_restrict = " + v)
	}
	// --- Protokollierung / Module ---
	evaluators["journald_persistent"] = func(e Env) Result {
		if e.Exists("/var/log/journal") {
			return pass("/var/log/journal vorhanden")
		}
		for _, p := range append([]string{"/etc/systemd/journald.conf"}, e.Glob("/etc/systemd/journald.conf.d/*.conf")...) {
			txt, err := e.ReadFile(p)
			if err != nil {
				continue
			}
			for _, l := range lines(txt) {
				if k, v, ok := strings.Cut(l, "="); ok && strings.TrimSpace(k) == "Storage" && strings.TrimSpace(v) == "persistent" {
					return pass(p + ": Storage=persistent")
				}
			}
		}
		if _, exit, err := e.Run("systemctl", "is-active", "rsyslog"); err == nil && exit == 0 {
			return pass("rsyslog aktiv")
		}
		return fail("journald nicht persistent und kein rsyslog")
	}
	evaluators["fs_unused_modules"] = func(e Env) Result {
		loaded := loadedModules(e)
		var bad []string
		for _, m := range []string{"cramfs", "freevxfs", "jffs2", "hfs", "hfsplus", "udf"} {
			if loaded[m] {
				bad = append(bad, m)
			}
		}
		if len(bad) > 0 {
			return fail("geladen: " + strings.Join(bad, ", "))
		}
		return pass("keine unnötigen Dateisystemmodule geladen")
	}
}
