// Package rootcheck prüft auf Rootkit-Spuren (Linux) und scannt FIM-geänderte Dateien mit
// einem vorhandenen Virenscanner (ClamAV unter Linux, Defender unter Windows). Funde werden
// als AuthEvents „rootkit“ bzw. „malware“ gemeldet; der Server macht kritische Vorfälle daraus.
package rootcheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Finding ist ein Befund: Prüfung und betroffenes Element.
type Finding struct {
	Check string
	Item  string
}

// Run führt alle Rootkit-Prüfungen der Plattform aus.
func Run(ctx context.Context) []Finding { return run(ctx) }

// Events bündelt Befunde je Prüfung zu einem Ereignis (bis zu 10 Elemente genannt, Rest
// gezählt); RecordID je Prüfung, Elementmenge und Tag – ein Vorfall pro Prüfung und Tag,
// auch wenn /tmp hunderte ausführbare Dateien enthält.
func Events(findings []Finding, now time.Time) []shared.AuthEvent {
	byCheck := map[string][]string{}
	var order []string
	for _, f := range findings {
		if _, ok := byCheck[f.Check]; !ok {
			order = append(order, f.Check)
		}
		byCheck[f.Check] = append(byCheck[f.Check], f.Item)
	}
	var out []shared.AuthEvent
	for _, check := range order {
		items := byCheck[check]
		sort.Strings(items)
		h := sha256.Sum256([]byte(check + "|" + strings.Join(items, "|")))
		shown := items
		if len(shown) > 10 {
			shown = shown[:10]
		}
		detail := check + ": " + strings.Join(shown, ", ")
		if len(items) > len(shown) {
			detail += fmt.Sprintf(" (+%d weitere)", len(items)-len(shown))
		}
		out = append(out, shared.AuthEvent{Time: now, Kind: "rootkit", Service: "rootcheck", Detail: detail,
			RecordID: "rk:" + hex.EncodeToString(h[:6]) + ":" + now.Format("2006-01-02")})
	}
	return out
}

// Critical meldet, ob ein Befund-Detail auf eine kritische Prüfung zurückgeht (versteckte
// Prozesse/Module, ld.so.preload, bekannte Rootkit-Pfade); der Rest ist eine Warnung.
func Critical(detail string) bool {
	for _, p := range []string{"versteckter Prozess", "verstecktes Kernelmodul", "ld.so.preload", "bekannter Rootkit-Pfad"} {
		if strings.HasPrefix(detail, p) {
			return true
		}
	}
	return false
}

// MalwareEvents macht aus Scanner-Funden Ereignisse (Detail "pfad: signatur").
func MalwareEvents(hits []Hit, scanner string, now time.Time) []shared.AuthEvent {
	var out []shared.AuthEvent
	for _, h := range hits {
		sum := sha256.Sum256([]byte(h.Path + "|" + h.Signature))
		out = append(out, shared.AuthEvent{Time: now, Kind: "malware", Service: scanner, Detail: h.Path + ": " + h.Signature,
			RecordID: "mw:" + hex.EncodeToString(sum[:6]) + ":" + now.Format("2006-01-02")})
	}
	return out
}

// Hit ist ein Virenscanner-Fund.
type Hit struct {
	Path      string
	Signature string
}
