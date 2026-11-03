//go:build windows

package fim

import (
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/boonkerz/roster/internal/shared"
)

// TestRegistryScan legt einen Schlüssel unter HKCU an und prüft Baseline, created, modified
// und deleted über den Registry-Walker (HKCU wird auf HKU\<SID> abgebildet).
func TestRegistryScan(t *testing.T) {
	const sub = `Software\RosterFIMTest`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, sub, registry.ALL_ACCESS)
	if err != nil {
		t.Skip("HKCU nicht beschreibbar:", err)
	}
	t.Cleanup(func() { k.Close(); _ = registry.DeleteKey(registry.CURRENT_USER, sub) })
	_ = k.SetStringValue("Run1", "a.exe")
	rule := shared.FIMRule{ID: "r1", Path: `HKCU\` + sub, Recursive: true, Alert: true}
	now := time.Now()
	bl, ev, _ := Scan(t.Context(), []shared.FIMRule{rule}, &Baseline{}, now)
	if len(ev) != 1 || ev[0].Change != "baseline" {
		t.Fatalf("baseline: %+v", ev)
	}
	var found bool
	for p := range bl.Entries {
		if len(p) > 4 && p[len(p)-4:] == "Run1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Wert fehlt in Baseline: %v", bl.Entries)
	}
	_ = k.SetStringValue("Run2", "b.exe")
	_ = k.SetStringValue("Run1", "c.exe")
	bl2, ev, _ := Scan(t.Context(), []shared.FIMRule{rule}, bl, now.Add(time.Minute))
	changes := map[string]int{}
	for _, e := range ev {
		changes[e.Change]++
	}
	if changes["created"] != 1 || changes["modified"] != 1 {
		t.Fatalf("changes: %v %+v", changes, ev)
	}
	_ = k.DeleteValue("Run2")
	_, ev, _ = Scan(t.Context(), []shared.FIMRule{rule}, bl2, now.Add(2*time.Minute))
	if len(ev) != 1 || ev[0].Change != "deleted" {
		t.Fatalf("deleted: %+v", ev)
	}
}
