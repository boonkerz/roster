package rootcheck

import (
	"testing"
	"time"
)

func TestParseClamOutput(t *testing.T) {
	out := "/var/www/x.php: Php.Malware.Webshell-1 FOUND\n/tmp/ok.txt: OK\n/tmp/a: b: Eicar-Test-Signature FOUND\n\n"
	hits := ParseClamOutput(out)
	if len(hits) != 2 || hits[0].Path != "/var/www/x.php" || hits[0].Signature != "Php.Malware.Webshell-1" || hits[1].Path != "/tmp/a: b" || hits[1].Signature != "Eicar-Test-Signature" {
		t.Fatalf("hits: %+v", hits)
	}
}

func TestParseDefenderOutput(t *testing.T) {
	out := "Scan starting...\r\nScan finished.\r\nScanning C:\\x.exe found 1 threats.\r\n<===========================LIST OF DETECTED THREATS==========================>\r\n----------------------------- Threat information ------------------------------\r\nThreat                  : Virus:DOS/EICAR_Test_File\r\nResources               : 1 total\r\n"
	if got := ParseDefenderOutput(out); got != "Virus:DOS/EICAR_Test_File" {
		t.Fatalf("got %q", got)
	}
	if got := ParseDefenderOutput("Scan finished.\nNo threats found."); got != "" {
		t.Fatalf("leer erwartet: %q", got)
	}
}

func TestEvents(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	ev := Events([]Finding{{Check: "versteckter Prozess", Item: "PID 4711"}}, now)
	if len(ev) != 1 || ev[0].Kind != "rootkit" || ev[0].Detail != "versteckter Prozess: PID 4711" || ev[0].RecordID == "" {
		t.Fatalf("%+v", ev)
	}
	ev2 := Events([]Finding{{Check: "versteckter Prozess", Item: "PID 4711"}}, now.Add(time.Hour))
	if ev2[0].RecordID != ev[0].RecordID {
		t.Fatal("gleicher Befund am selben Tag muss dieselbe RecordID haben")
	}
	mw := MalwareEvents([]Hit{{Path: "/tmp/x", Signature: "Eicar"}}, "clamdscan", now)
	if len(mw) != 1 || mw[0].Kind != "malware" || mw[0].Service != "clamdscan" || mw[0].Detail != "/tmp/x: Eicar" {
		t.Fatalf("%+v", mw)
	}
}
