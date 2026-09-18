package api

import (
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

func TestBackupDue(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("keine Zeitzonendatenbank")
	}
	at := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, berlin)
	}
	base := model.PolicyBackup{Enabled: true, AtTime: "18:00", CatchUpMinutes: 360}
	ptr := func(t time.Time) *time.Time { return &t }

	cases := []struct {
		name string
		b    model.PolicyBackup
		now  time.Time
		want bool
	}{
		{"vor der Zeit", base, at(2026, 9, 18, 17, 59), false},
		{"auf die Minute", base, at(2026, 9, 18, 18, 0), true},
		{"kurz danach", base, at(2026, 9, 18, 18, 30), true},
		{"innerhalb des Nachholfensters", base, at(2026, 9, 18, 23, 59), true},
		{"Nachholfenster abgelaufen", base, at(2026, 9, 19, 9, 0), false},
		{"deaktiviert", model.PolicyBackup{Enabled: false, AtTime: "18:00"}, at(2026, 9, 18, 18, 1), false},
		{"ohne Uhrzeit", model.PolicyBackup{Enabled: true}, at(2026, 9, 18, 18, 1), false},
	}
	for _, c := range cases {
		if _, got := backupDue(c.b, c.now); got != c.want {
			t.Errorf("%s: %v, erwartet %v", c.name, got, c.want)
		}
	}

	// Wochentage: 5 = Freitag.
	weekly := base
	weekly.Weekdays = "5"
	if _, ok := backupDue(weekly, at(2026, 9, 18, 18, 1)); !ok { // 18.9.2026 ist ein Freitag
		t.Error("Freitag muss laufen")
	}
	if _, ok := backupDue(weekly, at(2026, 9, 19, 18, 1)); ok {
		t.Error("Samstag darf nicht laufen")
	}

	// Schon geplant: derselbe Zeitpunkt darf kein zweites Mal kommen, der nächste Tag schon.
	done := base
	done.LastRunAt = ptr(at(2026, 9, 18, 18, 0))
	if _, ok := backupDue(done, at(2026, 9, 18, 18, 5)); ok {
		t.Error("für diesen Zeitpunkt wurde schon geplant")
	}
	if _, ok := backupDue(done, at(2026, 9, 19, 18, 1)); !ok {
		t.Error("am Folgetag muss wieder geplant werden")
	}

	// Sommerzeitende: 25.10.2026 hat 25 Stunden – „18:00" bleibt 18:00 Wanduhr.
	sched, ok := backupDue(base, at(2026, 10, 25, 18, 2))
	if !ok || sched.Hour() != 18 {
		t.Errorf("Zeitumstellung: %v %v", sched, ok)
	}
}

func TestBackupWeekdayAllowed(t *testing.T) {
	if !backupWeekdayAllowed("", time.Monday) {
		t.Error("leer = jeden Tag")
	}
	if !backupWeekdayAllowed("1, 3,5", time.Wednesday) || backupWeekdayAllowed("1,3,5", time.Tuesday) {
		t.Error("Liste falsch ausgewertet")
	}
	if !backupWeekdayAllowed("0", time.Sunday) {
		t.Error("0 muss Sonntag sein (time.Weekday)")
	}
}
