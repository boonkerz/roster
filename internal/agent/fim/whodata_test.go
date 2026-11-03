package fim

import (
	"strings"
	"testing"
)

func TestParseAusearch(t *testing.T) {
	raw := `type=PROCTITLE msg=audit(1762164000.123:4567): proctitle=76696D002F6574632F706173737764
type=PATH msg=audit(1762164000.123:4567): item=1 name="/etc/passwd" inode=1234 dev=fd:01 mode=0100644 ouid=0 ogid=0 rdev=00:00 nametype=NORMAL cap_fp=0
type=PATH msg=audit(1762164000.123:4567): item=0 name="/etc/" inode=2 dev=fd:01 mode=040755 ouid=0 ogid=0 rdev=00:00 nametype=PARENT cap_fp=0
type=CWD msg=audit(1762164000.123:4567): cwd="/root"
type=SYSCALL msg=audit(1762164000.123:4567): arch=c000003e syscall=257 success=yes exit=3 a0=ffffff9c items=2 ppid=1 pid=2 auid=1000 uid=0 gid=0 euid=0 tty=pts0 ses=3 comm="vim" exe="/usr/bin/vim.basic" key="roster_fim"
----
type=PATH msg=audit(1762164005.000:4570): item=0 name=2F6574632F6D7920646972 nametype=CREATE
type=CWD msg=audit(1762164005.000:4570): cwd="/etc"
type=SYSCALL msg=audit(1762164005.000:4570): syscall=83 success=yes auid=4294967295 uid=0 comm="mkdir" exe="/usr/bin/mkdir" key="roster_fim"
----
type=PATH msg=audit(1762164006.000:4571): item=0 name="hosts" nametype=DELETE
type=CWD msg=audit(1762164006.000:4571): cwd="/etc"
type=SYSCALL msg=audit(1762164006.000:4571): syscall=87 success=no auid=1000 uid=1000 comm="rm" exe="/usr/bin/rm" key="roster_fim"
----
type=PATH msg=audit(1762164007.000:4572): item=0 name="shadow" nametype=NORMAL
type=CWD msg=audit(1762164007.000:4572): cwd="/etc"
type=SYSCALL msg=audit(1762164007.000:4572): syscall=257 success=yes auid=1000 uid=0 comm="vim" exe="/usr/bin/vim.basic" key="other_key"
`
	who := ParseAusearch(raw, func(uid string) string {
		if uid == "1000" {
			return "thomas"
		}
		return ""
	})
	if w, ok := who["/etc/passwd"]; !ok || w.User != "thomas" || w.Process != "/usr/bin/vim.basic (vim)" || w.Time.Unix() != 1762164000 {
		t.Fatalf("passwd: %+v %v", w, ok)
	}
	if w, ok := who["/etc/my dir"]; !ok || w.User != "0" || !strings.HasPrefix(w.Process, "/usr/bin/mkdir") {
		t.Fatalf("hex name/relativer Pfad: %+v %v", w, ok)
	}
	if _, ok := who["/etc/hosts"]; ok {
		t.Fatal("fehlgeschlagener Syscall darf nicht zählen")
	}
	if _, ok := who["/etc/shadow"]; ok {
		t.Fatal("fremder Schlüssel darf nicht zählen")
	}
}

func TestParseAuditRules(t *testing.T) {
	out := "-w /etc -p wa -k roster_fim\n-w /usr/local/bin -p wa -k roster_fim\n-w /var/log -p wa -k logs\n-a always,exit -F arch=b64 -S execve -k exec\n"
	got := ParseAuditRules(out)
	if len(got) != 2 || !got["/etc"] || !got["/usr/local/bin"] {
		t.Fatalf("rules: %v", got)
	}
}

func TestMapWindowsWho(t *testing.T) {
	rows := []WinWhoRow{
		{ID: 4663, Time: "2026-11-03T10:00:00Z", ObjectName: `C:\Windows\Tasks\x.job`, Domain: "CORP", User: "bob", Process: `C:\Windows\explorer.exe`, HandleID: "0x1a"},
		{ID: 4660, Time: "2026-11-03T10:00:01Z", Domain: "CORP", User: "bob", Process: `C:\Windows\explorer.exe`, HandleID: "0x1a"},
		{ID: 4663, Time: "2026-11-03T10:00:02Z", ObjectName: `C:\Windows\Tasks\y.job`, Domain: "NT AUTHORITY", User: "SYSTEM$", Process: `svchost.exe`, HandleID: "0x2b"},
	}
	who := MapWindowsWho(rows)
	w, ok := who[`c:\windows\tasks\x.job`]
	if !ok || w.User != `CORP\bob` || w.Time.Second() != 1 {
		t.Fatalf("x.job: %+v %v", w, ok)
	}
	if _, ok := who[`c:\windows\tasks\y.job`]; ok {
		t.Fatal("Maschinenkonto muss raus")
	}
}
