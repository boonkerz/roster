// Package authlog liest Anmelde- und Kontoereignisse (SSH/RDP/Konsole, sudo, su,
// Benutzer-/Gruppenänderungen) inkrementell aus journald, /var/log/auth.log, dem
// Windows-Sicherheitsprotokoll bzw. dem macOS-Log und normalisiert sie zu
// shared.AuthEvent. Die Parser sind rein und plattformneutral testbar.
package authlog

import (
	"regexp"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

var (
	reFailedPw   = regexp.MustCompile(`^Failed (?:password|publickey|keyboard-interactive(?:/pam)?|none) for (invalid user )?(\S+) from (\S+)`)
	reAccepted   = regexp.MustCompile(`^Accepted (\S+) for (\S+) from (\S+)`)
	reInvalid    = regexp.MustCompile(`^Invalid user (\S*) from (\S+)`)
	reMaxAuth    = regexp.MustCompile(`maximum authentication attempts exceeded for (?:invalid user )?(\S+) from (\S+)`)
	reSudoCmd    = regexp.MustCompile(`^\s*(\S+) : (?:.*; )?COMMAND=(.*)$`)
	reSudoFail   = regexp.MustCompile(`^\s*(\S+) : (\d+) incorrect password attempts?`)
	reSuOK       = regexp.MustCompile(`^\(to (\S+)\) (\S+) on (\S+)`)
	reSuFail     = regexp.MustCompile(`^FAILED SU \(to (\S+)\) (\S+) on (\S+)`)
	reNewUser    = regexp.MustCompile(`^new user: name=([^,]+)`)
	reNewGroup   = regexp.MustCompile(`^new group: name=([^,]+)`)
	reDelUser    = regexp.MustCompile(`^delete user '([^']+)'`)
	reAddToGroup = regexp.MustCompile(`^add '([^']+)' to (?:shadow )?group '([^']+)'`)
	reGpasswd    = regexp.MustCompile(`^user (\S+) added by \S+ to group (\S+)`)
	reUsermodPw  = regexp.MustCompile(`^change user '([^']+)' password`)
	rePwChanged  = regexp.MustCompile(`password changed for (\S+)`)
	reFailedTTY  = regexp.MustCompile(`^FAILED LOGIN \(\d+\) on '([^']+)' FOR '([^']+)'`)
	rePamSession = regexp.MustCompile(`^pam_unix\(([a-z-]+):session\): session opened for user ([^( ]+)`)
	rePamAuth    = regexp.MustCompile(`^pam_unix\(([a-z-]+):auth\): authentication failure;.*rhost=(\S*).*user=(\S*)`)
	reIdentPID   = regexp.MustCompile(`^([A-Za-z0-9_./-]+)(?:\[\d+\])?$`)
)

// ParseMessage normalisiert eine Meldung des Programms ident (sshd, sudo, su, useradd …).
// ok=false für uninteressante Zeilen (Session-Rauschen, Duplikate).
func ParseMessage(ident, msg string, ts time.Time) (shared.AuthEvent, bool) {
	msg = strings.TrimSpace(msg)
	ev := shared.AuthEvent{Time: ts, Service: "pam"}
	switch {
	case ident == "sshd" || strings.HasPrefix(ident, "sshd"):
		ev.Service = "sshd"
		if m := reFailedPw.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.SourceIP = "login_failed", m[2], m[3]
			if m[1] != "" {
				ev.Kind = "invalid_user"
			}
			return ev, true
		}
		if m := reAccepted.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.SourceIP, ev.Detail = "login_ok", m[2], m[3], m[1]
			return ev, true
		}
		if m := reInvalid.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.SourceIP = "invalid_user", m[1], m[2]
			return ev, true
		}
		if m := reMaxAuth.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.SourceIP, ev.Detail = "login_failed", m[1], m[2], "max attempts"
			return ev, true
		}
		return ev, false
	case ident == "sudo":
		ev.Service = "sudo"
		if m := reSudoFail.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "login_failed", m[1], m[2]+" incorrect password attempts"
			return ev, true
		}
		if m := reSudoCmd.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "sudo", m[1], strings.TrimSpace(m[2])
			return ev, true
		}
		return ev, false
	case ident == "su" || ident == "su-l":
		ev.Service = "su"
		if m := reSuFail.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "login_failed", m[2], "su to "+m[1]+" on "+m[3]
			return ev, true
		}
		if m := reSuOK.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "su", m[2], "to "+m[1]+" on "+m[3]
			return ev, true
		}
		return ev, false
	case ident == "useradd":
		ev.Service = "system"
		if m := reNewUser.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "user_added", m[1], msg
			return ev, true
		}
		return ev, false // "new group" von useradd ist die private Gruppe
	case ident == "groupadd":
		ev.Service = "system"
		if m := reNewGroup.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.Detail = "group_changed", "new group "+m[1]
			return ev, true
		}
		return ev, false
	case ident == "userdel":
		ev.Service = "system"
		if m := reDelUser.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "user_removed", m[1], msg
			return ev, true
		}
		return ev, false
	case ident == "usermod":
		ev.Service = "system"
		if m := reAddToGroup.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "group_changed", m[1], "group "+m[2]
			return ev, true
		}
		if m := reUsermodPw.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User = "password_changed", m[1]
			return ev, true
		}
		if strings.HasPrefix(msg, "change user") || strings.HasPrefix(msg, "lock user") || strings.HasPrefix(msg, "unlock user") {
			ev.Kind, ev.Detail = "user_modified", msg
			if i := strings.Index(msg, "'"); i >= 0 {
				if j := strings.Index(msg[i+1:], "'"); j >= 0 {
					ev.User = msg[i+1 : i+1+j]
				}
			}
			return ev, true
		}
		return ev, false
	case ident == "gpasswd":
		ev.Service = "system"
		if m := reGpasswd.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "group_changed", m[1], "group "+m[2]
			return ev, true
		}
		return ev, false
	case ident == "passwd" || ident == "chpasswd":
		ev.Service = "system"
		if m := rePwChanged.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User = "password_changed", m[1]
			return ev, true
		}
		return ev, false
	case ident == "login":
		ev.Service = "console"
		if m := reFailedTTY.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.Detail = "login_failed", m[2], m[1]
			return ev, true
		}
		if m := rePamSession.FindStringSubmatch(msg); m != nil && m[1] == "login" {
			ev.Kind, ev.User = "login_ok", m[2]
			return ev, true
		}
		if m := rePamAuth.FindStringSubmatch(msg); m != nil {
			ev.Kind, ev.User, ev.SourceIP = "login_failed", m[3], m[2]
			return ev, true
		}
		return ev, false
	}
	return ev, false
}

var reSyslog = regexp.MustCompile(`^(?:(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))|([A-Z][a-z]{2}\s+\d{1,2} \d{2}:\d{2}:\d{2}))\s+\S+\s+([^:\s]+):\s(.*)$`)

// ParseSyslogLine liest eine Zeile aus /var/log/auth.log bzw. /var/log/secure (klassisches
// oder ISO-8601-Zeitformat). Beim klassischen Format fehlt das Jahr; es kommt aus now.
func ParseSyslogLine(line string, now time.Time) (shared.AuthEvent, bool) {
	m := reSyslog.FindStringSubmatch(strings.TrimRight(line, "\r"))
	if m == nil {
		return shared.AuthEvent{}, false
	}
	var ts time.Time
	if m[1] != "" {
		t, err := time.Parse(time.RFC3339Nano, m[1])
		if err != nil {
			return shared.AuthEvent{}, false
		}
		ts = t
	} else {
		t, err := time.ParseInLocation("Jan 2 15:04:05 2006", m[2]+" "+now.Format("2006"), time.Local)
		if err != nil {
			return shared.AuthEvent{}, false
		}
		if t.After(now.Add(24 * time.Hour)) {
			t = t.AddDate(-1, 0, 0)
		}
		ts = t
	}
	ident := m[3]
	if im := reIdentPID.FindStringSubmatch(ident); im != nil {
		ident = im[1]
	}
	return ParseMessage(ident, m[4], ts)
}

// WinEvent ist ein Eintrag des Windows-Sicherheitsprotokolls (vom PowerShell-Skript als
// JSON geliefert; Data enthält die EventData-Felder nach Name).
type WinEvent struct {
	Log      string            `json:"Log,omitempty"` // Security | System | Microsoft-Windows-PowerShell/Operational
	ID       int               `json:"Id"`
	Time     string            `json:"Time"`
	RecordID int64             `json:"RecordId"`
	Data     map[string]string `json:"Data"`
}

var winNoise = []string{"SYSTEM", "ANONYMOUS LOGON", "LOCAL SERVICE", "NETWORK SERVICE", "DWM-", "UMFD-", "WINDOW MANAGER"}

func winNoiseUser(u string) bool {
	u = strings.ToUpper(strings.TrimSpace(u))
	if u == "" || u == "-" || strings.HasSuffix(u, "$") {
		return true
	}
	for _, n := range winNoise {
		if strings.HasPrefix(u, n) {
			return true
		}
	}
	return false
}

func winService(logonType string) string {
	switch logonType {
	case "10":
		return "rdp"
	case "3", "8":
		return "network"
	case "2", "7", "11":
		return "console"
	}
	return "windows"
}

// ParseWindowsEvent normalisiert ein Sicherheitsereignis (4624/4625/4740/4720/4726/4722/4725/
// 4728/4732/4756/4767/4723/4724). ok=false für Rauschen (Maschinen-/Systemkonten,
// uninteressante Logon-Typen).
func ParseWindowsEvent(w WinEvent) (shared.AuthEvent, bool) {
	ts, err := time.Parse(time.RFC3339Nano, w.Time)
	if err != nil {
		return shared.AuthEvent{}, false
	}
	d := w.Data
	ip := d["IpAddress"]
	if ip == "-" || ip == "::1" || ip == "127.0.0.1" {
		ip = ""
	}
	ev := shared.AuthEvent{Time: ts, SourceIP: ip, RecordID: itoa64(w.RecordID), Service: "windows"}
	switch w.ID {
	case 4625:
		if winNoiseUser(d["TargetUserName"]) {
			return ev, false
		}
		ev.Kind, ev.User, ev.Service = "login_failed", d["TargetUserName"], winService(d["LogonType"])
		if strings.EqualFold(d["Status"], "0xc0000064") || strings.EqualFold(d["SubStatus"], "0xc0000064") {
			ev.Kind = "invalid_user"
		}
		ev.Detail = "LogonType " + d["LogonType"]
		if ws := d["WorkstationName"]; ws != "" && ws != "-" {
			ev.Detail += " from " + ws
		}
		return ev, true
	case 4624:
		lt := d["LogonType"]
		if winNoiseUser(d["TargetUserName"]) || (lt != "2" && lt != "3" && lt != "7" && lt != "10" && lt != "11") {
			return ev, false
		}
		if lt == "3" && ip == "" {
			return ev, false // lokale Netzwerk-Anmeldungen (Dienste) sind Rauschen
		}
		ev.Kind, ev.User, ev.Service, ev.Detail = "login_ok", d["TargetUserName"], winService(lt), "LogonType "+lt
		return ev, true
	case 4740:
		ev.Kind, ev.User, ev.Service, ev.Detail = "lockout", d["TargetUserName"], "windows", "locked out from "+d["TargetDomainName"]
		return ev, true
	case 4720:
		ev.Kind, ev.User, ev.Detail = "user_added", d["TargetUserName"], "by "+d["SubjectUserName"]
		return ev, true
	case 4726:
		ev.Kind, ev.User, ev.Detail = "user_removed", d["TargetUserName"], "by "+d["SubjectUserName"]
		return ev, true
	case 4722, 4725, 4767:
		verb := map[int]string{4722: "enabled", 4725: "disabled", 4767: "unlocked"}[w.ID]
		ev.Kind, ev.User, ev.Detail = "user_modified", d["TargetUserName"], verb+" by "+d["SubjectUserName"]
		return ev, true
	case 4728, 4732, 4756:
		member := d["MemberName"]
		if strings.HasPrefix(member, "CN=") {
			member = strings.TrimPrefix(member, "CN=")
			if i := strings.Index(member, ","); i > 0 {
				member = member[:i]
			}
		}
		if member == "" || member == "-" {
			member = d["MemberSid"]
		}
		ev.Kind, ev.User, ev.Detail = "group_changed", member, "group "+d["TargetUserName"]+" by "+d["SubjectUserName"]
		return ev, true
	case 4723, 4724:
		ev.Kind, ev.User, ev.Detail = "password_changed", d["TargetUserName"], "by "+d["SubjectUserName"]
		return ev, true
	case 7045: // System: Dienst installiert
		ev.Kind, ev.User, ev.Service = "service_installed", d["AccountName"], "system"
		ev.Detail = d["ServiceName"] + " → " + d["ImagePath"] + " (" + d["StartType"] + ")"
		return ev, true
	case 4698, 4702: // geplante Aufgabe angelegt/geändert
		ev.Kind, ev.User, ev.Service = "task_created", d["SubjectUserName"], "system"
		ev.Detail = d["TaskName"]
		if cmd := taskCommand(d["TaskContent"]); cmd != "" {
			ev.Detail += " → " + cmd
		}
		if w.ID == 4702 {
			ev.Detail = "geändert: " + ev.Detail
		}
		return ev, true
	case 1102: // Sicherheitsprotokoll gelöscht (UserData.LogFileCleared)
		ev.Kind, ev.User, ev.Service = "audit_cleared", d["SubjectUserName"], "system"
		ev.Detail = "Sicherheitsprotokoll gelöscht"
		return ev, true
	case 4688: // Prozessstart – nur verdächtige Muster
		exe, cmd, parent := d["NewProcessName"], d["CommandLine"], d["ParentProcessName"]
		why, ok := suspiciousProcess(exe, cmd, parent)
		if !ok {
			return ev, false
		}
		ev.Kind, ev.User, ev.Service = "suspicious_process", d["SubjectUserName"], "process"
		ev.Detail = why + ": " + exe
		if cmd != "" {
			ev.Detail += " " + cut(cmd, 160)
		}
		if parent != "" {
			ev.Detail += " [" + parent + "]"
		}
		return ev, true
	case 4104: // PowerShell-Skriptblock – nur verdächtige Muster
		text := d["ScriptBlockText"]
		mark, ok := suspiciousScript(text)
		if !ok {
			return ev, false
		}
		ev.Kind, ev.Service = "powershell_suspicious", "powershell"
		ev.Detail = mark + " · " + cut(strings.Join(strings.Fields(text), " "), 160)
		return ev, true
	}
	return ev, false
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func itoa64(n int64) string {
	if n == 0 {
		return ""
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
