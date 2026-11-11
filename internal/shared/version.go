package shared

import (
	"strconv"
	"strings"
)

// VersionNewer meldet, ob Version a neuer ist als b ("0.27.0" > "0.26.3"). Unbekannte
// oder Entwicklungsversionen ("", "dev") gelten nie als neuer bzw. älter.
func VersionNewer(a, b string) bool {
	if !isRelease(a) || !isRelease(b) {
		return false
	}
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < 3; i++ {
		na, nb := vpart(pa, i), vpart(pb, i)
		if na != nb {
			return na > nb
		}
	}
	return false
}

func isRelease(v string) bool {
	v = strings.TrimPrefix(v, "v")
	return v != "" && v[0] >= '0' && v[0] <= '9'
}

func vpart(p []string, i int) int {
	if i >= len(p) {
		return 0
	}
	d := p[i]
	if j := strings.IndexFunc(d, func(r rune) bool { return r < '0' || r > '9' }); j >= 0 {
		d = d[:j]
	}
	n, _ := strconv.Atoi(d)
	return n
}

// ClientVersionLabel baut den Versionshinweis für native Clients (Viewer/Tray):
// „Viewer 0.27.0“ bzw. „Viewer 0.26.0 – Update 0.27.0 verfügbar“.
func ClientVersionLabel(name, own, server string) string {
	l := name + " " + own
	if VersionNewer(server, own) {
		l += " – Update " + server + " verfügbar (Einstellungen → Downloads)"
	}
	return l
}
