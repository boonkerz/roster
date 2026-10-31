package collect

import (
	"testing"
	"time"
)

var farPast = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func TestParseAptHistory(t *testing.T) {
	text := `
Start-Date: 2024-05-01  06:25:03
Commandline: /usr/bin/unattended-upgrade
Upgrade: libssl3:amd64 (3.0.2-0ubuntu1.15, 3.0.2-0ubuntu1.16), openssl:amd64 (3.0.2-0ubuntu1.15, 3.0.2-0ubuntu1.16)
End-Date: 2024-05-01  06:25:10

Start-Date: 2024-05-02  10:00:00
Commandline: apt-get install htop
Requested-By: thomas (1000)
Install: htop:amd64 (3.0.5-7build2), libnl-3-200:amd64 (3.5.0-0.1, automatic)
Remove: nano:amd64 (6.2-1)
End-Date: 2024-05-02  10:00:05
`
	ev := parseAptHistory(text, farPast)
	if len(ev) != 5 {
		t.Fatalf("events=%d: %+v", len(ev), ev)
	}
	if ev[0].Name != "libssl3" || ev[0].Action != "upgrade" || ev[0].OldVersion != "3.0.2-0ubuntu1.15" || ev[0].NewVersion != "3.0.2-0ubuntu1.16" || ev[0].Detail != "/usr/bin/unattended-upgrade" {
		t.Errorf("libssl3: %+v", ev[0])
	}
	if ev[0].Time.Format("2006-01-02 15:04:05") != "2024-05-01 06:25:03" {
		t.Errorf("time: %v", ev[0].Time)
	}
	if ev[2].Name != "htop" || ev[2].Action != "install" || ev[2].NewVersion != "3.0.5-7build2" {
		t.Errorf("htop: %+v", ev[2])
	}
	if ev[3].Name != "libnl-3-200" || ev[3].NewVersion != "3.5.0-0.1" {
		t.Errorf("libnl: %+v", ev[3])
	}
	if ev[4].Name != "nano" || ev[4].Action != "remove" || ev[4].OldVersion != "6.2-1" {
		t.Errorf("nano: %+v", ev[4])
	}
	// since schneidet den ersten Block ab
	if n := len(parseAptHistory(text, time.Date(2024, 5, 2, 0, 0, 0, 0, time.Local))); n != 3 {
		t.Errorf("since: %d", n)
	}
}

func TestParseDnfRpmLog(t *testing.T) {
	text := `2024-05-01T06:25:03+0200 SUBDEBUG Upgrade: bash-5.2.26-3.fc40.x86_64
2024-05-01T06:25:04+0200 SUBDEBUG Upgraded: bash-5.2.26-2.fc40.x86_64
2024-05-01T06:25:05+0200 SUBDEBUG Installed: htop-3.3.0-1.fc40.x86_64
2024-05-01T06:25:06+0200 SUBDEBUG Erased: nano-7.2-7.fc40.x86_64
2024-05-01T06:25:07+0200 INFO --- logging initialized ---
`
	ev := parseDnfRpmLog(text, farPast)
	if len(ev) != 3 {
		t.Fatalf("events=%d: %+v", len(ev), ev)
	}
	if ev[0].Name != "bash" || ev[0].Action != "upgrade" || ev[0].NewVersion != "5.2.26-3.fc40" || ev[0].OldVersion != "5.2.26-2.fc40" {
		t.Errorf("bash: %+v", ev[0])
	}
	if ev[1].Action != "install" || ev[1].Name != "htop" || ev[1].NewVersion != "3.3.0-1.fc40" {
		t.Errorf("htop: %+v", ev[1])
	}
	if ev[2].Action != "remove" || ev[2].OldVersion != "7.2-7.fc40" {
		t.Errorf("nano: %+v", ev[2])
	}
}

func TestParseYumLog(t *testing.T) {
	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.Local)
	ev := parseYumLog("May 01 06:25:03 Updated: bash-4.2.46-35.el7.x86_64\nJul 01 06:25:03 Installed: htop-2.2.0-3.el7.x86_64\n", farPast, now)
	if len(ev) != 2 || ev[0].Time.Year() != 2024 || ev[1].Time.Year() != 2023 {
		t.Fatalf("yum: %+v", ev)
	}
	if ev[0].Action != "upgrade" || ev[0].Name != "bash" || ev[0].NewVersion != "4.2.46-35.el7" {
		t.Errorf("bash: %+v", ev[0])
	}
}

func TestParsePacmanLog(t *testing.T) {
	text := `[2024-05-01T06:25:03+0200] [PACMAN] Running 'pacman -Syu'
[2024-05-01T06:25:10+0200] [ALPM] upgraded bash (5.2.026-2 -> 5.2.026-3)
[2024-05-01T06:25:11+0200] [ALPM] installed htop (3.3.0-1)
[2024-05-01T06:25:12+0200] [ALPM] removed nano (7.2-1)
[2024-05-01T06:25:13+0200] [ALPM] downgraded curl (8.8.0-2 -> 8.8.0-1)
[2024-05-01T06:25:14+0200] [ALPM-SCRIPTLET] some output
`
	ev := parsePacmanLog(text, farPast)
	if len(ev) != 4 {
		t.Fatalf("events=%d: %+v", len(ev), ev)
	}
	if ev[0].Name != "bash" || ev[0].Action != "upgrade" || ev[0].OldVersion != "5.2.026-2" || ev[0].NewVersion != "5.2.026-3" {
		t.Errorf("bash: %+v", ev[0])
	}
	if ev[1].Action != "install" || ev[1].NewVersion != "3.3.0-1" || ev[2].Action != "remove" || ev[2].OldVersion != "7.2-1" {
		t.Errorf("install/remove: %+v %+v", ev[1], ev[2])
	}
	if ev[3].Action != "upgrade" || ev[3].NewVersion != "8.8.0-1" {
		t.Errorf("downgrade: %+v", ev[3])
	}
}

func TestParseSoftwareUpdateHistory(t *testing.T) {
	text := `Display Name                                       Version    Date
------------                                       -------    ----
macOS Sonoma 14.5                                  14.5       2024-05-14, 10:22:11
Safari                                             17.5       2024-05-14, 10:30:00
`
	ev := parseSoftwareUpdateHistory(text, farPast)
	if len(ev) != 2 || ev[0].Name != "macOS Sonoma 14.5" || ev[0].NewVersion != "14.5" || ev[1].Name != "Safari" {
		t.Fatalf("history: %+v", ev)
	}
}

func TestWuaHistoryEvents(t *testing.T) {
	rows := []wuaHistoryRow{
		{Title: "2024-10 Cumulative Update for Windows 11 (KB5044284)", Date: "2024-10-09T03:00:00Z", Operation: 1, ResultCode: 2},
		{Title: "Failed thing (KB1234567)", Date: "2024-10-09T03:00:00Z", Operation: 1, ResultCode: 4},
		{Title: "Old (KB7654321)", Date: "2020-01-01T00:00:00Z", Operation: 1, ResultCode: 2},
		{Title: "Removed app (KB1111111)", Date: "2024-10-10T03:00:00Z", Operation: 2, ResultCode: 3},
	}
	ev := wuaHistoryEvents(rows, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	if len(ev) != 2 || ev[0].Detail != "KB5044284" || ev[1].Action != "remove" || ev[1].Detail != "KB1111111 (mit Fehlern)" {
		t.Fatalf("wua: %+v", ev)
	}
}

func TestCapPatchHistory(t *testing.T) {
	ev := capPatchHistory(parsePacmanLog("[2024-05-01T06:25:10+0200] [ALPM] upgraded a (1 -> 2)\n[2024-05-02T06:25:10+0200] [ALPM] upgraded b (1 -> 2)\n[2024-05-03T06:25:10+0200] [ALPM] upgraded c (1 -> 2)\n", farPast), 2)
	if len(ev) != 2 || ev[0].Name != "c" || ev[1].Name != "b" {
		t.Fatalf("cap: %+v", ev)
	}
}
