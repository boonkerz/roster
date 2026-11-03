package logmon

import (
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestMapDockerEvent(t *testing.T) {
	src := shared.LogSource{ID: "s1", Name: "docker", Type: "docker_events", Target: "web*"}
	now := time.Now()
	ev, ok := mapDockerEvent(src, `{"status":"exec_create: /bin/sh -c id","id":"abc","from":"nginx","Type":"container","Action":"exec_create: /bin/sh -c id","Actor":{"ID":"abc","Attributes":{"image":"nginx","name":"web-1"}},"scope":"local","time":1762164000,"timeNano":1762164000123456789}`, now)
	if !ok || ev.Kind != "docker_exec" || ev.Detail != "web-1: /bin/sh -c id" || ev.RecordID == "" || ev.Time.Unix() != 1762164000 {
		t.Errorf("exec: %+v %v", ev, ok)
	}
	ev, ok = mapDockerEvent(src, `{"Type":"container","Action":"die","Actor":{"ID":"abc","Attributes":{"image":"nginx","name":"web-1","exitCode":"137"}},"timeNano":1762164001000000000}`, now)
	if !ok || ev.Kind != "docker_event" || ev.Detail != "die web-1 (nginx, exit=137)" {
		t.Errorf("die: %+v %v", ev, ok)
	}
	if _, ok := mapDockerEvent(src, `{"Type":"container","Action":"start","Actor":{"Attributes":{"name":"db-1"}},"timeNano":1}`, now); ok {
		t.Error("db-1 passt nicht auf web*")
	}
	if _, ok := mapDockerEvent(src, `{"Type":"network","Action":"connect","Actor":{"Attributes":{"name":"web-1"}},"timeNano":1}`, now); ok {
		t.Error("Netzwerk-Ereignisse ignorieren")
	}
	if _, ok := mapDockerEvent(src, `{"Type":"container","Action":"exec_start: id","Actor":{"Attributes":{"name":"web-1"}},"timeNano":1}`, now); ok {
		t.Error("exec_start doppelt zu exec_create")
	}
}

func TestJournalArgs(t *testing.T) {
	if a, ok := journalArgs("kernel"); !ok || len(a) != 1 || a[0] != "-k" {
		t.Errorf("kernel: %v %v", a, ok)
	}
	if a, ok := journalArgs("sshd"); !ok || a[0] != "SYSLOG_IDENTIFIER=sshd" {
		t.Errorf("ident: %v %v", a, ok)
	}
	if _, ok := journalArgs("a b; rm"); ok {
		t.Error("ungültiges Ziel muss abgelehnt werden")
	}
	jl := journalLine{Message: []byte(`"[UFW BLOCK] IN=eth0 OUT= SRC=1.2.3.4"`), Realtime: "1762164000123456"}
	if jl.text() == "" || jl.time(time.Now()).Unix() != 1762164000 {
		t.Errorf("journal line: %q %v", jl.text(), jl.time(time.Now()))
	}
	if (journalLine{Message: []byte(`[1,2,3]`)}).text() != "" {
		t.Error("Byte-Array muss leer sein")
	}
}
