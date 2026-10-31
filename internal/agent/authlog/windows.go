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

// Collect liest das Sicherheitsprotokoll ab dem gemerkten Zeitpunkt (RecordId dedupliziert).
// Die EventData-Felder kommen per ToXml() namentlich – unabhängig von Positionsindizes.
func Collect(ctx context.Context, stateDir string) []shared.AuthEvent {
	cur := loadCursor(stateDir)
	since := time.Now().Add(-time.Hour)
	if cur.LastTime != "" {
		if t, err := time.Parse(time.RFC3339Nano, cur.LastTime); err == nil {
			since = t.Add(-time.Second)
		}
	}
	script := `$ProgressPreference='SilentlyContinue'; $since=[datetime]::Parse('` + since.UTC().Format(time.RFC3339) + `').ToLocalTime(); ` +
		`$ev = Get-WinEvent -FilterHashtable @{LogName='Security'; Id=4624,4625,4720,4722,4723,4724,4725,4726,4728,4732,4740,4756,4767; StartTime=$since} -MaxEvents 1000 -ErrorAction SilentlyContinue; ` +
		`if (-not $ev) { exit 0 }; ` +
		`$ev | Sort-Object RecordId | ForEach-Object { $x=[xml]$_.ToXml(); $d=@{}; foreach($n in $x.Event.EventData.Data){ if ($n.Name) { $d[$n.Name]=[string]$n.'#text' } }; ` +
		`[pscustomobject]@{Id=$_.Id; Time=$_.TimeCreated.ToUniversalTime().ToString('o'); RecordId=$_.RecordId; Data=$d} } | ConvertTo-Json -Compress -Depth 4`
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
	lastTime, lastRec := cur.LastTime, cur.RecordID
	for _, w := range rows {
		if w.RecordID <= cur.RecordID {
			continue
		}
		if ev, ok := ParseWindowsEvent(w); ok {
			events = append(events, ev)
		}
		if w.RecordID > lastRec {
			lastRec, lastTime = w.RecordID, w.Time
		}
		if len(events) >= maxBatch {
			break
		}
	}
	if lastRec != cur.RecordID {
		cur.RecordID, cur.LastTime = lastRec, strings.TrimSpace(lastTime)
		saveCursor(stateDir, cur)
	}
	return events
}
