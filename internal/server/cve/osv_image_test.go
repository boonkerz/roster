package cve

import (
	"encoding/json"
	"testing"
)

func TestImageEcosystem(t *testing.T) {
	cases := [][4]string{
		{"alpine", "3.20.3", "apk", "Alpine:v3.20"},
		{"debian", "12", "dpkg", "Debian:12"},
		{"ubuntu", "24.04", "dpkg", "Ubuntu:24.04"},
		{"rocky", "9.4", "rpm", "Rocky Linux:9"},
		{"", "", "apk", "Alpine"},
		{"", "", "dpkg", "Debian"},
		{"fedora", "40", "rpm", ""},
	}
	for _, c := range cases {
		if got := ImageEcosystem(c[0], c[1], c[2]); got != c[3] {
			t.Errorf("%v: %q", c, got)
		}
	}
}

func TestFixedAndUrgencyPerRelease(t *testing.T) {
	var d osvDetail
	js := `{"affected":[
	 {"package":{"ecosystem":"Debian:12","name":"curl"},"ranges":[{"events":[{"introduced":"0"}]}],"ecosystem_specific":{"urgency":"not yet assigned"}},
	 {"package":{"ecosystem":"Debian:13","name":"curl"},"ranges":[{"events":[{"introduced":"0"}]}],"ecosystem_specific":{"urgency":"unimportant"}},
	 {"package":{"ecosystem":"Debian:14","name":"curl"},"ranges":[{"events":[{"introduced":"0"},{"fixed":"8.21.0~rc2-1"}]}]},
	 {"package":{"ecosystem":"Alpine:v3.20","name":"openssh"},"ranges":[{"events":[{"introduced":"0"},{"fixed":"9.7_p1-r4"}]}]}]}`
	if err := json.Unmarshal([]byte(js), &d); err != nil {
		t.Fatal(err)
	}
	if f := d.fixedFor("curl", "Debian:13"); f != "" {
		t.Fatalf("Debian 13 hat keinen Fix, bekam %q", f)
	}
	if f := d.fixedFor("curl", "Debian:14"); f != "8.21.0~rc2-1" {
		t.Fatal(f)
	}
	if f := d.fixedFor("curl", "Debian"); f != "8.21.0~rc2-1" { // unversioniert: erster Treffer (Host-Scans)
		t.Fatal(f)
	}
	if f := d.fixedFor("openssh", "Alpine:v3.21"); f != "9.7_p1-r4" { // kein Eintrag für das Release: erster Treffer
		t.Fatal(f)
	}
	if u := d.urgencyFor("curl", "Debian:13"); u != "LOW" {
		t.Fatal(u)
	}
	if u := d.urgencyFor("curl", "Debian:12"); u != "" {
		t.Fatal(u)
	}
	if urgencyLevel("low**") != "LOW" || urgencyLevel("high*") != "HIGH" || urgencyLevel("end-of-life") != "" {
		t.Fatal("urgencyLevel")
	}
}
