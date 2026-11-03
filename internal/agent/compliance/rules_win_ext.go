package compliance

import (
	"strconv"
	"strings"
)

// Erweiterte Windows-Regeln (0.19), PowerShell-3-/Server-2012-tauglich: Registry über
// Get-ItemProperty, Kontorichtlinie über net accounts, Dienste über WMI, Überwachung über
// auditpol mit GUIDs (sprachunabhängig), DEP über bcdedit.

// regInt liest einen numerischen Registry-Wert.
func regInt(e Env, key, name string) (int, bool) {
	v := reg(e, key, name)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	return n, err == nil
}

// wmiServiceStart liefert den Startmodus eines Dienstes (Auto|Manual|Disabled, "" = fehlt).
func wmiServiceStart(e Env, name string) string {
	out, _ := ps(e, `(Get-WmiObject Win32_Service -Filter "Name='`+name+`'").StartMode`)
	return strings.TrimSpace(out)
}

// auditpol-Unterkategorien per GUID (funktioniert in jeder Sprache).
const (
	auditLogon          = "{0CCE9215-69AE-11D9-BED3-505054503030}"
	auditAccountMgmt    = "{0CCE9235-69AE-11D9-BED3-505054503030}"
	auditProcessCreate  = "{0CCE922B-69AE-11D9-BED3-505054503030}"
	auditPolicyChange   = "{0CCE922F-69AE-11D9-BED3-505054503030}"
	auditFileSystemGUID = "{0CCE921D-69AE-11D9-BED3-505054503030}"
)

// auditpolSub liest Erfolg/Fehler einer Unterkategorie (`auditpol /get /subcategory:<guid> /r`, CSV).
func auditpolSub(e Env, guid string) (success, failure, ok bool) {
	out, _, err := e.Run("auditpol", "/get", "/subcategory:"+guid, "/r")
	if err != nil || strings.TrimSpace(out) == "" {
		return false, false, false
	}
	for _, l := range strings.Split(out, "\n") {
		if !strings.Contains(l, guid) {
			continue
		}
		f := strings.Split(l, ",")
		setting := strings.ToLower(strings.TrimSpace(f[len(f)-1]))
		if setting == "" && len(f) > 1 {
			setting = strings.ToLower(strings.TrimSpace(f[len(f)-2]))
		}
		success = strings.Contains(setting, "success") || strings.Contains(setting, "erfolg")
		failure = strings.Contains(setting, "failure") || strings.Contains(setting, "fehler")
		return success, failure, true
	}
	return false, false, false
}

func netAccountsMin(labels []string, min int, title string) evaluator {
	return func(e Env) Result {
		n, ok := netAccountsNumber(e, labels)
		if !ok {
			return na("net accounts nicht auswertbar")
		}
		if n < min {
			return fail(title + " " + strconv.Itoa(n))
		}
		return pass(title + " " + strconv.Itoa(n))
	}
}

func regFlag(key, name string, want int, missingPass bool, title string) evaluator {
	return func(e Env) Result {
		n, ok := regInt(e, key, name)
		if !ok {
			if missingPass {
				return pass(title + " nicht gesetzt (Standard sicher)")
			}
			return fail(title + " nicht gesetzt")
		}
		if n != want {
			return fail(title + " = " + strconv.Itoa(n))
		}
		return pass(title + " = " + strconv.Itoa(n))
	}
}

func init() {
	const lsa = `HKLM:\SYSTEM\CurrentControlSet\Control\Lsa`
	const winlogon = `HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon`
	// --- Konten ---
	evaluators["pw_history"] = netAccountsMin([]string{"length of password history", "kennwortchronik"}, 24, "Kennwortchronik")
	evaluators["pw_max_age"] = func(e Env) Result {
		n, ok := netAccountsNumber(e, []string{"maximum password age", "maximales kennwortalter"})
		if !ok {
			return na("net accounts nicht auswertbar")
		}
		if n < 1 || n > 365 {
			return fail("maximales Kennwortalter " + strconv.Itoa(n) + " (0 = unbegrenzt)")
		}
		return pass("maximales Kennwortalter " + strconv.Itoa(n))
	}
	evaluators["pw_min_age"] = netAccountsMin([]string{"minimum password age", "minimales kennwortalter"}, 1, "minimales Kennwortalter")
	evaluators["lockout_duration"] = netAccountsMin([]string{"lockout duration", "sperrdauer"}, 15, "Sperrdauer")
	evaluators["lsa_anonymous"] = func(e Env) Result {
		ra, _ := regInt(e, lsa, "RestrictAnonymous")
		sam, okSam := regInt(e, lsa, "RestrictAnonymousSAM")
		if !okSam {
			sam = 1
		}
		ev, _ := regInt(e, lsa, "EveryoneIncludesAnonymous")
		if ra < 1 || sam != 1 || ev != 0 {
			return fail("RestrictAnonymous=" + strconv.Itoa(ra) + " RestrictAnonymousSAM=" + strconv.Itoa(sam) + " EveryoneIncludesAnonymous=" + strconv.Itoa(ev))
		}
		return pass("anonyme Aufzählung eingeschränkt")
	}
	evaluators["lm_compat_level"] = func(e Env) Result {
		n, ok := regInt(e, lsa, "LmCompatibilityLevel")
		if !ok {
			return fail("LmCompatibilityLevel nicht gesetzt (Standard 3)")
		}
		if n < 5 {
			return fail("LmCompatibilityLevel = " + strconv.Itoa(n))
		}
		return pass("LmCompatibilityLevel = " + strconv.Itoa(n))
	}
	evaluators["cached_logons"] = func(e Env) Result {
		v := strings.TrimSpace(reg(e, winlogon, "CachedLogonsCount"))
		n, err := strconv.Atoi(v)
		if err != nil {
			return fail("CachedLogonsCount nicht gesetzt (Standard 10)")
		}
		if n > 4 {
			return fail("CachedLogonsCount = " + v)
		}
		return pass("CachedLogonsCount = " + v)
	}
	evaluators["autologon_off"] = func(e Env) Result {
		auto := strings.TrimSpace(reg(e, winlogon, "AutoAdminLogon"))
		pw := strings.TrimSpace(reg(e, winlogon, "DefaultPassword"))
		if auto == "1" || pw != "" {
			return fail("AutoAdminLogon=" + auto + ", DefaultPassword gesetzt: " + strconv.FormatBool(pw != ""))
		}
		return pass("keine automatische Anmeldung")
	}
	evaluators["screensaver_lock"] = func(e Env) Result {
		const k = `HKLM:\SOFTWARE\Policies\Microsoft\Windows\Control Panel\Desktop`
		active, _ := regInt(e, k, "ScreenSaveActive")
		secure, _ := regInt(e, k, "ScreenSaverIsSecure")
		timeout, okT := regInt(e, k, "ScreenSaveTimeOut")
		if active != 1 || secure != 1 || !okT || timeout > 900 || timeout <= 0 {
			return fail("ScreenSaveActive=" + strconv.Itoa(active) + " ScreenSaverIsSecure=" + strconv.Itoa(secure) + " ScreenSaveTimeOut=" + strconv.Itoa(timeout))
		}
		return pass("Bildschirmsperre nach " + strconv.Itoa(timeout) + " s")
	}
	// --- Netzwerk / Dienste ---
	evaluators["smb_signing_server"] = regFlag(`HKLM:\SYSTEM\CurrentControlSet\Services\LanmanServer\Parameters`, "RequireSecuritySignature", 1, false, "RequireSecuritySignature (Server)")
	evaluators["smb_signing_client"] = regFlag(`HKLM:\SYSTEM\CurrentControlSet\Services\LanmanWorkstation\Parameters`, "RequireSecuritySignature", 1, false, "RequireSecuritySignature (Client)")
	evaluators["rdp_idle_timeout"] = func(e Env) Result {
		if deny, ok := regInt(e, `HKLM:\SYSTEM\CurrentControlSet\Control\Terminal Server`, "fDenyTSConnections"); ok && deny == 1 {
			return na("RDP deaktiviert")
		}
		n, ok := regInt(e, `HKLM:\SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services`, "MaxIdleTime")
		if !ok || n <= 0 || n > 900000 {
			return fail("MaxIdleTime = " + strconv.Itoa(n) + " ms")
		}
		return pass("MaxIdleTime = " + strconv.Itoa(n) + " ms")
	}
	evaluators["firewall_logging"] = func(e Env) Result {
		out, ok := ps(e, `(Get-NetFirewallProfile | ForEach-Object { "$($_.Name)=$($_.LogBlocked)" }) -join ';'`)
		if !ok || out == "" {
			return errRes("Firewall-Profile nicht lesbar")
		}
		if strings.Contains(out, "=False") || strings.Contains(out, "=NotConfigured") {
			return fail(out)
		}
		return pass(out)
	}
	evaluators["remote_registry_disabled"] = func(e Env) Result {
		mode := wmiServiceStart(e, "RemoteRegistry")
		if mode == "" {
			return na("Dienst RemoteRegistry fehlt")
		}
		if !strings.EqualFold(mode, "Disabled") {
			return fail("RemoteRegistry: " + mode)
		}
		return pass("RemoteRegistry deaktiviert")
	}
	evaluators["print_spooler_server"] = func(e Env) Result {
		mode := wmiServiceStart(e, "Spooler")
		if mode == "" {
			return na("Dienst Spooler fehlt")
		}
		if !strings.EqualFold(mode, "Disabled") {
			return fail("Spooler: " + mode)
		}
		return pass("Spooler deaktiviert")
	}
	evaluators["winrm_basic_auth"] = regFlag(`HKLM:\SOFTWARE\Policies\Microsoft\Windows\WinRM\Service`, "AllowBasic", 0, true, "AllowBasic")
	evaluators["winrm_unencrypted"] = regFlag(`HKLM:\SOFTWARE\Policies\Microsoft\Windows\WinRM\Service`, "AllowUnencryptedTraffic", 0, true, "AllowUnencryptedTraffic")
	evaluators["netbios_disabled"] = func(e Env) Result {
		out, ok := ps(e, `(Get-ChildItem 'HKLM:\SYSTEM\CurrentControlSet\Services\NetBT\Parameters\Interfaces' | ForEach-Object { (Get-ItemProperty $_.PSPath).NetbiosOptions }) -join ','`)
		if !ok || out == "" {
			return errRes("NetBT-Schnittstellen nicht lesbar")
		}
		for _, v := range strings.Split(out, ",") {
			if strings.TrimSpace(v) != "2" {
				return fail("NetbiosOptions: " + out)
			}
		}
		return pass("NetBIOS auf allen Schnittstellen aus")
	}
	// --- Härtung / Defender ---
	evaluators["defender_cloud"] = func(e Env) Result {
		out, ok := ps(e, `try { $p=Get-MpPreference; "$($p.MAPSReporting);$($p.SubmitSamplesConsent)" } catch { 'n/a' }`)
		if !ok || out == "" || out == "n/a" {
			return na("Defender nicht verfügbar")
		}
		f := strings.Split(out, ";")
		maps, _ := strconv.Atoi(strings.TrimSpace(f[0]))
		consent := 1
		if len(f) > 1 {
			consent, _ = strconv.Atoi(strings.TrimSpace(f[1]))
		}
		if maps < 1 || consent == 2 {
			return fail("MAPSReporting=" + strconv.Itoa(maps) + " SubmitSamplesConsent=" + strconv.Itoa(consent))
		}
		return pass("MAPSReporting=" + strconv.Itoa(maps))
	}
	evaluators["defender_pua"] = func(e Env) Result {
		out, ok := ps(e, `try { (Get-MpPreference).PUAProtection } catch { 'n/a' }`)
		if !ok || out == "" || out == "n/a" {
			return na("Defender nicht verfügbar")
		}
		if strings.TrimSpace(out) != "1" {
			return fail("PUAProtection = " + strings.TrimSpace(out))
		}
		return pass("PUAProtection = 1")
	}
	evaluators["uac_secure_desktop"] = regFlag(`HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`, "PromptOnSecureDesktop", 1, false, "PromptOnSecureDesktop")
	evaluators["sehop_enabled"] = regFlag(`HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\kernel`, "DisableExceptionChainValidation", 0, true, "DisableExceptionChainValidation")
	evaluators["dep_optout"] = func(e Env) Result {
		out, _, err := e.Run("bcdedit", "/enum", "{current}")
		if err != nil || strings.TrimSpace(out) == "" {
			return na("bcdedit nicht ausführbar")
		}
		for _, l := range strings.Split(out, "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 && strings.EqualFold(f[0], "nx") {
				v := strings.ToLower(f[1])
				if v == "optout" || v == "alwayson" {
					return pass("nx " + f[1])
				}
				return fail("nx " + f[1])
			}
		}
		return pass("nx nicht gesetzt (Standard OptIn/OptOut je SKU)")
	}
	evaluators["powershell_logging"] = regFlag(`HKLM:\SOFTWARE\Policies\Microsoft\Windows\PowerShell\ScriptBlockLogging`, "EnableScriptBlockLogging", 1, false, "EnableScriptBlockLogging")
	// --- Überwachung ---
	auditRule := func(guid, title string, needFailure bool) evaluator {
		return func(e Env) Result {
			s, f, ok := auditpolSub(e, guid)
			if !ok {
				return na("auditpol nicht auswertbar")
			}
			if !s || (needFailure && !f) {
				return fail(title + ": Erfolg=" + strconv.FormatBool(s) + " Fehler=" + strconv.FormatBool(f))
			}
			return pass(title + " überwacht")
		}
	}
	evaluators["audit_logon"] = auditRule(auditLogon, "Anmelden", true)
	evaluators["audit_account_mgmt"] = auditRule(auditAccountMgmt, "Benutzerkontenverwaltung", true)
	evaluators["audit_policy_change"] = auditRule(auditPolicyChange, "Richtlinienänderung", false)
	evaluators["audit_process_creation"] = func(e Env) Result {
		r := auditRule(auditProcessCreate, "Prozesserstellung", false)(e)
		if r.Status != "pass" {
			return r
		}
		if n, ok := regInt(e, `HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System\Audit`, "ProcessCreationIncludeCmdLine_Enabled"); !ok || n != 1 {
			return fail("Prozesserstellung überwacht, aber ohne Befehlszeile (ProcessCreationIncludeCmdLine_Enabled)")
		}
		return pass("Prozesserstellung mit Befehlszeile überwacht")
	}
	evaluators["eventlog_size_security"] = func(e Env) Result {
		out, ok := ps(e, `(Get-WinEvent -ListLog Security).MaximumSizeInBytes`)
		n, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
		if !ok || err != nil {
			return errRes("Protokollgröße nicht lesbar")
		}
		if n < 192<<20 {
			return fail("Security " + strconv.FormatInt(n>>20, 10) + " MB")
		}
		return pass("Security " + strconv.FormatInt(n>>20, 10) + " MB")
	}
	evaluators["eventlog_size_system"] = func(e Env) Result {
		out, ok := ps(e, `"$((Get-WinEvent -ListLog System).MaximumSizeInBytes);$((Get-WinEvent -ListLog Application).MaximumSizeInBytes)"`)
		if !ok || out == "" {
			return errRes("Protokollgröße nicht lesbar")
		}
		var bad []string
		for i, v := range strings.Split(out, ";") {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			name := []string{"System", "Application"}[i%2]
			if err != nil || n < 32<<20 {
				bad = append(bad, name+" "+strconv.FormatInt(n>>20, 10)+" MB")
			}
		}
		if len(bad) > 0 {
			return fail(strings.Join(bad, ", "))
		}
		return pass("System/Application ≥ 32 MB")
	}
}
