package authlog

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseSyslogLines(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)
	cases := []struct {
		line           string
		kind, user, ip string
	}{
		{"Oct 31 10:00:01 srv sshd[123]: Failed password for root from 203.0.113.5 port 5022 ssh2", "login_failed", "root", "203.0.113.5"},
		{"Oct 31 10:00:02 srv sshd[124]: Failed password for invalid user admin from 203.0.113.5 port 5023 ssh2", "invalid_user", "admin", "203.0.113.5"},
		{"2026-10-31T10:00:03.123456+01:00 srv sshd[125]: Accepted publickey for thomas from 192.168.1.10 port 51000 ssh2: ED25519 SHA256:abc", "login_ok", "thomas", "192.168.1.10"},
		{"Oct 31 10:00:04 srv sshd[126]: Invalid user test from 203.0.113.9 port 1", "invalid_user", "test", "203.0.113.9"},
		{"Oct 31 10:00:05 srv sshd[127]: error: maximum authentication attempts exceeded for root from 203.0.113.5 port 5024 ssh2 [preauth]", "login_failed", "root", "203.0.113.5"},
		{"Oct 31 10:00:06 srv sudo:   thomas : TTY=pts/0 ; PWD=/home/thomas ; USER=root ; COMMAND=/usr/bin/apt update", "sudo", "thomas", ""},
		{"Oct 31 10:00:07 srv sudo:   thomas : 3 incorrect password attempts ; TTY=pts/0 ; PWD=/ ; USER=root ; COMMAND=/bin/ls", "login_failed", "thomas", ""},
		{"Oct 31 10:00:08 srv su[200]: (to root) thomas on pts/1", "su", "thomas", ""},
		{"Oct 31 10:00:09 srv su[201]: FAILED SU (to root) bob on pts/1", "login_failed", "bob", ""},
		{"Oct 31 10:00:10 srv useradd[300]: new user: name=bob, UID=1001, GID=1001, home=/home/bob, shell=/bin/bash, from=/dev/pts/0", "user_added", "bob", ""},
		{"Oct 31 10:00:11 srv userdel[301]: delete user 'bob'", "user_removed", "bob", ""},
		{"Oct 31 10:00:12 srv usermod[302]: add 'bob' to group 'sudo'", "group_changed", "bob", ""},
		{"Oct 31 10:00:13 srv gpasswd[303]: user carol added by root to group wheel", "group_changed", "carol", ""},
		{"Oct 31 10:00:14 srv passwd[304]: pam_unix(passwd:chauthtok): password changed for bob", "password_changed", "bob", ""},
		{"Oct 31 10:00:15 srv login[400]: FAILED LOGIN (1) on '/dev/tty1' FOR 'root', Authentication failure", "login_failed", "root", ""},
		{"Oct 31 10:00:16 srv login[401]: pam_unix(login:session): session opened for user root(uid=0) by LOGIN(uid=0)", "login_ok", "root", ""},
	}
	for _, c := range cases {
		ev, ok := ParseSyslogLine(c.line, now)
		if !ok {
			t.Errorf("nicht erkannt: %s", c.line)
			continue
		}
		if ev.Kind != c.kind || ev.User != c.user || ev.SourceIP != c.ip {
			t.Errorf("%s\n  got %s %q %q want %s %q %q", c.line, ev.Kind, ev.User, ev.SourceIP, c.kind, c.user, c.ip)
		}
		if ev.Time.Year() != 2026 || ev.Time.Month() != 10 || ev.Time.Day() != 31 {
			t.Errorf("zeit: %v", ev.Time)
		}
	}
	noise := []string{
		"Oct 31 10:00:00 srv sshd[1]: Connection closed by authenticating user root 203.0.113.5 port 1 [preauth]",
		"Oct 31 10:00:00 srv sshd[1]: pam_unix(sshd:auth): authentication failure; logname= uid=0 euid=0 tty=ssh ruser= rhost=203.0.113.5  user=root",
		"Oct 31 10:00:00 srv sshd[1]: pam_unix(sshd:session): session opened for user thomas(uid=1000) by (uid=0)",
		"Oct 31 10:00:00 srv systemd-logind[1]: New session 5 of user thomas.",
		"Oct 31 10:00:00 srv useradd[300]: new group: name=bob, GID=1001",
		"Oct 31 10:00:00 srv CRON[1]: pam_unix(cron:session): session opened for user root",
		"garbage line",
	}
	for _, l := range noise {
		if ev, ok := ParseSyslogLine(l, now); ok {
			t.Errorf("Rauschen erkannt: %s → %+v", l, ev)
		}
	}
	// Jahreswechsel: Dezember-Zeile im Januar gehört ins Vorjahr.
	ev, _ := ParseSyslogLine("Dec 31 23:59:59 srv sshd[1]: Accepted password for a from 1.1.1.1 port 1", time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local))
	if ev.Time.Year() != 2026 {
		t.Errorf("jahreswechsel: %v", ev.Time)
	}
}

func TestParseWindowsEvent(t *testing.T) {
	mk := func(id int, data map[string]string) WinEvent {
		return WinEvent{ID: id, Time: "2026-10-31T10:00:00Z", RecordID: 42, Data: data}
	}
	ev, ok := ParseWindowsEvent(mk(4625, map[string]string{"TargetUserName": "Administrator", "IpAddress": "203.0.113.7", "LogonType": "10", "Status": "0xc000006d"}))
	if !ok || ev.Kind != "login_failed" || ev.Service != "rdp" || ev.SourceIP != "203.0.113.7" || ev.RecordID != "42" {
		t.Errorf("4625: %+v %v", ev, ok)
	}
	ev, ok = ParseWindowsEvent(mk(4625, map[string]string{"TargetUserName": "nobody", "IpAddress": "203.0.113.7", "LogonType": "3", "Status": "0xc000006d", "SubStatus": "0xc0000064"}))
	if !ok || ev.Kind != "invalid_user" || ev.Service != "network" {
		t.Errorf("4625 unknown user: %+v %v", ev, ok)
	}
	if _, ok := ParseWindowsEvent(mk(4624, map[string]string{"TargetUserName": "WIN-PC$", "LogonType": "3"})); ok {
		t.Error("Maschinenkonto muss raus")
	}
	if _, ok := ParseWindowsEvent(mk(4624, map[string]string{"TargetUserName": "SYSTEM", "LogonType": "5"})); ok {
		t.Error("SYSTEM/Service-Logon muss raus")
	}
	ev, ok = ParseWindowsEvent(mk(4624, map[string]string{"TargetUserName": "thomas", "LogonType": "10", "IpAddress": "192.168.1.5"}))
	if !ok || ev.Kind != "login_ok" || ev.Service != "rdp" {
		t.Errorf("4624 rdp: %+v %v", ev, ok)
	}
	ev, ok = ParseWindowsEvent(mk(4732, map[string]string{"MemberName": "CN=Bob,CN=Users,DC=x", "TargetUserName": "Administrators", "SubjectUserName": "admin"}))
	if !ok || ev.Kind != "group_changed" || ev.User != "Bob" || ev.Detail != "group Administrators by admin" {
		t.Errorf("4732: %+v %v", ev, ok)
	}
	ev, ok = ParseWindowsEvent(mk(4740, map[string]string{"TargetUserName": "bob", "TargetDomainName": "PC1"}))
	if !ok || ev.Kind != "lockout" {
		t.Errorf("4740: %+v", ev)
	}
	ev, ok = ParseWindowsEvent(mk(4720, map[string]string{"TargetUserName": "newguy", "SubjectUserName": "admin"}))
	if !ok || ev.Kind != "user_added" || ev.User != "newguy" {
		t.Errorf("4720: %+v", ev)
	}
}

func TestCollectFileIncremental(t *testing.T) {
	dir := t.TempDir()
	log := dir + "/auth.log"
	write := func(s string) {
		f, _ := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString(s)
		_ = f.Close()
	}
	write("Oct 31 10:00:01 srv sshd[1]: Failed password for root from 203.0.113.5 port 1 ssh2\n")
	t.Setenv("ROSTER_AUTHLOG_PATH", log)
	// Erster Lauf: springt ans Ende (kein Altbestand).
	if ev := Collect(context.Background(), dir); len(ev) != 0 {
		t.Fatalf("erster lauf: %+v", ev)
	}
	write("Oct 31 10:00:02 srv sshd[1]: Failed password for root from 203.0.113.5 port 2 ssh2\nOct 31 10:00:03 srv sshd[1]: Accepted publickey for t from 10.0.0.1 port 3 ssh2\nOct 31 10:00:04 srv sshd[1]: partial")
	ev := Collect(context.Background(), dir)
	if len(ev) != 2 || ev[0].Kind != "login_failed" || ev[1].Kind != "login_ok" || ev[0].RecordID == "" || ev[0].RecordID == ev[1].RecordID {
		t.Fatalf("zweiter lauf: %+v", ev)
	}
	// Nichts Neues (unvollständige Zeile bleibt liegen) → keine Duplikate.
	if ev := Collect(context.Background(), dir); len(ev) != 0 {
		t.Fatalf("dritter lauf: %+v", ev)
	}
	write(" line\nOct 31 10:00:05 srv sudo:   t : TTY=pts/0 ; PWD=/ ; USER=root ; COMMAND=/bin/id\n")
	if ev := Collect(context.Background(), dir); len(ev) != 1 || ev[0].Kind != "sudo" {
		t.Fatalf("vierter lauf: %+v", ev)
	}
	// Rotation: neue, kürzere Datei → von vorn lesen.
	_ = os.Remove(log)
	write("Oct 31 11:00:00 srv sshd[1]: Accepted password for r from 10.0.0.2 port 1 ssh2\n")
	if ev := Collect(context.Background(), dir); len(ev) != 1 || ev[0].User != "r" {
		t.Fatalf("rotation: %+v", ev)
	}
}
