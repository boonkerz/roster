// Package compliance beschreibt den Regelkatalog der Compliance-/Baseline-Prüfung
// (CIS-artig). Die Metadaten teilen sich Server (API/UI) und Agent (Auswertung); die
// Evaluatoren selbst liegen im Agent (internal/agent/compliance).
package compliance

// Rule sind die Metadaten einer Regel.
type Rule struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	TitleEN     string   `json:"title_en"`
	Description string   `json:"description"`
	Severity    string   `json:"severity"`  // low | medium | high
	Category    string   `json:"category"`  // auth | network | fs | updates | hardening | crypto
	Platforms   []string `json:"platforms"` // linux | windows | darwin
	Profiles    []string `json:"profiles"`  // leer = server + workstation
	Remediation string   `json:"remediation"`
}

var (
	lin = []string{"linux"}
	win = []string{"windows"}
	mac = []string{"darwin"}
	all = []string{"linux", "windows", "darwin"}
	srv = []string{"server"}
	wks = []string{"workstation"}
)

// Catalog ist der vollständige Regelkatalog (Reihenfolge = Anzeige).
var Catalog = []Rule{
	// --- Linux: SSH ---
	{ID: "ssh_permit_root_login", Title: "SSH: Root-Login verboten", TitleEN: "SSH: root login disabled", Description: "PermitRootLogin ist no oder prohibit-password.", Severity: "high", Category: "auth", Platforms: lin, Remediation: "In /etc/ssh/sshd_config: PermitRootLogin prohibit-password (oder no), dann sshd neu laden."},
	{ID: "ssh_password_auth", Title: "SSH: Passwort-Anmeldung aus", TitleEN: "SSH: password authentication off", Description: "PasswordAuthentication ist no (nur Schlüssel).", Severity: "medium", Category: "auth", Platforms: lin, Profiles: srv, Remediation: "PasswordAuthentication no setzen, vorher Schlüssel hinterlegen."},
	{ID: "ssh_max_auth_tries", Title: "SSH: MaxAuthTries ≤ 4", TitleEN: "SSH: MaxAuthTries ≤ 4", Description: "Begrenzt Anmeldeversuche je Verbindung.", Severity: "low", Category: "auth", Platforms: lin, Remediation: "MaxAuthTries 4 in sshd_config."},
	{ID: "ssh_x11_forwarding", Title: "SSH: X11-Forwarding aus", TitleEN: "SSH: X11 forwarding off", Description: "X11Forwarding ist no.", Severity: "low", Category: "hardening", Platforms: lin, Profiles: srv, Remediation: "X11Forwarding no in sshd_config."},
	{ID: "ssh_weak_ciphers", Title: "SSH: keine schwachen Chiffren", TitleEN: "SSH: no weak ciphers", Description: "Keine CBC-/arcfour-Chiffren und kein SHA1-only-MAC in der wirksamen Konfiguration.", Severity: "medium", Category: "crypto", Platforms: lin, Remediation: "Ciphers/MACs in sshd_config auf moderne Verfahren beschränken (z. B. chacha20-poly1305, aes-gcm; hmac-sha2-512-etm)."},
	// --- Linux: Konten & Dateirechte ---
	{ID: "shadow_perms", Title: "/etc/shadow geschützt", TitleEN: "/etc/shadow protected", Description: "Keine Rechte für „andere“, kein Gruppen-Schreibrecht, Eigentümer root.", Severity: "high", Category: "fs", Platforms: lin, Remediation: "chown root:shadow /etc/shadow && chmod 640 /etc/shadow"},
	{ID: "passwd_perms", Title: "/etc/passwd nicht beschreibbar", TitleEN: "/etc/passwd not writable", Description: "Nur root darf schreiben (0644).", Severity: "high", Category: "fs", Platforms: lin, Remediation: "chown root:root /etc/passwd && chmod 644 /etc/passwd"},
	{ID: "no_empty_passwords", Title: "Keine leeren Passwörter", TitleEN: "No empty passwords", Description: "Kein Konto in /etc/shadow hat ein leeres Passwortfeld.", Severity: "high", Category: "auth", Platforms: lin, Remediation: "passwd -l <user> oder Passwort setzen."},
	{ID: "single_uid0", Title: "Nur root hat UID 0", TitleEN: "Only root has UID 0", Description: "Kein weiteres Konto mit UID 0.", Severity: "high", Category: "auth", Platforms: lin, Remediation: "Fremde UID-0-Konten entfernen oder UID ändern."},
	{ID: "sudo_nopasswd_all", Title: "Kein sudo NOPASSWD: ALL", TitleEN: "No sudo NOPASSWD: ALL", Description: "Keine Regel erlaubt beliebige Befehle ohne Passwort.", Severity: "medium", Category: "auth", Platforms: lin, Remediation: "NOPASSWD-Regeln in /etc/sudoers(.d) auf einzelne Befehle beschränken."},
	{ID: "pass_max_days", Title: "Passwort-Höchstalter ≤ 365 Tage", TitleEN: "Password max age ≤ 365 days", Description: "PASS_MAX_DAYS in /etc/login.defs.", Severity: "low", Category: "auth", Platforms: lin, Remediation: "PASS_MAX_DAYS 365 in /etc/login.defs."},
	// --- Linux: Netzwerk & Härtung ---
	{ID: "firewall_active", Title: "Firewall aktiv", TitleEN: "Firewall active", Description: "ufw, firewalld, nftables oder iptables filtern eingehenden Verkehr.", Severity: "high", Category: "network", Platforms: lin, Remediation: "ufw enable bzw. firewalld starten oder ein nftables-Regelwerk laden."},
	{ID: "auto_updates_enabled", Title: "Automatische Updates aktiv", TitleEN: "Automatic updates enabled", Description: "unattended-upgrades, dnf-automatic, Windows Update bzw. macOS installieren Updates automatisch (mindestens Sicherheit).", Severity: "medium", Category: "updates", Platforms: all, Remediation: "Linux: unattended-upgrades / dnf-automatic einrichten; Windows: Automatische Updates aktivieren; macOS: Softwareupdate automatisch installieren."},
	{ID: "auditd_running", Title: "auditd läuft", TitleEN: "auditd running", Description: "Das Audit-Subsystem protokolliert sicherheitsrelevante Ereignisse.", Severity: "medium", Category: "hardening", Platforms: lin, Profiles: srv, Remediation: "auditd installieren und aktivieren (systemctl enable --now auditd)."},
	{ID: "ntp_active", Title: "Zeitsynchronisation aktiv", TitleEN: "Time synchronization active", Description: "NTP (timesyncd, chrony, ntpd) hält die Uhr synchron.", Severity: "low", Category: "hardening", Platforms: lin, Remediation: "timedatectl set-ntp true bzw. chrony aktivieren."},
	{ID: "mac_enforcing", Title: "SELinux/AppArmor aktiv", TitleEN: "SELinux/AppArmor enforcing", Description: "Ein Mandatory-Access-Control-System ist im Enforcing-Modus.", Severity: "medium", Category: "hardening", Platforms: lin, Profiles: srv, Remediation: "SELinux auf enforcing setzen bzw. AppArmor aktivieren."},
	{ID: "aslr", Title: "ASLR aktiv", TitleEN: "ASLR enabled", Description: "kernel.randomize_va_space = 2.", Severity: "medium", Category: "hardening", Platforms: lin, Remediation: "sysctl -w kernel.randomize_va_space=2 (persistent in /etc/sysctl.d)."},
	{ID: "ip_forward_off", Title: "IP-Forwarding aus", TitleEN: "IP forwarding off", Description: "net.ipv4.ip_forward = 0 (außer auf Routern/Virtualisierungs-Hosts).", Severity: "low", Category: "network", Platforms: lin, Remediation: "sysctl -w net.ipv4.ip_forward=0, falls das System nicht routet."},
	{ID: "icmp_redirects_off", Title: "ICMP-Redirects ignoriert", TitleEN: "ICMP redirects ignored", Description: "net.ipv4.conf.all.accept_redirects = 0.", Severity: "low", Category: "network", Platforms: lin, Remediation: "sysctl -w net.ipv4.conf.all.accept_redirects=0"},
	{ID: "tcp_syncookies", Title: "TCP-SYN-Cookies aktiv", TitleEN: "TCP SYN cookies enabled", Description: "net.ipv4.tcp_syncookies = 1.", Severity: "low", Category: "network", Platforms: lin, Remediation: "sysctl -w net.ipv4.tcp_syncookies=1"},
	{ID: "world_writable_etc", Title: "Keine weltweit beschreibbaren Dateien in /etc", TitleEN: "No world-writable files in /etc", Description: "Konfigurationsdateien dürfen nicht von jedem geändert werden.", Severity: "medium", Category: "fs", Platforms: lin, Remediation: "chmod o-w auf die genannten Dateien."},
	{ID: "legacy_services", Title: "Keine Klartext-Dienste (telnet/rsh)", TitleEN: "No legacy clear-text services", Description: "telnetd, rshd, rlogind sind nicht installiert.", Severity: "high", Category: "network", Platforms: lin, Remediation: "Pakete telnetd/rsh-server entfernen."},
	{ID: "core_dumps_restricted", Title: "SUID-Core-Dumps aus", TitleEN: "SUID core dumps disabled", Description: "fs.suid_dumpable = 0.", Severity: "low", Category: "hardening", Platforms: lin, Remediation: "sysctl -w fs.suid_dumpable=0"},
	{ID: "reboot_not_pending", Title: "Kein Neustart ausstehend", TitleEN: "No reboot pending", Description: "Updates sind vollständig aktiv (kein ausstehender Neustart).", Severity: "low", Category: "updates", Platforms: all, Remediation: "Gerät neu starten."},
	// --- Windows ---
	{ID: "uac_enabled", Title: "UAC aktiv", TitleEN: "UAC enabled", Description: "EnableLUA = 1 und Admin-Bestätigung nicht abgeschaltet.", Severity: "high", Category: "hardening", Platforms: win, Remediation: "Benutzerkontensteuerung aktivieren (Gruppenrichtlinie oder Systemsteuerung)."},
	{ID: "smb1_disabled", Title: "SMBv1 deaktiviert", TitleEN: "SMBv1 disabled", Description: "Der SMB1-Server ist abgeschaltet.", Severity: "high", Category: "network", Platforms: win, Remediation: "Set-SmbServerConfiguration -EnableSMB1Protocol $false"},
	{ID: "rdp_nla", Title: "RDP nur mit NLA", TitleEN: "RDP requires NLA", Description: "Ist RDP aktiv, ist Authentifizierung auf Netzwerkebene Pflicht.", Severity: "high", Category: "auth", Platforms: win, Remediation: "Systemeigenschaften → Remote → „Nur mit Authentifizierung auf Netzwerkebene“."},
	{ID: "rdp_disabled_workstation", Title: "RDP deaktiviert", TitleEN: "RDP disabled", Description: "Auf Arbeitsplätzen ist Remotedesktop aus.", Severity: "medium", Category: "network", Platforms: win, Profiles: wks, Remediation: "Remotedesktop in den Systemeigenschaften deaktivieren."},
	{ID: "guest_disabled", Title: "Gastkonto deaktiviert", TitleEN: "Guest account disabled", Description: "Das lokale Gastkonto ist aus.", Severity: "medium", Category: "auth", Platforms: win, Remediation: "Disable-LocalUser Guest"},
	{ID: "builtin_admin_disabled", Title: "Eingebauter Administrator aus/umbenannt", TitleEN: "Built-in Administrator disabled/renamed", Description: "Das Konto mit RID 500 ist deaktiviert oder umbenannt.", Severity: "medium", Category: "auth", Platforms: win, Remediation: "Konto deaktivieren oder umbenennen (Rename-LocalUser)."},
	{ID: "firewall_all_profiles", Title: "Windows-Firewall in allen Profilen an", TitleEN: "Windows Firewall on for all profiles", Description: "Domäne, Privat und Öffentlich sind aktiviert.", Severity: "high", Category: "network", Platforms: win, Remediation: "Set-NetFirewallProfile -All -Enabled True"},
	{ID: "defender_realtime", Title: "Defender-Echtzeitschutz an", TitleEN: "Defender real-time protection on", Description: "Microsoft Defender läuft mit Echtzeitschutz (oder ein anderer Virenschutz ist registriert).", Severity: "high", Category: "hardening", Platforms: win, Remediation: "Echtzeitschutz in Windows-Sicherheit aktivieren."},
	{ID: "bitlocker_system_drive", Title: "Systemlaufwerk mit BitLocker verschlüsselt", TitleEN: "System drive BitLocker-encrypted", Description: "Schutz des Systemlaufwerks ist aktiv.", Severity: "high", Category: "crypto", Platforms: win, Profiles: wks, Remediation: "BitLocker für das Systemlaufwerk aktivieren (TPM)."},
	{ID: "autoplay_disabled", Title: "AutoPlay deaktiviert", TitleEN: "AutoPlay disabled", Description: "NoDriveTypeAutoRun = 255 für alle Laufwerke.", Severity: "low", Category: "hardening", Platforms: win, Remediation: "Gruppenrichtlinie „Automatische Wiedergabe deaktivieren“ für alle Laufwerke."},
	{ID: "llmnr_disabled", Title: "LLMNR deaktiviert", TitleEN: "LLMNR disabled", Description: "Multicast-Namensauflösung ist abgeschaltet (Schutz vor Responder-Angriffen).", Severity: "medium", Category: "network", Platforms: win, Remediation: "Richtlinie „Multicastnamensauflösung deaktivieren“ aktivieren (EnableMulticast=0)."},
	{ID: "password_min_length", Title: "Mindestpasswortlänge ≥ 12", TitleEN: "Minimum password length ≥ 12", Description: "Lokale Kennwortrichtlinie.", Severity: "medium", Category: "auth", Platforms: win, Remediation: "net accounts /minpwlen:12"},
	{ID: "lockout_threshold", Title: "Kontosperre nach ≤ 10 Fehlversuchen", TitleEN: "Lockout after ≤ 10 failed attempts", Description: "Kontosperrungsschwelle ist gesetzt.", Severity: "medium", Category: "auth", Platforms: win, Remediation: "net accounts /lockoutthreshold:10"},
	{ID: "powershell_v2_disabled", Title: "PowerShell 2.0 deaktiviert", TitleEN: "PowerShell 2.0 disabled", Description: "Das Feature MicrosoftWindowsPowerShellV2 ist aus (Downgrade-Schutz).", Severity: "medium", Category: "hardening", Platforms: win, Remediation: "Disable-WindowsOptionalFeature -Online -FeatureName MicrosoftWindowsPowerShellV2Root"},
	{ID: "wdigest_disabled", Title: "WDigest-Klartext-Cache aus", TitleEN: "WDigest credential caching off", Description: "UseLogonCredential = 0.", Severity: "high", Category: "crypto", Platforms: win, Remediation: "HKLM\\SYSTEM\\CurrentControlSet\\Control\\SecurityProviders\\WDigest UseLogonCredential = 0"},
	{ID: "lsa_ppl", Title: "LSA-Schutz (RunAsPPL) aktiv", TitleEN: "LSA protection (RunAsPPL) enabled", Description: "Schützt lsass vor Credential-Dumping.", Severity: "medium", Category: "hardening", Platforms: win, Remediation: "HKLM\\SYSTEM\\CurrentControlSet\\Control\\Lsa RunAsPPL = 1 und Neustart."},
	// --- macOS ---
	{ID: "filevault_on", Title: "FileVault aktiv", TitleEN: "FileVault on", Description: "Die Systemfestplatte ist verschlüsselt.", Severity: "high", Category: "crypto", Platforms: mac, Remediation: "Systemeinstellungen → Datenschutz & Sicherheit → FileVault aktivieren."},
	{ID: "firewall_on", Title: "Firewall aktiv", TitleEN: "Firewall on", Description: "Die Anwendungs-Firewall ist eingeschaltet.", Severity: "high", Category: "network", Platforms: mac, Remediation: "socketfilterfw --setglobalstate on"},
	{ID: "gatekeeper_on", Title: "Gatekeeper aktiv", TitleEN: "Gatekeeper on", Description: "spctl-Bewertungen sind aktiviert.", Severity: "medium", Category: "hardening", Platforms: mac, Remediation: "spctl --master-enable"},
	{ID: "sip_enabled", Title: "System Integrity Protection aktiv", TitleEN: "System Integrity Protection enabled", Description: "csrutil status: enabled.", Severity: "high", Category: "hardening", Platforms: mac, Remediation: "Im Recovery-Modus: csrutil enable"},
	{ID: "screensaver_password", Title: "Passwort nach Bildschirmschoner", TitleEN: "Password after screen saver", Description: "askForPassword = 1.", Severity: "low", Category: "auth", Platforms: mac, Profiles: wks, Remediation: "Systemeinstellungen → Sperrbildschirm → Passwort sofort anfordern."},
	{ID: "guest_disabled", Title: "Gastbenutzer deaktiviert", TitleEN: "Guest user disabled", Description: "GuestEnabled = 0.", Severity: "medium", Category: "auth", Platforms: mac, Remediation: "Systemeinstellungen → Benutzer → Gastbenutzer aus."},
	{ID: "remote_login_off", Title: "Entfernte Anmeldung (SSH) aus", TitleEN: "Remote login (SSH) off", Description: "Auf Arbeitsplätzen ist Remote Login deaktiviert.", Severity: "low", Category: "network", Platforms: mac, Profiles: wks, Remediation: "systemsetup -setremotelogin off"},
	{ID: "stealth_mode", Title: "Firewall-Tarnmodus aktiv", TitleEN: "Firewall stealth mode on", Description: "Das System antwortet nicht auf Ping/Portscans.", Severity: "low", Category: "network", Platforms: mac, Remediation: "socketfilterfw --setstealthmode on"},
}

// Applies meldet, ob eine Regel für Plattform und Profil gilt.
func (r Rule) Applies(platform, profile string) bool {
	okP := false
	for _, p := range r.Platforms {
		if p == platform {
			okP = true
			break
		}
	}
	if !okP {
		return false
	}
	if len(r.Profiles) == 0 {
		return true
	}
	for _, p := range r.Profiles {
		if p == profile {
			return true
		}
	}
	return false
}

// ByID liefert eine Regel.
func ByID(id string) (Rule, bool) {
	for _, r := range Catalog {
		if r.ID == id {
			return r, true
		}
	}
	return Rule{}, false
}
