//go:build linux

package response

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

func run(ctx context.Context, name string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %v (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func have(bin string) bool { _, err := exec.LookPath(bin); return err == nil }

// ensureNft legt Tabelle, Sets (mit Timeout-Flag) und die Drop-Regeln an (idempotent).
func ensureNft(ctx context.Context) error {
	if _, err := run(ctx, "nft", "add", "table", "inet", "roster"); err != nil {
		return err
	}
	_, _ = run(ctx, "nft", "add", "set", "inet", "roster", "blocked4", "{ type ipv4_addr; flags timeout; }")
	_, _ = run(ctx, "nft", "add", "set", "inet", "roster", "blocked6", "{ type ipv6_addr; flags timeout; }")
	if _, err := run(ctx, "nft", "add", "chain", "inet", "roster", "input", "{ type filter hook input priority -10; policy accept; }"); err != nil {
		return err
	}
	out, _ := run(ctx, "nft", "list", "chain", "inet", "roster", "input")
	if !strings.Contains(out, "@blocked4") {
		if _, err := run(ctx, "nft", "add", "rule", "inet", "roster", "input", "ip", "saddr", "@blocked4", "drop"); err != nil {
			return err
		}
	}
	if !strings.Contains(out, "@blocked6") {
		if _, err := run(ctx, "nft", "add", "rule", "inet", "roster", "input", "ip6", "saddr", "@blocked6", "drop"); err != nil {
			return err
		}
	}
	return nil
}

func nftSet(ip net.IP) string {
	if ip.To4() != nil {
		return "blocked4"
	}
	return "blocked6"
}

// blockOS sperrt über nftables (Timeout im Set) oder iptables (Ablauf per Expire).
func blockOS(ctx context.Context, ip net.IP, minutes int) (string, error) {
	if have("nft") {
		if err := ensureNft(ctx); err != nil {
			return "nftables", err
		}
		elem := "{ " + ip.String()
		if minutes > 0 {
			elem += fmt.Sprintf(" timeout %dm", minutes)
		}
		elem += " }"
		// Vorhandenes Element erst entfernen (sonst bleibt das alte Timeout).
		_, _ = run(ctx, "nft", "delete", "element", "inet", "roster", nftSet(ip), "{ "+ip.String()+" }")
		if _, err := run(ctx, "nft", "add", "element", "inet", "roster", nftSet(ip), elem); err != nil {
			return "nftables", err
		}
		return "nftables", nil
	}
	bin := "iptables"
	if ip.To4() == nil {
		bin = "ip6tables"
	}
	if !have(bin) {
		return "iptables", errNoTool
	}
	// -C prüft, ob die Regel schon existiert.
	if _, err := run(ctx, bin, "-C", "INPUT", "-s", ip.String(), "-j", "DROP", "-m", "comment", "--comment", "roster-block"); err == nil {
		return "iptables", nil
	}
	if _, err := run(ctx, bin, "-I", "INPUT", "1", "-s", ip.String(), "-j", "DROP", "-m", "comment", "--comment", "roster-block"); err != nil {
		return "iptables", err
	}
	return "iptables", nil
}

func unblockOS(ctx context.Context, ip net.IP) error {
	var firstErr error
	if have("nft") {
		if _, err := run(ctx, "nft", "delete", "element", "inet", "roster", nftSet(ip), "{ "+ip.String()+" }"); err != nil && !strings.Contains(err.Error(), "No such file") {
			firstErr = err
		} else {
			firstErr = nil
		}
	}
	bin := "iptables"
	if ip.To4() == nil {
		bin = "ip6tables"
	}
	if have(bin) {
		for {
			if _, err := run(ctx, bin, "-D", "INPUT", "-s", ip.String(), "-j", "DROP", "-m", "comment", "--comment", "roster-block"); err != nil {
				break
			}
		}
	}
	return firstErr
}

func lockUserOS(ctx context.Context, user string, lock bool) error {
	flag := "-L"
	if !lock {
		flag = "-U"
	}
	if have("usermod") {
		_, err := run(ctx, "usermod", flag, user)
		return err
	}
	if have("passwd") {
		pf := "-l"
		if !lock {
			pf = "-u"
		}
		_, err := run(ctx, "passwd", pf, user)
		return err
	}
	return fmt.Errorf("weder usermod noch passwd verfügbar")
}
