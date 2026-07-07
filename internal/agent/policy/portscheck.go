package policy

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/thomaspeterson/pc-inventory/internal/agent/collect"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// portsCheck vergleicht die öffentlich lauschenden Ports des Geräts gegen eine
// Whitelist (config["allowed"], z. B. "22,80,443"). Failing, sobald ein öffentlich
// erreichbarer Port nicht in der Liste steht. Nur an Loopback gebundene Ports zählen
// nicht (das ist keine „nach außen offene" Fläche).
func portsCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	allowed := parsePortSet(strConfig(c, "allowed"))
	return evalPorts(c.ID, allowed, collect.ListenPorts(ctx))
}

// evalPorts ist die reine Vergleichslogik (ohne Socket-Enumeration), damit testbar.
func evalPorts(checkID string, allowed map[int]bool, ports []shared.ListenPort) shared.CheckResult {
	unexpected := map[int]string{} // Port -> Protokoll(e)
	openPublic := map[int]bool{}
	for _, p := range ports {
		if !p.Public {
			continue
		}
		openPublic[p.Port] = true
		if !allowed[p.Port] {
			if unexpected[p.Port] == "" {
				unexpected[p.Port] = p.Proto
			} else if !strings.Contains(unexpected[p.Port], p.Proto) {
				unexpected[p.Port] += "/" + p.Proto
			}
		}
	}

	if len(unexpected) == 0 {
		return shared.CheckResult{
			CheckID: checkID, Status: "passing",
			Output: fmt.Sprintf("%d öffentliche Ports, alle erlaubt", len(openPublic)),
		}
	}
	// Ausgabe: sortierte Liste der unerwarteten Ports.
	nums := make([]int, 0, len(unexpected))
	for port := range unexpected {
		nums = append(nums, port)
	}
	sort.Ints(nums)
	parts := make([]string, 0, len(nums))
	for _, port := range nums {
		parts = append(parts, fmt.Sprintf("%s/%d", unexpected[port], port))
	}
	return shared.CheckResult{
		CheckID: checkID, Status: "failing",
		Output: "Unerwartet öffentlich erreichbar: " + strings.Join(parts, ", "),
	}
}

// parsePortSet liest eine Komma-/Leerzeichen-getrennte Portliste ("22,80, 443").
func parsePortSet(s string) map[int]bool {
	set := map[int]bool{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n > 0 {
			set[n] = true
		}
	}
	return set
}
