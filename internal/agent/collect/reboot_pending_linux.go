//go:build linux

package collect

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/host"
)

// RebootPending meldet, ob ein Neustart aussteht: Debian/Ubuntu über
// /var/run/reboot-required, RHEL-Familie über `needs-restarting -r`, sonst über den
// Vergleich laufender vs. neuester installierter Kernel. known=false, wenn nicht ermittelbar.
func RebootPending() (pending, known bool) {
	for _, p := range []string{"/var/run/reboot-required", "/run/reboot-required"} {
		if _, err := os.Stat(p); err == nil {
			return true, true
		}
	}
	if _, err := exec.LookPath("needs-restarting"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Exit 1 = Neustart nötig, 0 = nicht nötig; andere Fehler ignorieren.
		if err := exec.CommandContext(ctx, "needs-restarting", "-r").Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
				return true, true
			}
		} else {
			return false, true
		}
	}
	if running, err := host.KernelVersion(); err == nil {
		if kernelUpgradePending(strings.TrimSpace(running), kernelInstalled()) {
			return true, true
		}
	}
	return false, true
}
