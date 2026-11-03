package compliance

import "testing"

// TestCatalogExt: Katalog ist um die 0.19-Regeln erweitert, IDs je Plattform eindeutig,
// jede Regel hat Frameworks und englische Texte.
func TestCatalogExt(t *testing.T) {
	if len(Catalog) < 100 {
		t.Fatalf("Katalog zu klein: %d", len(Catalog))
	}
	seen := map[string]bool{}
	for _, r := range Catalog {
		key := r.ID + "|" + r.Platforms[0]
		if seen[key] {
			t.Errorf("%s doppelt", key)
		}
		seen[key] = true
		if len(r.Frameworks) == 0 {
			t.Errorf("%s: keine Frameworks", r.ID)
		}
		if r.DescriptionEN == "" || r.RemediationEN == "" || r.TitleEN == "" {
			t.Errorf("%s: englische Texte fehlen", r.ID)
		}
		if r.Severity != "low" && r.Severity != "medium" && r.Severity != "high" {
			t.Errorf("%s: Schweregrad %q", r.ID, r.Severity)
		}
	}
	if r, ok := ByID("pam_pwquality"); !ok || r.Frameworks[0] != "CIS 5.3.1" {
		t.Errorf("pam_pwquality: %+v", r)
	}
}
