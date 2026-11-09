package yamledit

import (
	"strings"
	"testing"
)

func TestApplyAllPreview(t *testing.T) {
	in := "services:\n  web:\n    image: reg/web:2.4.0\n  cron:\n    image: reg/cron:2.4.0\n"
	ops, err := ParseOps("set services.{web,cron}.image = {{value.repo}}:2.5.0\n# Kommentar\nensure volumes.media = {}")
	if err != nil {
		t.Fatal(err)
	}
	out, res, err := ApplyAll(in, ops)
	if err != nil || len(res) != 3 || !res[0].Changed || !strings.Contains(out, "reg/cron:2.5.0") || !strings.Contains(out, "volumes:\n  media: {}") {
		t.Fatalf("%v %+v\n%s", err, res, out)
	}
	_, res, err = ApplyAll("a:\n  v: [x]\n", []Op{{Op: "append", Path: "a.v", Value: "y", Line: 1}})
	if err == nil || len(res) != 1 || res[0].Error == "" {
		t.Fatalf("fehler erwartet: %v %+v", err, res)
	}
	if _, err := ParseOps("frobnicate a = b"); err == nil {
		t.Fatal("unbekannte op akzeptiert")
	}
}
