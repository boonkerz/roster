//go:build windows

package logmon

import "os"

// Windows hat keine Inode; Rotation wird über „Datei kürzer als Offset“ erkannt.
func inodeOf(os.FileInfo) uint64 { return 0 }
