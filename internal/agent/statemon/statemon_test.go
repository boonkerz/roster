package statemon

import (
	"strings"
	"testing"
	"time"
)

func TestDiff(t *testing.T) {
	now := time.Now()
	prev := Snapshot{"ports": {"tcp 0.0.0.0:22 sshd", "tcp 0.0.0.0:80 nginx"}, "uid0": {"root"}, "modules": {"ext4"}}
	cur := Snapshot{"ports": {"tcp 0.0.0.0:22 sshd", "tcp 0.0.0.0:4444 nc"}, "uid0": {"root", "backdoor"}, "modules": {"ext4"}, "tasks": {"x"}}
	ev := Diff(prev, cur, now)
	if len(ev) != 2 {
		t.Fatalf("events: %+v", ev)
	}
	if ev[0].Service != "ports" || ev[0].Detail != "+ tcp 0.0.0.0:4444 nc, - tcp 0.0.0.0:80 nginx" || ev[0].Kind != "state_change" || ev[0].RecordID == "" {
		t.Errorf("ports: %+v", ev[0])
	}
	if ev[1].Service != "uid0" || ev[1].Detail != "+ backdoor" {
		t.Errorf("uid0: %+v", ev[1])
	}
	if len(Diff(cur, cur, now)) != 0 {
		t.Error("unverändert darf nichts liefern")
	}
	if len(Diff(Snapshot{}, cur, now)) != 0 {
		t.Error("leere Baseline darf nichts liefern")
	}
	long := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		long = append(long, strings.Repeat("x", 20)+string(rune('a'+i%26)))
	}
	ev = Diff(Snapshot{"cron": {}}, Snapshot{"cron": long}, now)
	if len(ev) != 1 || len(ev[0].Detail) > maxDetail+3 {
		t.Errorf("Kürzung: %d", len(ev[0].Detail))
	}
}

func TestParsers(t *testing.T) {
	if got := ParseCron("/etc/crontab", "# comment\n\n*/5 * * * * root run-parts /etc/cron.d\n"); len(got) != 1 || got[0] != "/etc/crontab: */5 * * * * root run-parts /etc/cron.d" {
		t.Errorf("cron: %v", got)
	}
	if got := ParsePasswdUID0("root:x:0:0:root:/root:/bin/bash\nthomas:x:1000:1000::/home/t:/bin/sh\ntoor:x:0:0::/:/bin/sh\n"); len(got) != 2 || got[1] != "toor" {
		t.Errorf("uid0: %v", got)
	}
	got := ParseGroupMembers("root:x:0:\nsudo:x:27:thomas,alice\nwheel:x:10:\nusers:x:100:bob\n", "svc:x:1001:0::/:/bin/sh\n", map[string]bool{"sudo": true, "wheel": true, "root": true})
	if len(got) != 3 || got[0] != "sudo: thomas" || got[2] != "root: svc (primäre Gruppe)" {
		t.Errorf("groups: %v", got)
	}
	en := "Alias name     Administrators\nComment        Administrators have complete access\n\nMembers\n\n-------------------------------------------------------------------------------\nAdministrator\nCORP\\Domain Admins\nbob\nThe command completed successfully.\n\n"
	if got := ParseNetLocalgroup(en); len(got) != 3 || got[1] != `CORP\Domain Admins` {
		t.Errorf("net localgroup en: %v", got)
	}
	de := "Aliasname      Administratoren\nBeschreibung   Administratoren haben uneingeschränkten Zugriff\n\nMitglieder\n\n-------------------------------------------------------------------------------\nAdministrator\nthomas\nDer Befehl wurde erfolgreich ausgeführt.\n"
	if got := ParseNetLocalgroup(de); len(got) != 2 || got[1] != "thomas" {
		t.Errorf("net localgroup de: %v", got)
	}
	csvOut := "\"\\Microsoft\\Windows\\Defrag\\ScheduledDefrag\",\"03.11.2026 10:00:00\",\"Bereit\"\r\n\"\\Updater\",\"N/A\",\"Bereit\"\r\n"
	if got := ParseSchtasksCSV(csvOut); len(got) != 2 || got[1] != `\Updater` {
		t.Errorf("schtasks: %v", got)
	}
	if got := ParseUnitFiles("sshd.service enabled enabled\ncron.service enabled enabled\n"); len(got) != 2 || got[0] != "sshd.service" {
		t.Errorf("units: %v", got)
	}
	if got := ParseLaunchctl("PID\tStatus\tLabel\n123\t0\tcom.apple.foo\n-\t0\tcom.evil.agent\n"); len(got) != 2 || got[1] != "com.evil.agent" {
		t.Errorf("launchctl: %v", got)
	}
}
