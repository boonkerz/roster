package geoip

import "testing"

func TestCountry(t *testing.T) {
	cases := map[string]string{
		"8.8.8.8":               "US",
		"1.1.1.1":               "AU",
		"213.209.159.10":        "", // wird unten nur auf „nicht leer“ geprüft
		"2001:4860:4860::8888":  "US",
		"192.168.1.1":           "",
		"127.0.0.1":             "",
		"10.0.0.0/8":            "",
		"nicht-ip":              "",
		"":                      "",
		"2a01:4f8:c010:1234::1": "DE",
	}
	for ip, want := range cases {
		got := Country(ip)
		if want == "" && ip == "213.209.159.10" {
			if got == "" {
				t.Errorf("%s: Land erwartet", ip)
			}
			continue
		}
		if got != want {
			t.Errorf("%s: got %q want %q", ip, got, want)
		}
	}
	if Version() == "" {
		t.Error("Version fehlt")
	}
}

func BenchmarkCountry(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Country("213.209.159.10")
	}
}
