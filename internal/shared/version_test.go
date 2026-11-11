package shared

import "testing"

func TestVersionNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.27.0", "0.26.9", true}, {"0.27.0", "0.27.0", false}, {"0.9.0", "0.10.0", false},
		{"1.0.0", "0.99.99", true}, {"dev", "0.1.0", false}, {"0.2.0", "dev", false}, {"", "0.1.0", false},
	} {
		if got := VersionNewer(c.a, c.b); got != c.want {
			t.Errorf("%s>%s = %v", c.a, c.b, got)
		}
	}
	if l := ClientVersionLabel("Viewer", "0.26.0", "0.27.0"); l != "Viewer 0.26.0 – Update 0.27.0 verfügbar (Einstellungen → Downloads)" {
		t.Fatal(l)
	}
	if l := ClientVersionLabel("Viewer", "0.27.0", "0.27.0"); l != "Viewer 0.27.0" {
		t.Fatal(l)
	}
}
