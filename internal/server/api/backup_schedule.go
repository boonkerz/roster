package api

import (
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// Zeitplan der Backup-Einträge. Bewusst eine reine Funktion: Wochenrhythmus, Nachholen
// nach einer Server-Auszeit und Sommerzeit lassen sich so tabellengetrieben testen.
//
// Gerechnet wird in der Zone von now – der Aufrufer (RunBackupLoop) übergibt die zentral
// eingestellte Zeitzone, siehe timezone.go. Ist keine gesetzt, ist das die Serverzeit.

// backupDue meldet, ob ein Eintrag jetzt fällig ist, und den geplanten Zeitpunkt des
// Laufs (Wanduhr-Zeit von heute). Ein Lauf ist fällig, wenn
//   - der Wochentag passt (leer = täglich),
//   - die Uhrzeit erreicht ist,
//   - seit dem geplanten Zeitpunkt höchstens catch_up_minutes vergangen sind
//     (danach wird ein verpasster Lauf nicht mehr nachgeholt – sonst startet ein
//     Backup mitten am nächsten Vormittag, wenn der Server nachts aus war),
//   - und für diesen Zeitpunkt noch nicht geplant wurde (last_run_at).
func backupDue(b model.PolicyBackup, now time.Time) (time.Time, bool) {
	if !b.Enabled {
		return time.Time{}, false
	}
	hh, mm, ok := parseClock(b.AtTime)
	if !ok {
		return time.Time{}, false
	}
	if !backupWeekdayAllowed(b.Weekdays, now.Weekday()) {
		return time.Time{}, false
	}
	scheduled := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
	if now.Before(scheduled) {
		return time.Time{}, false
	}
	catchUp := time.Duration(b.CatchUpMinutes) * time.Minute
	if catchUp <= 0 {
		catchUp = 6 * time.Hour
	}
	if now.Sub(scheduled) > catchUp {
		return time.Time{}, false
	}
	if b.LastRunAt != nil && !b.LastRunAt.Before(scheduled) {
		return time.Time{}, false // für diesen Zeitpunkt schon geplant
	}
	return scheduled, true
}

// backupWeekdayAllowed prüft die Wochentagsliste ("1,3,5", time.Weekday, 0 = Sonntag).
// Leer heißt „jeden Tag" – gleiche Schreibweise wie bei Tasks im Agent.
func backupWeekdayAllowed(weekdays string, wd time.Weekday) bool {
	weekdays = strings.TrimSpace(weekdays)
	if weekdays == "" {
		return true
	}
	for _, part := range strings.Split(weekdays, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && time.Weekday(n) == wd {
			return true
		}
	}
	return false
}

// parseClock liest "HH:MM".
func parseClock(s string) (hour, minute int, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}
