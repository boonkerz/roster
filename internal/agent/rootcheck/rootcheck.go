// Package rootcheck prüft auf Rootkit-Spuren (Linux) und scannt FIM-geänderte Dateien mit
// einem vorhandenen Virenscanner (ClamAV unter Linux, Defender unter Windows). Funde werden
// als AuthEvents „rootkit“ bzw. „malware“ gemeldet; der Server macht kritische Vorfälle daraus.
package rootcheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// Events macht aus Befunden Ereignisse; RecordID je Befund und Tag (ein Vorfall pro Tag).
func Events(findings []Finding, now time.Time) []shared.AuthEvent {
	var out []shared.AuthEvent
	for _, f := range findings {
		h := sha256.Sum256([]byte(f.Check + "|" + f.Item))
		out = append(out, shared.AuthEvent{Time: now, Kind: "rootkit", Service: "rootcheck", Detail: f.Check + ": " + f.Item,
			RecordID: "rk:" + hex.EncodeToString(h[:6]) + ":" + now.Format("2006-01-02")})
	}
	return out
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
