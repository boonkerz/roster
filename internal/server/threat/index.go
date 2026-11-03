package threat

import (
	"net"
	"sort"
	"sync/atomic"
)

type netEntry struct {
	n     *net.IPNet
	feeds uint16
}

// Index ist der Speicher-Index aller geladenen Feeds: Einzeladressen in einer Map,
// Netze als Liste (einige tausend, linear geprüft), je Eintrag eine Bitmaske der Feeds.
type Index struct {
	names []string
	ips   map[string]uint16
	nets  []netEntry
}

// NewIndex baut einen leeren Index.
func NewIndex() *Index { return &Index{ips: map[string]uint16{}} }

// Add fügt die Einträge eines Feeds hinzu (Name wird für Lookup-Ergebnisse verwendet).
func (ix *Index) Add(name string, ips []string, nets []*net.IPNet) {
	if len(ix.names) >= 16 {
		return
	}
	bit := uint16(1) << uint(len(ix.names))
	ix.names = append(ix.names, name)
	for _, ip := range ips {
		ix.ips[ip] |= bit
	}
	for _, n := range nets {
		ix.nets = append(ix.nets, netEntry{n: n, feeds: bit})
	}
}

// Entries liefert die Zahl der Einzeladressen und Netze.
func (ix *Index) Entries() (ips, nets int) { return len(ix.ips), len(ix.nets) }

// Lookup liefert die Namen der Feeds, in denen ip (oder ein umfassendes Netz) steht.
func (ix *Index) Lookup(ip string) []string {
	if ix == nil {
		return nil
	}
	p := net.ParseIP(ip)
	if p == nil {
		return nil
	}
	mask := ix.ips[p.String()]
	for _, e := range ix.nets {
		if e.n.Contains(p) {
			mask |= e.feeds
		}
	}
	if mask == 0 {
		return nil
	}
	var out []string
	for i, name := range ix.names {
		if mask&(1<<uint(i)) != 0 {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

var current atomic.Pointer[Index]

// Install macht ix zum aktiven Index (nil = Abgleich aus).
func Install(ix *Index) { current.Store(ix) }

// Lookup prüft ip gegen den aktiven Index; nil, wenn nicht gelistet oder Abgleich aus.
func Lookup(ip string) []string {
	if ip == "" {
		return nil
	}
	return current.Load().Lookup(ip)
}

// Active meldet, ob ein Index installiert ist.
func Active() bool { return current.Load() != nil }
