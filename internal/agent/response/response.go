// Package response führt die Active-Response-Aktionen des Agents aus: Quell-IPs sperren
// (nftables/iptables, Windows-Firewall, pf) und Konten sperren/entsperren. Zeitlich
// begrenzte Sperren laufen über nft-Timeouts bzw. eine persistierte Ablaufliste.
package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Block ist eine aktive Sperre.
type Block struct {
	IP      string    `json:"ip"`
	Until   time.Time `json:"until"` // Zero = dauerhaft
	Method  string    `json:"method"`
	Created time.Time `json:"created"`
}

var (
	mu        sync.Mutex
	stateDir  string
	protected []net.IP // nie sperren: Loopback + Server-Adressen
)

// protectedUsers werden nie gesperrt (Aussperren wäre schlimmer als der Vorfall).
var protectedUsers = map[string]bool{"root": true, "administrator": true, "system": true, "localsystem": true}

// Init setzt das Zustandsverzeichnis und die geschützten Adressen (Server-URL wird aufgelöst).
func Init(dir, serverURL string) {
	mu.Lock()
	defer mu.Unlock()
	stateDir = dir
	protected = nil
	if u, err := url.Parse(serverURL); err == nil && u.Hostname() != "" {
		if ip := net.ParseIP(u.Hostname()); ip != nil {
			protected = append(protected, ip)
		} else if addrs, err := net.LookupIP(u.Hostname()); err == nil {
			protected = append(protected, addrs...)
		}
	}
}

func blocksPath() string { return filepath.Join(stateDir, "ip-blocks.json") }

func loadBlocks() []Block {
	var out []Block
	if b, err := os.ReadFile(blocksPath()); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func saveBlocks(bl []Block) {
	b, _ := json.Marshal(bl)
	tmp := blocksPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, blocksPath())
	}
}

// Target ist eine zu sperrende Adresse: einzelne IP oder Netz (CIDR).
type Target struct {
	S   string     // normalisierte Schreibweise ("203.0.113.5", "203.0.113.0/24")
	IP  net.IP     // bei Einzeladresse
	Net *net.IPNet // bei Netz
}

// V4 meldet, ob das Ziel IPv4 ist.
func (t Target) V4() bool {
	if t.Net != nil {
		return t.Net.IP.To4() != nil
	}
	return t.IP.To4() != nil
}

// IsNet meldet, ob das Ziel ein Netz ist.
func (t Target) IsNet() bool { return t.Net != nil }

// ParseTarget akzeptiert eine IP oder ein CIDR-Netz.
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(s); ip != nil {
		return Target{S: ip.String(), IP: ip}, nil
	}
	if _, n, err := net.ParseCIDR(s); err == nil {
		return Target{S: n.String(), Net: n}, nil
	}
	return Target{}, fmt.Errorf("ungültige IP oder CIDR: %s", s)
}

// isProtected: Loopback, unspezifiziert oder Server-Adresse (bei Netzen: enthält eine davon).
func isProtected(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return true
	}
	for _, p := range protected {
		if p.Equal(ip) {
			return true
		}
	}
	return false
}

func targetProtected(t Target) bool {
	if t.Net == nil {
		return isProtected(t.IP)
	}
	if t.Net.Contains(net.IPv4(127, 0, 0, 1)) || t.Net.Contains(net.IPv6loopback) {
		return true
	}
	for _, p := range protected {
		if t.Net.Contains(p) {
			return true
		}
	}
	return false
}

// BlockIP sperrt eine Quell-IP oder ein Netz (CIDR) für minutes Minuten (0 = dauerhaft).
func BlockIP(ctx context.Context, ipStr string, minutes int) (int, string) {
	t, err := ParseTarget(ipStr)
	if err != nil {
		return 2, err.Error()
	}
	if targetProtected(t) {
		return 3, "Adresse ist geschützt (Loopback/Server) und wird nicht gesperrt: " + t.S
	}
	ip := t
	mu.Lock()
	defer mu.Unlock()
	method, err := blockOS(ctx, t, minutes)
	if err != nil {
		return 1, "Sperre fehlgeschlagen (" + method + "): " + err.Error()
	}
	bl := loadBlocks()
	var until time.Time
	if minutes > 0 {
		until = time.Now().Add(time.Duration(minutes) * time.Minute)
	}
	replaced := false
	for i := range bl {
		if bl[i].IP == ip.S {
			bl[i] = Block{IP: ip.S, Until: until, Method: method, Created: time.Now()}
			replaced = true
		}
	}
	if !replaced {
		bl = append(bl, Block{IP: ip.S, Until: until, Method: method, Created: time.Now()})
	}
	saveBlocks(bl)
	if minutes > 0 {
		return 0, fmt.Sprintf("%s gesperrt über %s bis %s", ip.S, method, until.Format("02.01.2006 15:04"))
	}
	return 0, fmt.Sprintf("%s dauerhaft gesperrt über %s", ip.S, method)
}

// UnblockIP hebt eine Sperre auf.
func UnblockIP(ctx context.Context, ipStr string) (int, string) {
	t, err := ParseTarget(ipStr)
	if err != nil {
		return 2, err.Error()
	}
	mu.Lock()
	defer mu.Unlock()
	err = unblockOS(ctx, t)
	bl := loadBlocks()
	kept := bl[:0]
	for _, b := range bl {
		if b.IP != t.S {
			kept = append(kept, b)
		}
	}
	saveBlocks(kept)
	if err != nil {
		return 1, "Aufheben fehlgeschlagen: " + err.Error()
	}
	return 0, t.S + " entsperrt"
}

// Expire hebt abgelaufene Sperren auf (für Methoden ohne eigenes Timeout).
func Expire(ctx context.Context) {
	mu.Lock()
	defer mu.Unlock()
	bl := loadBlocks()
	now := time.Now()
	changed := false
	kept := bl[:0]
	for _, b := range bl {
		if !b.Until.IsZero() && now.After(b.Until) {
			if t, err := ParseTarget(b.IP); err == nil {
				_ = unblockOS(ctx, t)
			}
			changed = true
			continue
		}
		kept = append(kept, b)
	}
	if changed {
		saveBlocks(kept)
	}
}

// ListJSON liefert die aktiven Sperren als JSON (für list_blocks).
func ListJSON() string {
	mu.Lock()
	defer mu.Unlock()
	bl := loadBlocks()
	sort.Slice(bl, func(i, j int) bool { return bl[i].Created.After(bl[j].Created) })
	b, _ := json.Marshal(map[string]any{"blocks": bl})
	return string(b)
}

// LockUser sperrt ein lokales Konto (nie root/Administrator).
func LockUser(ctx context.Context, user string) (int, string) {
	user = strings.TrimSpace(user)
	if user == "" || strings.ContainsAny(user, " \t\n;&|$`\"'\\/") {
		return 2, "ungültiger Benutzername"
	}
	if protectedUsers[strings.ToLower(user)] {
		return 3, "Konto ist geschützt und wird nicht gesperrt: " + user
	}
	if err := lockUserOS(ctx, user, true); err != nil {
		return 1, "Sperren fehlgeschlagen: " + err.Error()
	}
	return 0, "Konto gesperrt: " + user
}

// UnlockUser entsperrt ein lokales Konto.
func UnlockUser(ctx context.Context, user string) (int, string) {
	user = strings.TrimSpace(user)
	if user == "" || strings.ContainsAny(user, " \t\n;&|$`\"'\\/") {
		return 2, "ungültiger Benutzername"
	}
	if err := lockUserOS(ctx, user, false); err != nil {
		return 1, "Entsperren fehlgeschlagen: " + err.Error()
	}
	return 0, "Konto entsperrt: " + user
}

var errNoTool = errors.New("kein Firewall-Werkzeug verfügbar (nft/iptables)")
