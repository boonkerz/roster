//go:build !windows

package fim

import (
	"io/fs"
	"os/user"
	"strconv"
	"syscall"
)

// ownerOf liefert "user:group" aus den Stat-Daten (IDs, Namen gecacht).
func ownerOf(info fs.FileInfo, cache map[string]string) string {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	key := strconv.Itoa(int(st.Uid)) + ":" + strconv.Itoa(int(st.Gid))
	if v, ok := cache[key]; ok {
		return v
	}
	u, g := strconv.Itoa(int(st.Uid)), strconv.Itoa(int(st.Gid))
	if usr, err := user.LookupId(u); err == nil {
		u = usr.Username
	}
	if grp, err := user.LookupGroupId(g); err == nil {
		g = grp.Name
	}
	cache[key] = u + ":" + g
	return cache[key]
}
