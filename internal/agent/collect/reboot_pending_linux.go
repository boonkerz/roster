//go:build linux

package collect

import "os"

// RebootPending meldet, ob ein Neustart aussteht (Debian/Ubuntu:
// /var/run/reboot-required). known=false, wenn nicht ermittelbar.
func RebootPending() (pending, known bool) {
	for _, p := range []string{"/var/run/reboot-required", "/run/reboot-required"} {
		if _, err := os.Stat(p); err == nil {
			return true, true
		}
	}
	return false, true
}
