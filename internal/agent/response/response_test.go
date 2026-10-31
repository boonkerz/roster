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
