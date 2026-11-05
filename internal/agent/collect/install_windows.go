//go:build windows

package collect

import (
	"context"
	"encoding/json"
	"strings"
)

// InstallUpdates lädt und installiert ausstehende Updates über den Windows-Update-Agenten.
// Ist names leer, werden alle installiert, sonst nur Updates mit passendem Titel.
// Kein automatischer Neustart – "Reboot erforderlich" wird nur gemeldet.
func InstallUpdates(ctx context.Context, names []string, _ bool, _ bool) (int, string) {
	// Genehmigte Titel als JSON-Array ins Skript geben (leer = alle).
	approved, _ := json.Marshal(names)
	script := `$ProgressPreference='SilentlyContinue'
$approved = '` + strings.ReplaceAll(string(approved), "'", "''") + `' | ConvertFrom-Json
try {
  $s = New-Object -ComObject Microsoft.Update.Session
  $r = $s.CreateUpdateSearcher().Search('IsInstalled=0 and IsHidden=0')
  if ($r.Updates.Count -eq 0) { Write-Output 'Keine ausstehenden Updates'; exit 0 }
  $coll = New-Object -ComObject Microsoft.Update.UpdateColl
  foreach ($u in $r.Updates) {
    if ($approved -and ($approved.Count -gt 0) -and ($approved -notcontains $u.Title)) { continue }
    if (-not $u.EulaAccepted) { try { $u.AcceptEula() } catch {} }; [void]$coll.Add($u)
  }
  if ($coll.Count -eq 0) { Write-Output 'Keine genehmigten Updates'; exit 0 }
  $dl = $s.CreateUpdateDownloader(); $dl.Updates = $coll; [void]$dl.Download()
  $inst = $s.CreateUpdateInstaller(); $inst.Updates = $coll
  $res = $inst.Install()
  Write-Output ("Installiert: " + $coll.Count + ", Ergebnis-Code: " + $res.ResultCode + ", Reboot erforderlich: " + $res.RebootRequired)
  if ($res.ResultCode -ne 2) { exit 1 }
} catch { Write-Output $_.Exception.Message; exit 1 }`

	out, err := psOutput(ctx, script)
	exit := 0
	if err != nil { // PowerShell endete mit exit 1 (Fehler im Skript)
		exit = 1
	}
	return exit, capOutput(string(out))
}
