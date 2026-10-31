//go:build windows

package collect

import (
	"context"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// patchHistoryOS liest die Windows-Update-Historie (IUpdateSearcher.QueryHistory):
// Titel, Datum (UTC, ISO), Operation (1 install / 2 uninstall) und Ergebniscode.
func patchHistoryOS(ctx context.Context, since time.Time) []shared.PatchEvent {
	const script = `$ProgressPreference='SilentlyContinue'; ` +
		`try { $s=New-Object -ComObject Microsoft.Update.Session; $u=$s.CreateUpdateSearcher(); ` +
		`$n=[Math]::Min($u.GetTotalHistoryCount(), 300); if ($n -le 0) { exit 0 }; ` +
		`$u.QueryHistory(0,$n) | ForEach-Object { [pscustomobject]@{Title=$_.Title; Date=$_.Date.ToUniversalTime().ToString('o'); Operation=[int]$_.Operation; ResultCode=[int]$_.ResultCode} } | ConvertTo-Json -Compress } catch { exit 1 }`
	return wuaHistoryEvents(psJSON[wuaHistoryRow](ctx, script), since)
}
