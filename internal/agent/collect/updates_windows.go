//go:build windows

package collect

import (
	"context"
	"strings"

	"github.com/boonkerz/roster/internal/shared"
)

// osUpdateList fragt den Windows-Update-Agenten nach nicht installierten Updates inkl.
// Schweregrad und Support-URL. Der COM-Aufruf kann mehrere Sekunden dauern.
func osUpdateList(ctx context.Context) ([]shared.UpdateItem, bool) {
	const script = `$ProgressPreference='SilentlyContinue'; ` +
		`try { $s=New-Object -ComObject Microsoft.Update.Session; ` +
		`$r=$s.CreateUpdateSearcher().Search('IsInstalled=0 and IsHidden=0'); ` +
		`$r.Updates | ForEach-Object { [pscustomobject]@{Name=$_.Title; Severity=$_.MsrcSeverity; URL=$_.SupportUrl; ` +
		`Cats=@($_.Categories | ForEach-Object { $_.Name }); KBs=@($_.KBArticleIDs)} } | ConvertTo-Json -Compress -Depth 3 } catch { exit 1 }`
	type wu struct {
		Name     string
		Severity string
		URL      string
		Cats     []string
		KBs      []string
	}
	items := psJSON[wu](ctx, script)
	out := make([]shared.UpdateItem, 0, len(items))
	for _, it := range items {
		if it.Name == "" {
			continue
		}
		sev := it.Severity
		if sev == "" {
			sev = "Other"
		}
		security, kind := classifyWindowsUpdate(it.Cats, it.Severity)
		source := strings.Join(it.Cats, ", ")
		if len(it.KBs) > 0 {
			source = strings.TrimSpace(source + " KB" + strings.Join(it.KBs, ", KB"))
		}
		out = append(out, shared.UpdateItem{Name: it.Name, Severity: sev, URL: it.URL, Source: source, Security: security, Kind: kind})
	}
	return out, true
}
