//go:build unix

package api

import "golang.org/x/sys/unix"

// diskFree liefert den für den Dienst verfügbaren Platz im Verzeichnis.
func diskFree(dir string) (uint64, bool) {
	var st unix.Statfs_t
	if unix.Statfs(dir, &st) != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
