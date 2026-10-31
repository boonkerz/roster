package collect

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// ttlCache hält ein Ergebnis für eine Weile – für Abfragen, die sich selten ändern
// (Paketquellen, Auto-Update-Status, OS-Details), aber je Checkin gebraucht werden.
type ttlCache[T any] struct {
	mu sync.Mutex
	at time.Time
	v  T
}

func (c *ttlCache[T]) get(ttl time.Duration, fn func() T) T {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < ttl {
		return c.v
	}
	c.v, c.at = fn(), time.Now()
	return c.v
}

var (
	reposCache     ttlCache[[]shared.PackageRepo]
	autoUpdCache   ttlCache[string]
	osDetailsCache ttlCache[[3]string]
	sysInfoTTL     = time.Hour
)

// Repos liefert die Paketquellen des Geräts (stündlich neu gelesen).
func Repos(ctx context.Context) []shared.PackageRepo {
	return reposCache.get(sysInfoTTL, func() []shared.PackageRepo { return reposOS(ctx) })
}

// AutoUpdates liefert den Status automatischer Updates (stündlich neu ermittelt).
func AutoUpdates(ctx context.Context) string {
	return autoUpdCache.get(sysInfoTTL, func() string { return autoUpdatesOS(ctx) })
}

// OSDetails liefert Build, Codename und Edition des Betriebssystems.
func OSDetails(ctx context.Context) (build, codename, edition string) {
	v := osDetailsCache.get(sysInfoTTL, func() [3]string {
		b, c, e := osDetailsOS(ctx)
		return [3]string{b, c, e}
	})
	return v[0], v[1], v[2]
}

// readFile liest eine Datei als Text; Fehler → leer.
func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
