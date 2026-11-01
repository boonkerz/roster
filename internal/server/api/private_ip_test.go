package api

import "testing"

func TestIsPrivateIP(t *testing.T) {
	for ip, want := range map[string]bool{"172.18.0.2": true, "10.1.2.3": true, "192.168.178.1": true, "100.64.1.1": true, "fd00::1": true, "fe80::1": true, "127.0.0.1": true,
		"8.8.8.8": false, "213.209.159.242": false, "2a01:4f8::1": false, "203.0.113.0/24": false, "10.0.0.0/8": true, "x": false} {
		if got := isPrivateIP(ip); got != want {
			t.Errorf("%s: got %v want %v", ip, got, want)
		}
	}
}
