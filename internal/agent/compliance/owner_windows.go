//go:build windows

package compliance

import "io/fs"

func ownerIDs(_ fs.FileInfo) (int, int) { return -1, -1 }
