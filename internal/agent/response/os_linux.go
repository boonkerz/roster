//go:build linux

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
		return string(out), fmt.Errorf("%s %s: %v (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func have(bin string) bool { _, err := exec.LookPath(bin); return err == nil }

// nftSets sind die Sets der Tabelle inet roster: Einzeladressen (blocked4/6) und Netze
// (net4/6, flags interval). Alle vier werden in der input- UND forward-Kette verworfen,
// damit auch Verkehr zu Docker-Containern (veröffentlichte Ports laufen über forward) und
// geroutete Pakete gesperrt sind.
var nftSets = []struct{ name, typ, flags, match string }{
	{"blocked4", "ipv4_addr", "timeout", "ip saddr"},
	{"blocked6", "ipv6_addr", "timeout", "ip6 saddr"},
	{"net4", "ipv4_addr", "interval, timeout", "ip saddr"},
	{"net6", "ipv6_addr", "interval, timeout", "ip6 saddr"},
}

// ensureNft legt Tabelle, Sets und die Drop-Regeln in input und forward an (idempotent).
func ensureNft(ctx context.Context) error {
	if _, err := run(ctx, "nft", "add", "table", "inet", "roster"); err != nil {
		return err
	}
	for _, s := range nftSets {
		if _, err := run(ctx, "nft", "add", "set", "inet", "roster", s.name, "{ type "+s.typ+"; flags "+s.flags+"; }"); err != nil && strings.Contains(s.flags, "interval") {
			// Ältere Kernel können interval+timeout nicht kombinieren: dann ohne Timeout
			// (Ablauf übernimmt Expire über die persistierte Sperrliste).
			_, _ = run(ctx, "nft", "add", "set", "inet", "roster", s.name, "{ type "+s.typ+"; flags interval; }")
		}
	}
	for _, chain := range []string{"input", "forward"} {
		if _, err := run(ctx, "nft", "add", "chain", "inet", "roster", chain, "{ type filter hook "+chain+" priority -10; policy accept; }"); err != nil {
			return err
		}
		out, _ := run(ctx, "nft", "list", "chain", "inet", "roster", chain)
		for _, s := range nftSets {
			if strings.Contains(out, "@"+s.name) {
				continue
			}
			args := append([]string{"add", "rule", "inet", "roster", chain}, strings.Fields(s.match)...)
			args = append(args, "@"+s.name, "drop")
			if _, err := run(ctx, "nft", args...); err != nil {
				return err
			}
		}
	}
	return nil
}

func nftSet(t Target) string {
	switch {
	case t.IsNet() && t.V4():
		return "net4"
	case t.IsNet():
		return "net6"
	case t.V4():
		return "blocked4"
	}
	return "blocked6"
}

// iptChains liefert die Ketten, in die die iptables-Regel eingefügt wird: INPUT, FORWARD
// und – falls Docker läuft – DOCKER-USER (dort greifen Regeln vor Dockers eigenen).
func iptChains(ctx context.Context, bin string) []string {
	chains := []string{"INPUT", "FORWARD"}
	if _, err := run(ctx, bin, "-n", "-L", "DOCKER-USER"); err == nil {
		chains = append(chains, "DOCKER-USER")
	}
	return chains
}

// blockOS sperrt über nftables (Timeout im Set) oder iptables (Ablauf per Expire).
func blockOS(ctx context.Context, t Target, minutes int) (string, error) {
	if have("nft") {
		if err := ensureNft(ctx); err != nil {
			return "nftables", err
		}
		elem := "{ " + t.S
		if minutes > 0 {
			elem += fmt.Sprintf(" timeout %dm", minutes)
		}
		elem += " }"
		// Vorhandenes Element erst entfernen (sonst bleibt das alte Timeout).
		_, _ = run(ctx, "nft", "delete", "element", "inet", "roster", nftSet(t), "{ "+t.S+" }")
		if _, err := run(ctx, "nft", "add", "element", "inet", "roster", nftSet(t), elem); err != nil {
			if !t.IsNet() || minutes <= 0 {
				return "nftables", err
			}
			// Netz-Set ohne Timeout-Flag: Element ohne Timeout setzen, Ablauf per Expire.
			if _, err2 := run(ctx, "nft", "add", "element", "inet", "roster", nftSet(t), "{ "+t.S+" }"); err2 != nil {
				return "nftables", err
			}
		}
		return "nftables", nil
	}
	bin := "iptables"
	if !t.V4() {
		bin = "ip6tables"
	}
	if !have(bin) {
		return "iptables", errNoTool
	}
	for _, chain := range iptChains(ctx, bin) {
		// -C prüft, ob die Regel schon existiert.
		if _, err := run(ctx, bin, "-C", chain, "-s", t.S, "-j", "DROP", "-m", "comment", "--comment", "roster-block"); err == nil {
			continue
		}
		if _, err := run(ctx, bin, "-I", chain, "1", "-s", t.S, "-j", "DROP", "-m", "comment", "--comment", "roster-block"); err != nil {
			return "iptables", err
		}
	}
	return "iptables", nil
}

func unblockOS(ctx context.Context, t Target) error {
	var firstErr error
	if have("nft") {
		if _, err := run(ctx, "nft", "delete", "element", "inet", "roster", nftSet(t), "{ "+t.S+" }"); err != nil && !strings.Contains(err.Error(), "No such file") {
			firstErr = err
		} else {
			firstErr = nil
		}
	}
	bin := "iptables"
	if !t.V4() {
		bin = "ip6tables"
	}
	if have(bin) {
		for _, chain := range iptChains(ctx, bin) {
			for {
				if _, err := run(ctx, bin, "-D", chain, "-s", t.S, "-j", "DROP", "-m", "comment", "--comment", "roster-block"); err != nil {
					break
				}
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
