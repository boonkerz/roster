//go:build windows

package authlog

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// winLog beschreibt ein abzufragendes Ereignisprotokoll (FilterHashtable-Teile).
type winLog struct {
	Name   string
	Filter string // zusätzliche Hashtable-Felder, z. B. "ProviderName='Service Control Manager'; "
	IDs    string
}

// winLogs sind die drei Quellen: Sicherheitsprotokoll (Anmeldungen, Konten, Prozesse,
// Aufgaben, gelöschtes Protokoll), System (7045 Dienst installiert) und PowerShell
// (4104 Skriptblock). Fehlt ein Protokoll (z. B. PowerShell/Operational auf Server 2012
// ohne WMF 5), wird es übersprungen.
var winLogs = []winLog{
	{Name: "Security", IDs: "4624,4625,4720,4722,4723,4724,4725,4726,4728,4732,4740,4756,4767,4688,4698,4702,1102"},
	{Name: "System", Filter: "ProviderName='Service Control Manager'; ", IDs: "7045"},
	{Name: "Microsoft-Windows-PowerShell/Operational", IDs: "4104"},
}

// Collect liest die Protokolle ab dem je Protokoll gemerkten Zeitpunkt (RecordId
// dedupliziert). EventData- und UserData-Felder kommen per ToXml() namentlich.
func Collect(ctx context.Context, stateDir string) []shared.AuthEvent {
	cur := loadCursor(stateDir)
	var parts []string
	for i, l := range winLogs {
		since := time.Now().Add(-time.Hour)
		if lc := cur.logCursor(l.Name); lc.LastTime != "" {
			if t, err := time.Parse(time.RFC3339Nano, lc.LastTime); err == nil {
				since = t.Add(-time.Second)
			}
		}
		parts = append(parts, `@{LogName='`+l.Name+`'; `+l.Filter+`Id=`+l.IDs+`; StartTime=[datetime]::Parse('`+since.UTC().Format(time.RFC3339)+`').ToLocalTime()}`)
		_ = i
	}
	script := `$ProgressPreference='SilentlyContinue'; $qs=@(` + strings.Join(parts, ", ") + `); ` +
		`$all=@(); foreach ($q in $qs) { try { $ev = Get-WinEvent -FilterHashtable $q -MaxEvents 1000 -ErrorAction Stop } catch { continue }; ` +
		`foreach ($e in ($ev | Sort-Object RecordId)) { $x=[xml]$e.ToXml(); $d=@{}; ` +
		`foreach($n in $x.Event.EventData.Data){ if ($n.Name) { $d[$n.Name]=[string]$n.'#text' } }; ` +
		`if ($x.Event.UserData) { foreach($u in $x.Event.UserData.ChildNodes){ foreach($c in $u.ChildNodes){ if ($c.Name) { $d[$c.Name]=[string]$c.'#text' } } } }; ` +
		`$all += [pscustomobject]@{Log=$q.LogName; Id=$e.Id; Time=$e.TimeCreated.ToUniversalTime().ToString('o'); RecordId=$e.RecordId; Data=$d} } }; ` +
		`if ($all.Count -eq 0) { exit 0 }; ConvertTo-Json -InputObject $all -Compress -Depth 4`
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil
	}
	s := bytes.TrimSpace(out)
	if len(s) == 0 {
		return nil
	}
	var rows []WinEvent
	if json.Unmarshal(s, &rows) != nil {
		var one WinEvent
		if json.Unmarshal(s, &one) != nil {
			return nil
		}
		rows = []WinEvent{one}
	}
	var events []shared.AuthEvent
	last := map[string]LogCursor{}
	changed := false
	for _, w := range rows {
		if w.Log == "" {
			w.Log = "Security"
		}
		lc, ok := last[w.Log]
		if !ok {
			lc = cur.logCursor(w.Log)
			last[w.Log] = lc
		}
		if w.RecordID <= lc.RecordID {
			continue
		}
		if ev, ok := ParseWindowsEvent(w); ok {
			events = append(events, ev)
		}
		last[w.Log] = LogCursor{RecordID: w.RecordID, LastTime: strings.TrimSpace(w.Time)}
		changed = true
		if len(events) >= maxBatch {
			break
		}
	}
	if changed {
		for name, lc := range last {
			cur.setLogCursor(name, lc)
		}
		saveCursor(stateDir, cur)
	}
	return events
}
