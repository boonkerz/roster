package compliance

import "strings"

func init() {
	evaluators["filevault_on"] = func(e Env) Result {
		out, _, err := e.Run("fdesetup", "status")
		if err != nil {
			return na("fdesetup nicht ausführbar")
		}
		if strings.Contains(out, "FileVault is On") {
			return pass(out)
		}
		return fail(out)
	}
	evaluators["firewall_on"] = func(e Env) Result {
		out, _, err := e.Run("/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate")
		if err != nil {
			return na("socketfilterfw nicht ausführbar")
		}
		if strings.Contains(strings.ToLower(out), "enabled") {
			return pass(out)
		}
		return fail(out)
	}
	evaluators["stealth_mode"] = func(e Env) Result {
		out, _, err := e.Run("/usr/libexec/ApplicationFirewall/socketfilterfw", "--getstealthmode")
		if err != nil {
			return na("socketfilterfw nicht ausführbar")
		}
		if strings.Contains(strings.ToLower(out), "enabled") {
			return pass(out)
		}
		return fail(out)
	}
	evaluators["gatekeeper_on"] = func(e Env) Result {
		out, _, err := e.Run("spctl", "--status")
		if err != nil {
			return na("spctl nicht ausführbar")
		}
		if strings.Contains(out, "assessments enabled") {
			return pass(out)
		}
		return fail(out)
	}
	evaluators["sip_enabled"] = func(e Env) Result {
		out, _, err := e.Run("csrutil", "status")
		if err != nil {
			return na("csrutil nicht ausführbar")
		}
		if strings.Contains(strings.ToLower(out), "enabled") {
			return pass(out)
		}
		return fail(out)
	}
	evaluators["screensaver_password"] = func(e Env) Result {
		out, exit, err := e.Run("defaults", "-currentHost", "read", "com.apple.screensaver", "askForPassword")
		if err != nil || exit != 0 {
			return na("Einstellung nicht lesbar (benutzerspezifisch)")
		}
		if strings.TrimSpace(out) == "1" {
			return pass("askForPassword = 1")
		}
		return fail("askForPassword = " + strings.TrimSpace(out))
	}
	evaluators["guest_disabled"] = func(e Env) Result {
		out, exit, err := e.Run("defaults", "read", "/Library/Preferences/com.apple.loginwindow", "GuestEnabled")
		if err != nil || exit != 0 {
			return pass("GuestEnabled nicht gesetzt (Standard aus)")
		}
		if strings.TrimSpace(out) == "0" {
			return pass("GuestEnabled = 0")
		}
		return fail("GuestEnabled = " + strings.TrimSpace(out))
	}
	evaluators["remote_login_off"] = func(e Env) Result {
		out, _, err := e.Run("systemsetup", "-getremotelogin")
		if err != nil {
			return na("systemsetup nicht ausführbar")
		}
		if strings.Contains(strings.ToLower(out), "off") {
			return pass(out)
		}
		return fail(out)
	}
}
