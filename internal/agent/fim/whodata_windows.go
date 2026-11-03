//go:build windows

package fim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

type fimRule shared.FIMRule

func (r fimRule) path() string { return r.Path }

// EnsureAuditRules setzt auf die Datei-/Verzeichnisregeln eine Erfolgs-SACL (Everyone:
// Schreiben/Löschen/Rechte) und schaltet die Überwachung „Dateisystem“ ein, damit Windows
// 4663/4660 schreibt. Registry-Regeln bleiben außen vor. PowerShell-3-tauglich.
func EnsureAuditRules(ctx context.Context, rules []shared.FIMRule) error {
	var paths []string
	for _, r := range rules {
		p := strings.TrimSpace(r.Path)
		if !platformMatches(r.Platforms) || shared.IsRegistryPath(p) || strings.ContainsAny(p, "*?[") {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		paths = append(paths, strings.ReplaceAll(p, "'", "''"))
	}
	if len(paths) == 0 {
		return nil
	}
	script := `$ProgressPreference='SilentlyContinue'; auditpol /set /subcategory:"File System" /success:enable | Out-Null; ` +
		`$rule = New-Object System.Security.AccessControl.FileSystemAuditRule('Everyone','Write,Delete,DeleteSubdirectoriesAndFiles,ChangePermissions,TakeOwnership','ContainerInherit,ObjectInherit','None','Success'); ` +
		`$frule = New-Object System.Security.AccessControl.FileSystemAuditRule('Everyone','Write,Delete,ChangePermissions,TakeOwnership','None','None','Success'); ` +
		`foreach ($p in @('` + strings.Join(paths, "','") + `')) { try { $acl = Get-Acl -Path $p -Audit; ` +
		`$has = $acl.Audit | Where-Object { $_.IdentityReference -eq 'Everyone' -and $_.AuditFlags -match 'Success' -and $_.FileSystemRights -match 'Write' }; ` +
		`if (-not $has) { if ((Get-Item $p).PSIsContainer) { $acl.AddAuditRule($rule) } else { $acl.AddAuditRule($frule) }; Set-Acl -Path $p -AclObject $acl } } catch { Write-Output ("ERR " + $p + ": " + $_.Exception.Message) } }`
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(cctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return errors.New("SACL setzen: " + err.Error())
	}
	if s := strings.TrimSpace(string(out)); strings.HasPrefix(s, "ERR") {
		if i := strings.IndexByte(s, '\n'); i > 0 {
			s = s[:i]
		}
		return errors.New(s)
	}
	return nil
}

// CollectWho liest 4663/4660 seit dem letzten Aufruf (Cursor fim-who-cursor.json) und
// liefert Pfad (kleingeschrieben) → Verursacher.
func CollectWho(ctx context.Context, stateDir string) map[string]Who {
	curPath := filepath.Join(stateDir, "fim-who-cursor.json")
	var cur struct {
		LastTime string `json:"last_time"`
		RecordID int64  `json:"record_id"`
	}
	if b, err := os.ReadFile(curPath); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	since := time.Now().Add(-2 * time.Hour)
	if t, err := time.Parse(time.RFC3339Nano, cur.LastTime); err == nil {
		since = t.Add(-time.Second)
	}
	script := `$ProgressPreference='SilentlyContinue'; $since=[datetime]::Parse('` + since.UTC().Format(time.RFC3339) + `').ToLocalTime(); ` +
		`$ev = Get-WinEvent -FilterHashtable @{LogName='Security'; Id=4663,4660; StartTime=$since} -MaxEvents 5000 -ErrorAction SilentlyContinue; if (-not $ev) { exit 0 }; ` +
		`$rows = $ev | Sort-Object RecordId | ForEach-Object { $x=[xml]$_.ToXml(); $d=@{}; foreach($n in $x.Event.EventData.Data){ if ($n.Name) { $d[$n.Name]=[string]$n.'#text' } }; ` +
		`[pscustomobject]@{Id=$_.Id; Time=$_.TimeCreated.ToUniversalTime().ToString('o'); RecordId=$_.RecordId; ObjectName=$d['ObjectName']; SubjectDomainName=$d['SubjectDomainName']; SubjectUserName=$d['SubjectUserName']; ProcessName=$d['ProcessName']; AccessMask=$d['AccessMask']; HandleId=$d['HandleId']} }; ` +
		`ConvertTo-Json -InputObject @($rows) -Compress -Depth 3`
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(cctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return nil
	}
	var rows []struct {
		WinWhoRow
		RecordID int64 `json:"RecordId"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &rows) != nil {
		return nil
	}
	var plain []WinWhoRow
	for _, r := range rows {
		if r.RecordID <= cur.RecordID {
			continue
		}
		plain = append(plain, r.WinWhoRow)
		if r.RecordID > cur.RecordID {
			cur.RecordID, cur.LastTime = r.RecordID, r.Time
		}
	}
	if b, err := json.Marshal(cur); err == nil {
		_ = os.WriteFile(curPath, b, 0o600)
	}
	return MapWindowsWho(plain)
}

// whoFold: Windows-Pfade ohne Groß-/Kleinschreibung vergleichen.
const whoFold = true
