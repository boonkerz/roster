//go:build linux

package collect

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// patchHistoryOS liest den Patch-Verlauf aus den Logs des Paketmanagers (apt, dnf, yum,
// pacman). Rotierte apt-Logs (.1.gz …) werden mitgelesen, damit das 90-Tage-Fenster voll ist.
func patchHistoryOS(_ context.Context, since time.Time) []shared.PatchEvent {
	var out []shared.PatchEvent
	if files, _ := filepath.Glob("/var/log/apt/history.log*"); len(files) > 0 {
		sort.Strings(files)
		for _, f := range files {
			out = append(out, parseAptHistory(readMaybeGzip(f), since)...)
		}
	}
	if b := readMaybeGzip("/var/log/dnf.rpm.log"); b != "" {
		out = append(out, parseDnfRpmLog(b, since)...)
	} else if b := readMaybeGzip("/var/log/yum.log"); b != "" {
		out = append(out, parseYumLog(b, since, time.Now())...)
	}
	if b := readMaybeGzip("/var/log/pacman.log"); b != "" {
		out = append(out, parsePacmanLog(b, since)...)
	}
	return out
}

// readMaybeGzip liest eine Datei (gzip-komprimiert bei .gz); Fehler → leer.
func readMaybeGzip(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var r io.Reader = f
	if filepath.Ext(path) == ".gz" {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return ""
		}
		defer gz.Close()
		r = gz
	}
	b, err := io.ReadAll(io.LimitReader(r, 32<<20))
	if err != nil {
		return ""
	}
	return string(b)
}
