package authlog

import (
	"path"
	"regexp"
	"strings"
)

// winSuspiciousExe: Programme, deren Start (ggf. nur mit bestimmten Argumenten) als
// verdächtig gilt – Living-off-the-Land-Werkzeuge und typische Angreiferbefehle.
// Args leer = jeder Start; sonst muss die Befehlszeile eines der Fragmente enthalten.
var winSuspiciousExe = []struct {
	Exe  string
	Args []string
}{
	{"mimikatz.exe", nil}, {"procdump.exe", nil}, {"procdump64.exe", nil}, {"psexec.exe", nil}, {"psexesvc.exe", nil},
	{"nc.exe", nil}, {"ncat.exe", nil}, {"mshta.exe", nil},
	{"certutil.exe", []string{"-urlcache", "-decode", "-encode", "urlcache"}},
	{"bitsadmin.exe", []string{"/transfer", "/addfile"}},
	{"regsvr32.exe", []string{"/i:http", "scrobj"}},
	{"rundll32.exe", []string{"javascript:", "shell32.dll,control_rundll", "advpack", "url.dll,fileprotocolhandler"}},
	{"wmic.exe", []string{"process call create", "/node:"}},
	{"vssadmin.exe", []string{"delete shadows"}},
	{"wbadmin.exe", []string{"delete catalog", "delete systemstatebackup"}},
	{"wevtutil.exe", []string{" cl ", " clear-log "}},
	{"bcdedit.exe", []string{"recoveryenabled no", "bootstatuspolicy"}},
	{"schtasks.exe", []string{"/create"}},
	{"net.exe", []string{" user ", "localgroup administrators", "localgroup administratoren"}},
	{"net1.exe", []string{" user ", "localgroup administrators", "localgroup administratoren"}},
	{"reg.exe", []string{"currentversion\\run", "image file execution options", "winlogon"}},
	{"powershell.exe", []string{"-enc", "-encodedcommand", "-w hidden", "-windowstyle hidden", "downloadstring", "downloadfile", "iex", "invoke-expression", "-nop -", "bypass", "frombase64string"}},
	{"pwsh.exe", []string{"-enc", "-encodedcommand", "-w hidden", "downloadstring", "iex", "invoke-expression", "bypass"}},
	{"cmd.exe", []string{"/c powershell", "/c certutil", "/c bitsadmin", "/c mshta"}},
	{"cscript.exe", []string{"http://", "https://", "%temp%", "\\appdata\\"}},
	{"wscript.exe", []string{"http://", "https://", "%temp%", "\\appdata\\"}},
}

// winSuspiciousParent: Office-Programme, die keine Shells starten sollten (Makro-Malware).
var winSuspiciousParent = map[string][]string{
	"winword.exe":  {"cmd.exe", "powershell.exe", "pwsh.exe", "wscript.exe", "cscript.exe", "mshta.exe", "rundll32.exe", "regsvr32.exe", "certutil.exe"},
	"excel.exe":    {"cmd.exe", "powershell.exe", "pwsh.exe", "wscript.exe", "cscript.exe", "mshta.exe", "rundll32.exe", "regsvr32.exe", "certutil.exe"},
	"outlook.exe":  {"cmd.exe", "powershell.exe", "pwsh.exe", "wscript.exe", "cscript.exe", "mshta.exe", "rundll32.exe", "regsvr32.exe", "certutil.exe"},
	"powerpnt.exe": {"cmd.exe", "powershell.exe", "pwsh.exe", "wscript.exe", "cscript.exe", "mshta.exe"},
}

// psSuspiciousMarks: Fragmente in Skriptblöcken (4104), die auf Download-Cradles, Umgehung
// von AMSI/Defender, Credential-Dumping oder Shellcode hindeuten.
var psSuspiciousMarks = []string{
	"invoke-expression", "iex(", "iex ", "downloadstring", "downloadfile", "frombase64string", "-encodedcommand", "-enc ",
	"invoke-mimikatz", "amsiutils", "amsiinitfailed", "amsicontext", "set-mppreference -disablerealtimemonitoring",
	"add-mppreference -exclusionpath", "new-object net.webclient", "net.webclient", "reflection.assembly", "virtualalloc",
	"getdelegateforfunctionpointer", "invoke-shellcode", "invoke-reflectivepeinjection", "start-bitstransfer", "-nop -w hidden",
	"bypass -nop", "invoke-webrequest -uri http", "certutil -urlcache", "sekurlsa", "lsass", "invoke-kerberoast", "invoke-dcsync",
}

// rosterScriptPrefix: eigene Skripte des Agents (werden nicht gemeldet).
const rosterScriptPrefix = "$progresspreference='silentlycontinue'"

var reWS = regexp.MustCompile(`\s+`)

// suspiciousProcess bewertet einen Prozessstart (4688). Liefert den Grund und ob verdächtig.
func suspiciousProcess(exe, cmdline, parent string) (string, bool) {
	base := strings.ToLower(path.Base(strings.ReplaceAll(exe, "\\", "/")))
	cmd := strings.ToLower(reWS.ReplaceAllString(cmdline, " "))
	pbase := strings.ToLower(path.Base(strings.ReplaceAll(parent, "\\", "/")))
	if base == "" {
		return "", false
	}
	if children, ok := winSuspiciousParent[pbase]; ok {
		for _, c := range children {
			if c == base {
				return "Office startet " + base, true
			}
		}
	}
	for _, p := range winSuspiciousExe {
		if p.Exe != base {
			continue
		}
		if len(p.Args) == 0 {
			return "verdächtiges Programm", true
		}
		for _, a := range p.Args {
			if strings.Contains(cmd, a) {
				return "verdächtige Argumente (" + strings.TrimSpace(a) + ")", true
			}
		}
	}
	return "", false
}

// suspiciousScript bewertet einen PowerShell-Skriptblock (4104). Liefert das Fragment.
func suspiciousScript(text string) (string, bool) {
	low := strings.ToLower(strings.TrimSpace(text))
	if low == "" || strings.HasPrefix(low, rosterScriptPrefix) {
		return "", false
	}
	for _, m := range psSuspiciousMarks {
		if strings.Contains(low, m) {
			return strings.TrimSpace(m), true
		}
	}
	return "", false
}

var reTaskCommand = regexp.MustCompile(`(?is)<Command>\s*(.*?)\s*</Command>`)
var reTaskArgs = regexp.MustCompile(`(?is)<Arguments>\s*(.*?)\s*</Arguments>`)

// taskCommand zieht Befehl (+ Argumente) aus dem TaskContent-XML einer geplanten Aufgabe.
func taskCommand(xml string) string {
	m := reTaskCommand.FindStringSubmatch(xml)
	if m == nil {
		return ""
	}
	out := strings.TrimSpace(m[1])
	if a := reTaskArgs.FindStringSubmatch(xml); a != nil && strings.TrimSpace(a[1]) != "" {
		out += " " + strings.TrimSpace(a[1])
	}
	return out
}
