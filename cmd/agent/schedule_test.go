package main

import (
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func TestParseHHMM(t *testing.T) {
	if m, ok := parseHHMM("02:30"); !ok || m != 150 {
		t.Errorf("02:30 => %d,%v erwartet 150,true", m, ok)
	}
	if _, ok := parseHHMM("25:00"); ok {
		t.Error("25:00 sollte ungültig sein")
	}
	if _, ok := parseHHMM("abc"); ok {
		t.Error("abc sollte ungültig sein")
	}
}

func TestWeekdayAllowed(t *testing.T) {
	if !weekdayAllowed("", time.Monday) {
		t.Error("leer = alle Tage erlaubt")
	}
	if !weekdayAllowed("1,3,5", time.Wednesday) { // 3 = Mittwoch
		t.Error("Mittwoch (3) sollte erlaubt sein")
	}
	if weekdayAllowed("1,3,5", time.Tuesday) { // 2 = Dienstag
		t.Error("Dienstag (2) sollte nicht erlaubt sein")
	}
}

func TestTaskDueInterval(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.Local)
	task := shared.TaskSpec{ScheduleType: "interval", IntervalMinutes: 60}
	if !taskDue(task, time.Time{}, now) {
		t.Error("erstmals (last=zero) sollte fällig sein")
	}
	if taskDue(task, now.Add(-30*time.Minute), now) {
		t.Error("vor 30 Min gelaufen, Intervall 60 -> nicht fällig")
	}
	if !taskDue(task, now.Add(-90*time.Minute), now) {
		t.Error("vor 90 Min gelaufen, Intervall 60 -> fällig")
	}
}

func TestTaskDueDaily(t *testing.T) {
	// Sonntag, 28.6.2026, 03:00 lokal
	now := time.Date(2026, 6, 28, 3, 0, 0, 0, time.Local)
	task := shared.TaskSpec{ScheduleType: "daily", DailyTime: "02:00"}
	if !taskDue(task, time.Time{}, now) {
		t.Error("nach 02:00 und heute noch nicht gelaufen -> fällig")
	}
	// heute schon gelaufen (um 02:30)
	already := time.Date(2026, 6, 28, 2, 30, 0, 0, time.Local)
	if taskDue(task, already, now) {
		t.Error("heute bereits gelaufen -> nicht fällig")
	}
	// vor der Uhrzeit
	before := time.Date(2026, 6, 28, 1, 0, 0, 0, time.Local)
	if taskDue(task, time.Time{}, before) {
		t.Error("vor 02:00 -> nicht fällig")
	}
	// gestern gelaufen, jetzt nach Uhrzeit -> fällig
	yesterday := time.Date(2026, 6, 27, 2, 30, 0, 0, time.Local)
	if !taskDue(task, yesterday, now) {
		t.Error("gestern gelaufen, heute nach 02:00 -> fällig")
	}
}

// TestTaskDueTimezone belegt, dass der Zeitplan in der übergebenen Zone gilt: derselbe
// Augenblick ist in Berlin schon der nächste Tag, in London noch der vorige – ein
// Tages-Task darf dann genau einmal je Berliner Tag laufen.
func TestTaskDueTimezone(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("Zeitzone nicht ladbar: %v", err)
	}
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("Zeitzone nicht ladbar: %v", err)
	}
	task := shared.TaskSpec{Frequency: "daily", DailyTime: "20:00"}

	// 2026-09-18 19:30 UTC = 21:30 Berlin (fällig) = 20:30 London (auch fällig).
	instant := time.Date(2026, 9, 18, 19, 30, 0, 0, time.UTC)
	if !taskDue(task, time.Time{}, instant.In(berlin)) {
		t.Error("21:30 Berliner Zeit sollte einen 20:00-Task fällig machen")
	}

	// 2026-09-18 18:30 UTC = 20:30 Berlin (fällig), aber 19:30 London (noch nicht).
	instant = time.Date(2026, 9, 18, 18, 30, 0, 0, time.UTC)
	if !taskDue(task, time.Time{}, instant.In(berlin)) {
		t.Error("20:30 Berliner Zeit sollte fällig sein")
	}
	if taskDue(task, time.Time{}, instant.In(london)) {
		t.Error("19:30 Londoner Zeit ist noch nicht fällig – die Zone entscheidet")
	}

	// Tagesgrenze ohne Uhrzeit: 22:30 UTC ist in Berlin schon der 19., in London noch
	// der 18. – derselbe Augenblick, zwei verschiedene Antworten.
	daily := shared.TaskSpec{Frequency: "daily"}
	last := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC) // Lauf am 18. (beide Zonen)
	instant = time.Date(2026, 9, 18, 22, 30, 0, 0, time.UTC)
	if taskDue(daily, last.In(london), instant.In(london)) {
		t.Error("in London noch derselbe Tag – kein zweiter Lauf")
	}
	if !taskDue(daily, last.In(berlin), instant.In(berlin)) {
		t.Error("in Berlin ist bereits der 19. – wieder fällig")
	}
}
