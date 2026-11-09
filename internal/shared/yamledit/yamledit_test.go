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

func TestFlowValueBecomesBlock(t *testing.T) {
	in := "services:\n  php-fpm:\n    image: x\n\nvolumes:\n  dbdata: {}\n"
	want := "volumes:\n  dbdata: {}\n  shop_media:\n    driver: local\n    driver_opts:\n      type: none\n      o: bind\n      device: /data/volumes/shop_media\n"
	for _, opsText := range []string{
		"ensure volumes.shop_media = {driver: local, driver_opts: {type: none, o: bind, device: /data/volumes/shop_media}}",
		"ensure volumes.shop_media.driver = local\nensure volumes.shop_media.driver_opts.type = none\nensure volumes.shop_media.driver_opts.o = bind\nensure volumes.shop_media.driver_opts.device = /data/volumes/shop_media",
	} {
		ops, _ := ParseOps(opsText)
		out, _, err := ApplyAll(in, ops)
		if err != nil || !strings.HasSuffix(out, want) {
			t.Fatalf("%v\n%s", err, out)
		}
		// zweiter Durchgang: nichts zu tun
		again, _, _ := ApplyAll(out, ops)
		if again != out {
			t.Fatalf("nicht idempotent:\n%s", again)
		}
	}
	// Ganz neues volumes: in einer Datei ohne Volumes, Liste als Block, {} bleibt {}.
	out, _, err := ApplyAll("services:\n  web:\n    image: x\n", []Op{
		{Op: "ensure", Path: "volumes.media", Value: "{driver: local}", Line: 1},
		{Op: "set", Path: "services.web.dns", Value: "[1.1.1.1, 8.8.8.8]", Line: 2},
		{Op: "ensure", Path: "volumes.cache", Value: "{}", Line: 3},
	})
	if err != nil || !strings.Contains(out, "volumes:\n  media:\n    driver: local\n  cache: {}") || !strings.Contains(out, "    dns:\n      - 1.1.1.1\n      - 8.8.8.8") {
		t.Fatalf("%v\n%s", err, out)
	}
}
