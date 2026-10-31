package fim

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func kinds(ev []shared.FIMEvent) map[string]string {
	m := map[string]string{}
	for _, e := range ev {
		m[filepath.Base(e.Path)] = e.Change
	}
	return m
}

func TestScanBaselineAndDiff(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "etc", "passwd"), "root:x:0:0\n")
	write(t, filepath.Join(dir, "etc", "hosts"), "127.0.0.1 localhost\n")
	write(t, filepath.Join(dir, "etc", "cache", "tmp.db"), "x")
	write(t, filepath.Join(dir, "etc", "nested", "deep.conf"), "a=1\n")
	rule := shared.FIMRule{ID: "r1", Path: filepath.Join(dir, "etc"), Recursive: true, Exclude: []string{"cache/", "*.db"}, Alert: true}
	ctx := context.Background()
	now := time.Now()

	bl, ev, st := Scan(ctx, []shared.FIMRule{rule}, &Baseline{}, now)
	if len(ev) != 1 || ev[0].Change != "baseline" || ev[0].Size != 3 || st.Files != 3 {
		t.Fatalf("baseline: %+v %+v", ev, st)
	}
	if _, ok := bl.Entries[filepath.Join(dir, "etc", "cache", "tmp.db")]; ok {
		t.Fatal("exclude hat nicht gegriffen")
	}
	// Keine Änderung → keine Ereignisse, Hashes aus Metadaten übernommen.
	bl2, ev, st := Scan(ctx, []shared.FIMRule{rule}, bl, now.Add(time.Minute))
	if len(ev) != 0 || st.Hashed != 0 {
		t.Fatalf("unverändert: %+v %+v", ev, st)
	}
	// Änderungen: passwd geändert, hosts gelöscht, new.conf neu, Rechte von deep.conf.
	time.Sleep(10 * time.Millisecond)
	write(t, filepath.Join(dir, "etc", "passwd"), "root:x:0:0\nevil:x:0:0\n")
	_ = os.Remove(filepath.Join(dir, "etc", "hosts"))
	write(t, filepath.Join(dir, "etc", "new.conf"), "new")
	_ = os.Chmod(filepath.Join(dir, "etc", "nested", "deep.conf"), 0o600)
	bl3, ev, _ := Scan(ctx, []shared.FIMRule{rule}, bl2, now.Add(2*time.Minute))
	k := kinds(ev)
	if k["passwd"] != "modified" || k["hosts"] != "deleted" || k["new.conf"] != "created" || k["deep.conf"] != "attrs" || len(ev) != 4 {
		t.Fatalf("diff: %v (%d)", k, len(ev))
	}
	for _, e := range ev {
		if !e.Alert || e.RuleID != "r1" {
			t.Errorf("alert/rule fehlt: %+v", e)
		}
		if e.Change == "modified" && (e.OldHash == "" || e.NewHash == "" || e.OldHash == e.NewHash) {
			t.Errorf("hashes: %+v", e)
		}
	}
	// Regel entfernt → Baseline-Einträge still verworfen, keine „deleted“-Flut.
	_, ev, _ = Scan(ctx, nil, bl3, now.Add(3*time.Minute))
	if len(ev) != 0 {
		t.Fatalf("regel entfernt: %+v", ev)
	}
	// Nicht-rekursive Regel sieht nur die direkte Ebene.
	flat := shared.FIMRule{ID: "r2", Path: filepath.Join(dir, "etc"), Recursive: false}
	blf, _, stf := Scan(ctx, []shared.FIMRule{flat}, &Baseline{}, now)
	if _, ok := blf.Entries[filepath.Join(dir, "etc", "nested", "deep.conf")]; ok || stf.Files != 2 {
		t.Fatalf("flach: %+v", stf)
	}
	// Glob-Pfad auf eine Datei.
	g := shared.FIMRule{ID: "r3", Path: filepath.Join(dir, "etc", "*.conf")}
	blg, _, _ := Scan(ctx, []shared.FIMRule{g}, &Baseline{}, now)
	if len(blg.Entries) != 1 {
		t.Fatalf("glob: %d", len(blg.Entries))
	}
	// Persistenz
	p := filepath.Join(dir, "fim-baseline.json")
	if err := Save(p, bl3); err != nil {
		t.Fatal(err)
	}
	if got := Load(p); len(got.Entries) != len(bl3.Entries) || got.Created.IsZero() {
		t.Fatalf("load: %d", len(got.Entries))
	}
	if Load(filepath.Join(dir, "missing")).Entries != nil {
		t.Fatal("fehlende Datei muss nil-Map liefern")
	}
}

func TestExcluded(t *testing.T) {
	r := shared.FIMRule{Exclude: []string{"*.log", "/var/cache/", "tmp"}}
	if !excluded(r, "/var/log/x.log") || !excluded(r, "/var/cache/a/b") || !excluded(r, "/etc/tmp/x") || excluded(r, "/etc/passwd") {
		t.Error("excluded")
	}
}
