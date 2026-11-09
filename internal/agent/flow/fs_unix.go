//go:build !windows

package flow

import (
	"fmt"
	"os"
	"syscall"
)

func fileOwner(p string) (int, int, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("Besitzer nicht lesbar")
	}
	return int(st.Uid), int(st.Gid), nil
}

func chownPath(p string, uid, gid int) error { return os.Chown(p, uid, gid) }
