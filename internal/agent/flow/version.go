package flow

import (
	"strconv"
	"strings"
)

// compareVersions vergleicht zwei Versionen (2.10.0 > 2.9.1, v-Präfix egal, Vorabversion
// "2.5.0-rc1" < "2.5.0"). ok=false, wenn eine Seite keine Version ist (z. B. "latest").
func compareVersions(a, b string) (int, bool) {
	pa, prea, oka := splitVersion(a)
	pb, preb, okb := splitVersion(b)
	if !oka || !okb {
		return 0, false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case prea == preb:
		return 0, true
	case prea == "": // Endversion > Vorabversion
		return 1, true
	case preb == "":
		return -1, true
	case prea < preb:
		return -1, true
	}
	return 1, true
}

func splitVersion(v string) ([]int, string, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if v == "" {
		return nil, "", false
	}
	main, pre, _ := strings.Cut(v, "-")
	main, _, _ = strings.Cut(main, "+")
	var parts []int
	for _, p := range strings.Split(main, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, "", false
		}
		parts = append(parts, n)
	}
	return parts, pre, true
}
