package threat

import (
	"net"
	"strings"
)

// ParseFeed liest eine Plain-Text-Blockliste: je Zeile eine IP oder ein CIDR, Kommentare
// ab ';' oder '#' (Spamhaus schreibt "1.2.3.0/24 ; SBL123"), Leerzeilen ignoriert.
// Ungültige Zeilen werden übersprungen.
func ParseFeed(body []byte) (ips []string, nets []*net.IPNet) {
	for _, raw := range strings.Split(string(body), "\n") {
		line := raw
		if i := strings.IndexAny(line, ";#"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		tok := f[0]
		if strings.Contains(tok, "/") {
			if _, n, err := net.ParseCIDR(tok); err == nil {
				// /32 und /128 als Einzeladresse führen.
				if ones, bits := n.Mask.Size(); ones == bits {
					ips = append(ips, n.IP.String())
				} else {
					nets = append(nets, n)
				}
			}
			continue
		}
		if ip := net.ParseIP(tok); ip != nil {
			ips = append(ips, ip.String())
		}
	}
	return ips, nets
}
