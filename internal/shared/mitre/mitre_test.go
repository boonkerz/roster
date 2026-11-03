package mitre

import (
	"regexp"
	"testing"
)

func TestCatalog(t *testing.T) {
	seen := map[string]bool{}
	reID := regexp.MustCompile(`^T\d{4}(\.\d{3})?$`)
	for _, k := range Kinds {
		if seen[k.Kind] {
			t.Errorf("%s doppelt", k.Kind)
		}
		seen[k.Kind] = true
		if !ValidSeverity(k.DefaultSeverity) {
			t.Errorf("%s: ungültiger Schweregrad %q", k.Kind, k.DefaultSeverity)
		}
		if len(k.Techniques) == 0 {
			t.Errorf("%s: keine Technik", k.Kind)
		}
		for _, tq := range k.Techniques {
			if !reID.MatchString(tq.ID) || tq.Name == "" || tq.Tactic == "" {
				t.Errorf("%s: Technik %+v unvollständig", k.Kind, tq)
			}
		}
	}
	for _, want := range []string{"brute_force", "fim_change", "scan"} {
		if !IsKind(want) {
			t.Errorf("%s fehlt", want)
		}
	}
	if len(AllKinds()) != len(Kinds) {
		t.Fatal("AllKinds")
	}
}

func TestKindsForTechnique(t *testing.T) {
	ks := KindsForTechnique("T1110")
	if len(ks) < 2 || ks[0] != "brute_force" {
		t.Fatalf("T1110: %v", ks)
	}
	// Obertechnik trifft Untertechniken, aber nicht Präfix-Kollisionen (T105 ≠ T1053).
	if ks := KindsForTechnique("T1059"); len(ks) != 1 || ks[0] != "suspicious_process" {
		t.Fatalf("T1059: %v", ks)
	}
	if ks := KindsForTechnique("T105"); len(ks) != 0 {
		t.Fatalf("T105 darf nichts treffen: %v", ks)
	}
	if ByKind("nope") != nil || DefaultSeverity("nope") != "" {
		t.Fatal("unbekannte Art")
	}
}
