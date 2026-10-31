package compliance

import (
	"regexp"
	"strconv"
	"strings"
)

// ps führt ein PowerShell-Skript aus (Ausgabe getrimmt).
func ps(e Env, script string) (string, bool) {
	out, exit, err := e.Run("powershell", "-NoProfile", "-NonInteractive", "-Command", "$ProgressPreference='SilentlyContinue'; "+script)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), exit == 0
}

// reg liest einen Registry-Wert ("" wenn nicht vorhanden).
func reg(e Env, key, name string) string {
	out, _ := ps(e, `(Get-ItemProperty -Path '`+key+`' -Name '`+name+`' -ErrorAction SilentlyContinue).'`+name+`'`)
	return out
}

func init() {
	evaluators["uac_enabled"] = func(e Env) Result {
		k := `HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`
		lua, prompt := reg(e, k, "EnableLUA"), reg(e, k, "ConsentPromptBehaviorAdmin")
		if lua == "" {
			return errRes("Registry nicht lesbar")
		}
		if lua != "1" {
			return fail("EnableLUA = " + lua)
		}
		if prompt == "0" {
			return fail("ConsentPromptBehaviorAdmin = 0 (keine Nachfrage)")
		}
		return pass("EnableLUA = 1, ConsentPromptBehaviorAdmin = " + firstNonEmpty(prompt, "Standard"))
	}
	evaluators["smb1_disabled"] = func(e Env) Result {
		out, ok := ps(e, `(Get-SmbServerConfiguration).EnableSMB1Protocol`)
		if !ok || out == "" {
			return na("SMB-Serverkonfiguration nicht lesbar")
		}
		if strings.EqualFold(out, "False") {
			return pass("EnableSMB1Protocol = False")
		}
		return fail("EnableSMB1Protocol = " + out)
	}
	evaluators["rdp_nla"] = func(e Env) Result {
		deny := reg(e, `HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server`, "fDenyTSConnections")
		if deny == "1" {
			return na("RDP deaktiviert")
		}
		nla := reg(e, `HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server\WinStations\RDP-Tcp`, "UserAuthentication")
		if nla == "1" {
			return pass("UserAuthentication = 1 (NLA)")
		}
		return fail("UserAuthentication = " + firstNonEmpty(nla, "0"))
	}
	evaluators["rdp_disabled_workstation"] = func(e Env) Result {
		deny := reg(e, `HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server`, "fDenyTSConnections")
		if deny == "1" {
			return pass("RDP deaktiviert")
		}
		return fail("RDP aktiv (fDenyTSConnections = " + firstNonEmpty(deny, "0") + ")")
	}
	evaluators["guest_disabled"] = func(e Env) Result {
		out, ok := ps(e, `(Get-LocalUser | Where-Object { $_.SID -like '*-501' }).Enabled`)
		if !ok || out == "" {
			return na("Gastkonto nicht ermittelbar")
		}
		if strings.EqualFold(out, "False") {
			return pass("Gastkonto deaktiviert")
		}
		return fail("Gastkonto aktiv")
	}
	evaluators["builtin_admin_disabled"] = func(e Env) Result {
		out, ok := ps(e, `$u = Get-LocalUser | Where-Object { $_.SID -like '*-500' }; "$($u.Enabled)|$($u.Name)"`)
		if !ok || out == "" || out == "|" {
			return na("Administratorkonto nicht ermittelbar")
		}
		enabled, name, _ := strings.Cut(out, "|")
		if strings.EqualFold(enabled, "False") {
			return pass("Konto deaktiviert (" + name + ")")
		}
		if !strings.EqualFold(name, "Administrator") {
			return pass("Konto umbenannt (" + name + ")")
		}
		return fail("Administrator aktiv und nicht umbenannt")
	}
	evaluators["firewall_all_profiles"] = func(e Env) Result {
		out, ok := ps(e, `(Get-NetFirewallProfile | ForEach-Object { "$($_.Name)=$($_.Enabled)" }) -join ';'`)
		if !ok || out == "" {
			return errRes("Firewall-Profile nicht lesbar")
		}
		if strings.Contains(out, "=False") {
			return fail(out)
		}
		return pass(out)
	}
	evaluators["defender_realtime"] = func(e Env) Result {
		out, ok := ps(e, `try { (Get-MpComputerStatus).RealTimeProtectionEnabled } catch { 'n/a' }`)
		if !ok || out == "" || out == "n/a" {
			other, _ := ps(e, `(Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct -ErrorAction SilentlyContinue | Select-Object -First 1).displayName`)
			if other != "" {
				return pass("Virenschutz: " + other)
			}
			return na("Defender-Status nicht lesbar")
		}
		if strings.EqualFold(out, "True") {
			return pass("Echtzeitschutz an")
		}
		return fail("Echtzeitschutz aus")
	}
	evaluators["bitlocker_system_drive"] = func(e Env) Result {
		out, ok := ps(e, `try { (Get-BitLockerVolume -MountPoint $env:SystemDrive).ProtectionStatus } catch { 'n/a' }`)
		if !ok || out == "" || out == "n/a" {
			return na("BitLocker nicht verfügbar")
		}
		if strings.EqualFold(out, "On") || out == "1" {
			return pass("ProtectionStatus On")
		}
		return fail("ProtectionStatus " + out)
	}
	evaluators["autoplay_disabled"] = func(e Env) Result {
		v := reg(e, `HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Explorer`, "NoDriveTypeAutoRun")
		if v == "255" {
			return pass("NoDriveTypeAutoRun = 255")
		}
		return fail("NoDriveTypeAutoRun = " + firstNonEmpty(v, "nicht gesetzt"))
	}
	evaluators["llmnr_disabled"] = func(e Env) Result {
		v := reg(e, `HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\DNSClient`, "EnableMulticast")
		if v == "0" {
			return pass("EnableMulticast = 0")
		}
		return fail("EnableMulticast = " + firstNonEmpty(v, "nicht gesetzt (LLMNR aktiv)"))
	}
	evaluators["password_min_length"] = func(e Env) Result {
		n, ok := netAccountsNumber(e, []string{"minimum password length", "minimale kennwortlänge"})
		if !ok {
			return na("net accounts nicht auswertbar")
		}
		if n >= 12 {
			return pass("Mindestlänge " + strconv.Itoa(n))
		}
		return fail("Mindestlänge " + strconv.Itoa(n))
	}
	evaluators["lockout_threshold"] = func(e Env) Result {
		out, ok := ps(e, `net accounts`)
		if !ok {
			return na("net accounts nicht auswertbar")
		}
		for _, l := range strings.Split(out, "\n") {
			ll := strings.ToLower(l)
			if strings.Contains(ll, "lockout threshold") || strings.Contains(ll, "sperrungsschwelle") {
				if strings.Contains(ll, "never") || strings.Contains(ll, "nie") {
					return fail("keine Kontosperrungsschwelle")
				}
				if m := regexp.MustCompile(`(\d+)\s*$`).FindStringSubmatch(strings.TrimSpace(l)); m != nil {
					n, _ := strconv.Atoi(m[1])
					if n >= 1 && n <= 10 {
						return pass("Schwelle " + m[1])
					}
					return fail("Schwelle " + m[1])
				}
			}
		}
		return na("Schwelle nicht gefunden")
	}
	evaluators["powershell_v2_disabled"] = func(e Env) Result {
		out, ok := ps(e, `try { (Get-WindowsOptionalFeature -Online -FeatureName MicrosoftWindowsPowerShellV2).State } catch { 'n/a' }`)
		if !ok || out == "" || out == "n/a" {
			return na("Feature-Status nicht lesbar")
		}
		if strings.EqualFold(out, "Disabled") || strings.EqualFold(out, "DisabledWithPayloadRemoved") {
			return pass("PowerShell 2.0 " + out)
		}
		return fail("PowerShell 2.0 " + out)
	}
	evaluators["wdigest_disabled"] = func(e Env) Result {
		v := reg(e, `HKLM:\SYSTEM\CurrentControlSet\Control\SecurityProviders\WDigest`, "UseLogonCredential")
		if v == "" || v == "0" {
			return pass("UseLogonCredential = " + firstNonEmpty(v, "nicht gesetzt (Standard 0)"))
		}
		return fail("UseLogonCredential = " + v)
	}
	evaluators["lsa_ppl"] = func(e Env) Result {
		v := reg(e, `HKLM:\SYSTEM\CurrentControlSet\Control\Lsa`, "RunAsPPL")
		if v == "1" || v == "2" {
			return pass("RunAsPPL = " + v)
		}
		return fail("RunAsPPL = " + firstNonEmpty(v, "nicht gesetzt"))
	}
}

func netAccountsNumber(e Env, labels []string) (int, bool) {
	out, ok := ps(e, `net accounts`)
	if !ok {
		return 0, false
	}
	for _, l := range strings.Split(out, "\n") {
		ll := strings.ToLower(l)
		for _, lab := range labels {
			if strings.Contains(ll, lab) {
				if m := regexp.MustCompile(`(\d+)\s*$`).FindStringSubmatch(strings.TrimSpace(l)); m != nil {
					n, _ := strconv.Atoi(m[1])
					return n, true
				}
				return 0, true // „Keine“/„None“
			}
		}
	}
	return 0, false
}
