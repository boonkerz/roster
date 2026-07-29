package policy

import (
	"testing"

	"github.com/boonkerz/roster/internal/shared"
)

func p2Spec(cfg map[string]any) shared.CheckSpec { return shared.CheckSpec{ID: "1", Config: cfg} }

func TestEvalCertDays(t *testing.T) {
	if r := evalCertDays(p2Spec(nil), "x.de", 40, 14); r.Status != "passing" {
		t.Fatalf("40 Tage -> passing: %+v", r)
	}
	if r := evalCertDays(p2Spec(nil), "x.de", 5, 14); r.Status != "failing" {
		t.Fatalf("5 Tage < 14 -> failing: %+v", r)
	}
	if r := evalCertDays(p2Spec(nil), "x.de", -3, 14); r.Status != "failing" {
		t.Fatalf("abgelaufen -> failing: %+v", r)
	}
}

func TestEvalServiceRunning(t *testing.T) {
	js := `{"services":[{"name":"nginx","running":true},{"name":"cron","display":"Cron Daemon","running":false}]}`
	if r := evalServiceRunning(p2Spec(map[string]any{"name": "nginx"}), js); r.Status != "passing" {
		t.Fatalf("nginx läuft -> passing: %+v", r)
	}
	if r := evalServiceRunning(p2Spec(map[string]any{"name": "cron"}), js); r.Status != "failing" {
		t.Fatalf("cron gestoppt -> failing: %+v", r)
	}
	if r := evalServiceRunning(p2Spec(map[string]any{"name": "gibtsnicht"}), js); r.Status != "failing" {
		t.Fatalf("fehlt -> failing: %+v", r)
	}
	if r := evalServiceRunning(p2Spec(nil), js); r.Status != "unknown" {
		t.Fatalf("kein name -> unknown: %+v", r)
	}
	if r := evalServiceRunning(p2Spec(map[string]any{"name": "x"}), `{"error":"kein systemd"}`); r.Status != "unknown" {
		t.Fatalf("error -> unknown: %+v", r)
	}
}

func TestEvalProcessRunning(t *testing.T) {
	js := `{"processes":[{"pid":1,"name":"systemd"},{"pid":22,"name":"dockerd"}]}`
	if r := evalProcessRunning(p2Spec(map[string]any{"name": "dockerd"}), js); r.Status != "passing" {
		t.Fatalf("dockerd -> passing: %+v", r)
	}
	if r := evalProcessRunning(p2Spec(map[string]any{"name": "postgres"}), js); r.Status != "failing" {
		t.Fatalf("postgres fehlt -> failing: %+v", r)
	}
}

func TestEvalUptime(t *testing.T) {
	day := 86400.0
	if r := evalUptime(p2Spec(nil), 5*day, 30); r.Status != "passing" {
		t.Fatalf("5 Tage -> passing: %+v", r)
	}
	if r := evalUptime(p2Spec(nil), 45*day, 30); r.Status != "failing" {
		t.Fatalf("45 Tage > 30 -> failing: %+v", r)
	}
	if r := evalUptime(p2Spec(map[string]any{"max_days": 60.0}), 45*day, 60); r.Status != "passing" {
		t.Fatalf("max_days 60 -> passing: %+v", r)
	}
}
