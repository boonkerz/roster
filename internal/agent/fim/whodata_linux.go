//go:build linux

package fim

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

type fimRule shared.FIMRule

func (r fimRule) path() string { return r.Path }

// EnsureAuditRules legt für die Regelpfade auditd-Watch-Regeln (-w <p> -p wa -k roster_fim)
// an und entfernt verwaiste. Ohne auditctl/auditd: Fehler (einmal loggen, weiter ohne Who-Data).
func EnsureAuditRules(ctx context.Context, rules []shared.FIMRule) error {
	if _, err := exec.LookPath("auditctl"); err != nil {
		return errors.New("auditctl nicht installiert (Paket auditd)")
	}
	rl := make([]ruleLike, 0, len(rules))
	for _, r := range rules {
		if platformMatches(r.Platforms) {
			rl = append(rl, fimRule(r))
		}
	}
	want := auditWatchPaths(rl)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "auditctl", "-l").Output()
	if err != nil {
		return errors.New("auditctl -l: " + strings.TrimSpace(string(out)) + " " + err.Error())
	}
	have := ParseAuditRules(string(out))
	wantSet := map[string]bool{}
	var firstErr error
	for _, p := range want {
		wantSet[p] = true
		if have[p] {
			continue
		}
		if o, err := exec.CommandContext(cctx, "auditctl", "-w", p, "-p", "wa", "-k", auditKey).CombinedOutput(); err != nil && !strings.Contains(string(o), "exists") && firstErr == nil {
			firstErr = errors.New("auditctl -w " + p + ": " + strings.TrimSpace(string(o)))
		}
	}
	for p := range have {
		if !wantSet[p] {
			_ = exec.CommandContext(cctx, "auditctl", "-W", p, "-p", "wa", "-k", auditKey).Run()
		}
	}
	return firstErr
}

// CollectWho liest neue Audit-Ereignisse seit dem letzten Aufruf (ausearch --checkpoint)
// und liefert Pfad → Verursacher.
func CollectWho(ctx context.Context, stateDir string) map[string]Who {
	if _, err := exec.LookPath("ausearch"); err != nil {
		return nil
	}
	chk := filepath.Join(stateDir, "ausearch.chk")
	args := []string{"-k", auditKey, "--raw", "--checkpoint", chk}
	if _, err := os.Stat(chk); err != nil {
		// Erster Lauf: nur die letzte Stunde, nicht das ganze Protokoll.
		args = append(args, "-ts", "recent")
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(cctx, "ausearch", args...).Output() // Exit ≠ 0 auch bei "keine Treffer"
	if len(out) == 0 {
		return nil
	}
	cache := map[string]string{}
	return ParseAusearch(string(out), func(uid string) string {
		if n, ok := cache[uid]; ok {
			return n
		}
		n := ""
		if u, err := user.LookupId(uid); err == nil {
			n = u.Username
		}
		cache[uid] = n
		return n
	})
}

// whoFold: Linux-Pfade sind case-sensitiv.
const whoFold = false
