package threat

import (
	"net"
	"testing"
)

func TestParseFeed(t *testing.T) {
	body := []byte(`; Spamhaus DROP List 2026/11/03
; Last-Modified: ...
1.10.16.0/20 ; SBL256894
2.56.192.0/22 ; SBL459831
# Feodo-style comment
# DstIP
203.0.113.9
203.0.113.10/32

garbage line here
2001:db8::/32 ; v6
::1
`)
	ips, nets := ParseFeed(body)
	if len(ips) != 3 || ips[0] != "203.0.113.9" || ips[1] != "203.0.113.10" || ips[2] != "::1" {
		t.Fatalf("ips: %v", ips)
	}
	if len(nets) != 3 || nets[0].String() != "1.10.16.0/20" || nets[2].String() != "2001:db8::/32" {
		t.Fatalf("nets: %v", nets)
	}
}

func TestIndexLookup(t *testing.T) {
	ix := NewIndex()
	_, n1, _ := net.ParseCIDR("1.10.16.0/20")
	ix.Add("Spamhaus DROP", nil, []*net.IPNet{n1})
	ix.Add("blocklist.de", []string{"203.0.113.9", "1.10.20.4"}, nil)
	for ip, want := range map[string][]string{
		"1.10.20.4":   {"Spamhaus DROP", "blocklist.de"}, // Netz und Einzeladresse
		"1.10.31.255": {"Spamhaus DROP"},
		"203.0.113.9": {"blocklist.de"},
		"203.0.113.8": nil,
		"not-an-ip":   nil,
	} {
		got := ix.Lookup(ip)
		if len(got) != len(want) {
			t.Fatalf("%s: %v, want %v", ip, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: %v, want %v", ip, got, want)
			}
		}
	}
	if Active() || Lookup("1.10.20.4") != nil {
		t.Fatal("ohne Install darf nichts gelistet sein")
	}
	Install(ix)
	t.Cleanup(func() { Install(nil) })
	if !Active() || len(Lookup("1.10.20.4")) != 2 || Lookup("") != nil {
		t.Fatal("Install/Lookup")
	}
}
