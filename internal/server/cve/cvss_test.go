package cve

import "testing"

func TestScoreFromVector(t *testing.T) {
	cases := []struct {
		vec  string
		want float64
	}{
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8},
		{"CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:C/C:L/I:L/A:N", 6.4},
		{"CVSS:3.0/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:N/A:N", 5.5},
		{"CVSS:3.1/AV:N/AC:H/PR:N/UI:R/S:U/C:L/I:N/A:N", 3.1},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", 0},
		{"AV:N/AC:L/Au:N/C:P/I:P/A:P", 7.5},
		{"AV:L/AC:H/Au:N/C:N/I:N/A:P", 1.2},
		{"7.2", 7.2},
	}
	for _, c := range cases {
		got, ok := ScoreFromVector(c.vec)
		if !ok || got != c.want {
			t.Errorf("%s: got %.1f (ok=%v), want %.1f", c.vec, got, ok, c.want)
		}
	}
	if _, ok := ScoreFromVector("CVSS:4.0/AV:L/AC:L/AT:P/PR:H/UI:N/VC:N/VI:H/VA:N/SC:H/SI:H/SA:N"); ok {
		t.Error("CVSS v4 sollte nicht gerechnet werden")
	}
	if _, ok := ScoreFromVector("garbage"); ok {
		t.Error("ungültiger Vektor sollte false liefern")
	}
}

func TestLevel(t *testing.T) {
	for s, want := range map[float64]string{9.8: "CRITICAL", 7.0: "HIGH", 5.5: "MEDIUM", 0.1: "LOW", 0: ""} {
		if got := Level(s); got != want {
			t.Errorf("Level(%v)=%s, want %s", s, got, want)
		}
	}
	if LevelFromWord("Important") != "HIGH" || LevelFromWord("negligible") != "LOW" || LevelFromWord("x") != "" {
		t.Error("LevelFromWord")
	}
}

func TestDetailSeverity(t *testing.T) {
	d := &osvDetail{}
	d.Severity = append(d.Severity, struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	}{"CVSS_V4", "CVSS:4.0/AV:L/AC:L/AT:P/PR:H/UI:N/VC:N/VI:H/VA:N/SC:H/SI:H/SA:N"}, struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	}{"Ubuntu", "medium"})
	if got := d.severity(); got != "MEDIUM" {
		t.Errorf("Ubuntu-Priority: got %q", got)
	}
	d2 := &osvDetail{}
	d2.Severity = append(d2.Severity, struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	}{"CVSS_V3", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"})
	if got := d2.severity(); got != "CRITICAL" {
		t.Errorf("CVSS v3: got %q", got)
	}
}
