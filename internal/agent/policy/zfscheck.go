package policy

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// --- ZFS-Pool (zfs) ---
// Prüft ZFS-Pools per `zpool list`. Config:
//
//	pool  optional: nur diesen Pool prüfen (leer = alle)
//	mode  "health" (Standard) oder "capacity"
//	max   für capacity: Belegung in % (Standard 80)
func zfsCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	if _, err := exec.LookPath("zpool"); err != nil {
		return unknownCheck(c, "zpool nicht gefunden (kein ZFS auf diesem Gerät)")
	}
	cctx, cancel := context.WithTimeout(ctx, timeoutDur(c, 10*time.Second))
	defer cancel()
	out, err := exec.CommandContext(cctx, "zpool", "list", "-H", "-o", "name,health,capacity").CombinedOutput()
	if err != nil {
		return unknownCheck(c, "zpool list fehlgeschlagen: "+strings.TrimSpace(string(out)))
	}
	return evalZFS(c, string(out))
}

type zfsPool struct {
	name   string
	health string
	cap    int
}

// parseZpoolList liest tab-getrennte Zeilen "name<TAB>HEALTH<TAB>NN%".
func parseZpoolList(out string) []zfsPool {
	var pools []zfsPool
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			f = strings.Fields(line) // Fallback, falls nicht -H
		}
		if len(f) < 3 {
			continue
		}
		capv, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(f[2]), "%"))
		pools = append(pools, zfsPool{
			name:   strings.TrimSpace(f[0]),
			health: strings.ToUpper(strings.TrimSpace(f[1])),
			cap:    capv,
		})
	}
	return pools
}

func evalZFS(c shared.CheckSpec, out string) shared.CheckResult {
	pools := parseZpoolList(out)
	if want := strConfig(c, "pool"); want != "" {
		var f []zfsPool
		for _, p := range pools {
			if strings.EqualFold(p.name, want) {
				f = append(f, p)
			}
		}
		if len(f) == 0 {
			return failCheck(c, 0, fmt.Sprintf("ZFS-Pool %q nicht gefunden", want))
		}
		pools = f
	}
	if len(pools) == 0 {
		return unknownCheck(c, "keine ZFS-Pools gefunden")
	}

	mode := strConfig(c, "mode")
	if mode == "" {
		mode = "health"
	}
	switch mode {
	case "capacity":
		max := 80.0
		if v, ok := numConfig(c, "max"); ok && v > 0 {
			max = v
		}
		worst := 0
		var bad []string
		for _, p := range pools {
			if p.cap > worst {
				worst = p.cap
			}
			if float64(p.cap) > max {
				bad = append(bad, fmt.Sprintf("%s %d%%", p.name, p.cap))
			}
		}
		if len(bad) > 0 {
			return failCheck(c, float64(worst), fmt.Sprintf("ZFS-Belegung über %.0f%%: %s", max, strings.Join(bad, ", ")))
		}
		return passCheck(c, float64(worst), fmt.Sprintf("ZFS-Belegung ok (höchste %d%%, Grenze %.0f%%)", worst, max))
	default: // health
		var bad []string
		names := make([]string, 0, len(pools))
		for _, p := range pools {
			names = append(names, p.name)
			if p.health != "ONLINE" {
				bad = append(bad, fmt.Sprintf("%s=%s", p.name, p.health))
			}
		}
		if len(bad) > 0 {
			return failCheck(c, 0, "ZFS-Pool nicht ONLINE: "+strings.Join(bad, ", "))
		}
		return passCheck(c, 1, "ZFS-Pools ONLINE: "+strings.Join(names, ", "))
	}
}
