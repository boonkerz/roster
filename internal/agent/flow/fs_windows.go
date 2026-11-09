//go:build windows

package flow

import "fmt"

func fileOwner(string) (int, int, error) {
	return 0, 0, fmt.Errorf("chown/owner nur unter Linux, macOS und FreeBSD")
}

func chownPath(string, int, int) error {
	return fmt.Errorf("chown/owner nur unter Linux, macOS und FreeBSD")
}
