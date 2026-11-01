//go:build darwin

package response

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func run(ctx context.Context, name string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %v (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// blockOS nutzt die pf-Tabelle roster_block (muss in /etc/pf.conf referenziert sein:
// table <roster_block> persist / block in quick from <roster_block>).
func blockOS(ctx context.Context, t Target, _ int) (string, error) {
	if _, err := run(ctx, "pfctl", "-t", "roster_block", "-T", "show"); err != nil {
		return "pf", fmt.Errorf("pf-Tabelle roster_block fehlt – in /etc/pf.conf anlegen: table <roster_block> persist; block in quick from <roster_block>")
	}
	_, err := run(ctx, "pfctl", "-t", "roster_block", "-T", "add", t.S)
	return "pf", err
}

func unblockOS(ctx context.Context, t Target) error {
	_, err := run(ctx, "pfctl", "-t", "roster_block", "-T", "delete", t.S)
	return err
}

func lockUserOS(ctx context.Context, user string, lock bool) error {
	flag := "-disableuser"
	if !lock {
		flag = "-enableuser"
	}
	_, err := run(ctx, "pwpolicy", "-u", user, flag)
	return err
}
