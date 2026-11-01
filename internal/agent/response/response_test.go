package response

import (
	"net"
	"testing"
)

func TestProtected(t *testing.T) {
	Init(t.TempDir(), "https://192.0.2.10:8443")
	if !isProtected(net.ParseIP("127.0.0.1")) || !isProtected(net.ParseIP("::1")) || !isProtected(net.ParseIP("192.0.2.10")) {
		t.Error("Loopback/Server müssen geschützt sein")
	}
	if isProtected(net.ParseIP("203.0.113.5")) {
		t.Error("fremde IP darf nicht geschützt sein")
	}
	if code, _ := LockUser(nil, "root"); code != 3 {
		t.Errorf("root muss geschützt sein: %d", code)
	}
	if code, _ := LockUser(nil, "bad user; rm"); code != 2 {
		t.Errorf("ungültiger Name: %d", code)
	}
	if code, _ := BlockIP(nil, "127.0.0.1", 10); code != 3 {
		t.Errorf("loopback: %d", code)
	}
	if code, _ := BlockIP(nil, "nicht-ip", 10); code != 2 {
		t.Errorf("ungültig: %d", code)
	}
}

func TestTargets(t *testing.T) {
	Init(t.TempDir(), "https://192.0.2.10:8443")
	tg, err := ParseTarget("203.0.113.0/24")
	if err != nil || tg.S != "203.0.113.0/24" || !tg.IsNet() || !tg.V4() {
		t.Fatalf("%+v %v", tg, err)
	}
	if tg, _ = ParseTarget("203.0.113.77/24"); tg.S != "203.0.113.0/24" {
		t.Errorf("normalisiert: %s", tg.S)
	}
	if tg, _ = ParseTarget("2001:db8::/48"); tg.V4() || !tg.IsNet() {
		t.Errorf("v6: %+v", tg)
	}
	// Netz, das den Server enthält, ist geschützt.
	if tg, _ = ParseTarget("192.0.2.0/24"); !targetProtected(tg) {
		t.Error("Server-Netz muss geschützt sein")
	}
	if tg, _ = ParseTarget("127.0.0.0/8"); !targetProtected(tg) {
		t.Error("Loopback-Netz muss geschützt sein")
	}
	if tg, _ = ParseTarget("203.0.113.0/24"); targetProtected(tg) {
		t.Error("fremdes Netz darf nicht geschützt sein")
	}
	if code, _ := BlockIP(nil, "192.0.2.0/24", 10); code != 3 {
		t.Errorf("server net: %d", code)
	}
}
