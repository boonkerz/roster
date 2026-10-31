//go:build !windows

package compliance

import (
	"io/fs"
	"syscall"
)

func ownerIDs(fi fs.FileInfo) (int, int) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), int(st.Gid)
	}
	return -1, -1
}
