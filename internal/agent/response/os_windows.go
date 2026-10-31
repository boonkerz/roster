//go:build windows

package response

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

func ps(ctx context.Context, script string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%v (%s)", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// blockOS legt eine eingehende Blockregel der Windows-Firewall an (Ablauf per Expire).
func blockOS(ctx context.Context, ip net.IP, _ int) (string, error) {
	name := "Roster-Block " + ip.String()
	_, err := ps(ctx, `if (-not (Get-NetFirewallRule -DisplayName '`+name+`' -ErrorAction SilentlyContinue)) { New-NetFirewallRule -DisplayName '`+name+`' -Direction Inbound -RemoteAddress '`+ip.String()+`' -Action Block -Profile Any -Enabled True | Out-Null }`)
	return "windows-firewall", err
}

func unblockOS(ctx context.Context, ip net.IP) error {
	_, err := ps(ctx, `Remove-NetFirewallRule -DisplayName 'Roster-Block `+ip.String()+`' -ErrorAction SilentlyContinue`)
	return err
}

func lockUserOS(ctx context.Context, user string, lock bool) error {
	cmd := "Disable-LocalUser"
	if !lock {
		cmd = "Enable-LocalUser"
	}
	_, err := ps(ctx, cmd+` -Name '`+user+`'`)
	return err
}
