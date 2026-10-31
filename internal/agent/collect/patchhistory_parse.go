package collect

import (
	"bufio"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Reine Parser für Patch-Verläufe (plattformneutral, unit-getestet). Die OS-Dateien
// lesen nur die Logs und reichen den Text hier durch. since schneidet ältere Einträge ab.

// aptHistoryItem matcht "pkg:arch (a, b)" bzw. "pkg:arch (a)" in apt-Verlaufszeilen.
var aptHistoryItem = regexp.MustCompile(`([^\s,()]+) \(([^)]*)\)`)

// parseAptHistory liest /var/log/apt/history.log (Blöcke mit Start-Date, Commandline,
// Install/Upgrade/Remove/Purge/Downgrade/Reinstall, End-Date).
func parseAptHistory(text string, since time.Time) []shared.PatchEvent {
	var out []shared.PatchEvent
	var start time.Time
	var cmdline string
	var pending []shared.PatchEvent
	flush := func() {
		if !start.IsZero() && !start.Before(since) {
			for i := range pending {
				pending[i].Time = start
				pending[i].Detail = cmdline
			}
			out = append(out, pending...)
		}
		pending, start, cmdline = nil, time.Time{}, ""
	}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "Start-Date":
			flush()
			start = parseAptTime(val)
		case "Commandline":
			cmdline = val
		case "End-Date":
			flush()
		case "Install", "Upgrade", "Remove", "Purge", "Downgrade", "Reinstall":
			for _, m := range aptHistoryItem.FindAllStringSubmatch(val, -1) {
				name, _, _ := strings.Cut(m[1], ":") // Architektur abtrennen
				parts := strings.Split(m[2], ",")
				for i := range parts {
					parts[i] = strings.TrimSpace(parts[i])
				}
				ev := shared.PatchEvent{Name: name, Source: "apt"}
				switch key {
				case "Install", "Reinstall":
					ev.Action = "install"
					if len(parts) > 0 {
						ev.NewVersion = parts[0]
					}
				case "Upgrade", "Downgrade":
					ev.Action = "upgrade"
					if len(parts) >= 2 {
						ev.OldVersion, ev.NewVersion = parts[0], parts[1]
					} else if len(parts) == 1 {
						ev.NewVersion = parts[0]
					}
				default: // Remove, Purge
					ev.Action = "remove"
					if len(parts) > 0 {
						ev.OldVersion = parts[0]
					}
				}
				pending = append(pending, ev)
			}
		}
	}
	flush()
	return out
}

// parseAptTime liest "2024-05-01  06:25:03" (lokale Zeit).
func parseAptTime(s string) time.Time {
	f := strings.Fields(s)
	if len(f) < 2 {
		return time.Time{}
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", f[0]+" "+f[1], time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}

// rpmVersion trennt aus einem NEVRA-String die Version-Release (ohne Arch).
func rpmVersion(nevra string) string {
	name := rpmName(nevra)
	v := strings.TrimPrefix(nevra, name+"-")
	if i := strings.LastIndex(v, "."); i > 0 {
		v = v[:i]
	}
	return v
}

// parseDnfRpmLog liest /var/log/dnf.rpm.log:
//
//	2024-05-01T06:25:03+0200 SUBDEBUG Upgrade: bash-5.2.26-3.fc40.x86_64   (neues Paket)
//	2024-05-01T06:25:04+0200 SUBDEBUG Upgraded: bash-5.2.26-2.fc40.x86_64  (altes Paket)
//	... Installed: / Erased: / Downgrade: / Downgraded: / Reinstalled: / Obsoleted:
func parseDnfRpmLog(text string, since time.Time) []shared.PatchEvent {
	var out []shared.PatchEvent
	oldVer := map[string]string{} // Name -> alte Version (aus "Upgraded:"/"Downgraded:")
	type pend struct {
		idx  int
		name string
	}
	var upgrades []pend
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		ts, err := time.Parse("2006-01-02T15:04:05-0700", f[0])
		if err != nil {
			if ts, err = time.Parse(time.RFC3339, f[0]); err != nil {
				continue
			}
		}
		if ts.Before(since) {
			continue
		}
		verb := strings.TrimSuffix(f[2], ":")
		nevra := f[3]
		name, ver := rpmName(nevra), rpmVersion(nevra)
		switch verb {
		case "Installed", "Reinstalled":
			out = append(out, shared.PatchEvent{Time: ts, Action: "install", Name: name, NewVersion: ver, Source: "dnf"})
		case "Erased":
			out = append(out, shared.PatchEvent{Time: ts, Action: "remove", Name: name, OldVersion: ver, Source: "dnf"})
		case "Upgrade", "Downgrade":
			out = append(out, shared.PatchEvent{Time: ts, Action: "upgrade", Name: name, NewVersion: ver, Source: "dnf"})
			upgrades = append(upgrades, pend{len(out) - 1, name})
		case "Upgraded", "Downgraded", "Obsoleted":
			oldVer[name] = ver
		}
	}
	for _, u := range upgrades {
		if v, ok := oldVer[u.name]; ok {
			out[u.idx].OldVersion = v
		}
	}
	return out
}

// parseYumLog liest /var/log/yum.log ("May 01 06:25:03 Updated: bash-4.2.46-35.el7.x86_64");
// das Jahr fehlt – es wird aus now abgeleitet (liegt der Eintrag in der Zukunft, Vorjahr).
func parseYumLog(text string, since, now time.Time) []shared.PatchEvent {
	var out []shared.PatchEvent
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 {
			continue
		}
		ts, err := time.ParseInLocation("Jan 02 15:04:05 2006", f[0]+" "+f[1]+" "+f[2]+" "+now.Format("2006"), time.Local)
		if err != nil {
			continue
		}
		if ts.After(now) {
			ts = ts.AddDate(-1, 0, 0)
		}
		if ts.Before(since) {
			continue
		}
		name, ver := rpmName(f[4]), rpmVersion(f[4])
		switch strings.TrimSuffix(f[3], ":") {
		case "Installed":
			out = append(out, shared.PatchEvent{Time: ts, Action: "install", Name: name, NewVersion: ver, Source: "yum"})
		case "Updated":
			out = append(out, shared.PatchEvent{Time: ts, Action: "upgrade", Name: name, NewVersion: ver, Source: "yum"})
		case "Erased":
			out = append(out, shared.PatchEvent{Time: ts, Action: "remove", Name: name, OldVersion: ver, Source: "yum"})
		}
	}
	return out
}

var pacmanLogLine = regexp.MustCompile(`^\[([^\]]+)\] \[ALPM\] (installed|upgraded|downgraded|removed|reinstalled) (\S+) \(([^)]*)\)`)

// parsePacmanLog liest /var/log/pacman.log:
//
//	[2024-05-01T06:25:03+0200] [ALPM] upgraded bash (5.2.026-2 -> 5.2.026-3)
func parsePacmanLog(text string, since time.Time) []shared.PatchEvent {
	var out []shared.PatchEvent
	for _, line := range strings.Split(text, "\n") {
		m := pacmanLogLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ts, err := time.Parse("2006-01-02T15:04:05-0700", m[1])
		if err != nil {
			// Altes Format ohne Zone: "2019-01-01 10:00"
			if ts, err = time.ParseInLocation("2006-01-02 15:04", m[1], time.Local); err != nil {
				continue
			}
		}
		if ts.Before(since) {
			continue
		}
		ev := shared.PatchEvent{Time: ts, Name: m[3], Source: "pacman"}
		old, nw, hasArrow := strings.Cut(m[4], " -> ")
		switch m[2] {
		case "installed", "reinstalled":
			ev.Action, ev.NewVersion = "install", m[4]
		case "removed":
			ev.Action, ev.OldVersion = "remove", m[4]
		default:
			ev.Action = "upgrade"
			if hasArrow {
				ev.OldVersion, ev.NewVersion = old, nw
			} else {
				ev.NewVersion = m[4]
			}
		}
		out = append(out, ev)
	}
	return out
}

// parseSoftwareUpdateHistory liest `softwareupdate --history` (macOS) – Spalten
// „Display Name“, „Version“, „Date“ mit fester Breite laut Kopfzeile.
func parseSoftwareUpdateHistory(text string, since time.Time) []shared.PatchEvent {
	lines := strings.Split(text, "\n")
	var out []shared.PatchEvent
	verCol, dateCol := -1, -1
	for _, line := range lines {
		if verCol < 0 {
			if i := strings.Index(line, "Version"); i > 0 && strings.Contains(line, "Date") {
				verCol, dateCol = i, strings.Index(line, "Date")
			}
			continue
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "---") || len(line) <= dateCol {
			continue
		}
		name := strings.TrimSpace(line[:verCol])
		ver := strings.TrimSpace(line[verCol:dateCol])
		date := strings.TrimSpace(line[dateCol:])
		ts, err := time.ParseInLocation("2006-01-02, 15:04:05", date, time.Local)
		if err != nil {
			if ts, err = time.ParseInLocation("02.01.2006, 15:04:05", date, time.Local); err != nil {
				continue
			}
		}
		if name == "" || ts.Before(since) {
			continue
		}
		out = append(out, shared.PatchEvent{Time: ts, Action: "install", Name: name, NewVersion: ver, Source: "softwareupdate"})
	}
	return out
}

// wuaHistoryRow ist eine Zeile der Windows-Update-Historie (IUpdateSearcher.QueryHistory),
// vom PowerShell-Skript als JSON geliefert.
type wuaHistoryRow struct {
	Title      string
	Date       string // ISO 8601 (UTC)
	Operation  int    // 1 = Installation, 2 = Deinstallation
	ResultCode int    // 2 = erfolgreich, 3 = mit Fehlern erfolgreich
}

var kbRef = regexp.MustCompile(`KB\d{5,}`)

// wuaHistoryEvents wandelt erfolgreiche Einträge der Windows-Update-Historie um.
func wuaHistoryEvents(rows []wuaHistoryRow, since time.Time) []shared.PatchEvent {
	var out []shared.PatchEvent
	for _, r := range rows {
		if r.Title == "" || (r.ResultCode != 2 && r.ResultCode != 3) {
			continue
		}
		ts, err := time.Parse(time.RFC3339, r.Date)
		if err != nil {
			continue
		}
		if ts.Before(since) {
			continue
		}
		ev := shared.PatchEvent{Time: ts, Action: "install", Name: r.Title, Source: "wua", Detail: kbRef.FindString(r.Title)}
		if r.Operation == 2 {
			ev.Action = "remove"
		}
		if r.ResultCode == 3 {
			ev.Detail = strings.TrimSpace(ev.Detail + " (mit Fehlern)")
		}
		out = append(out, ev)
	}
	return out
}

// capPatchHistory sortiert neueste zuerst und kappt auf max Einträge.
func capPatchHistory(events []shared.PatchEvent, max int) []shared.PatchEvent {
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.After(events[j].Time) })
	if len(events) > max {
		events = events[:max]
	}
	return events
}
