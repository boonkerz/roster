package cve

import "testing"

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
