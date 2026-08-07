//go:build !linux && !darwin && !windows && !freebsd

package remote

import "fmt"

// startPTY ist auf nicht unterstützten Plattformen ein Stub.
func startPTY(_shell, _runas string) (ptySession, error) {
	return nil, fmt.Errorf("remote-terminal auf dieser plattform nicht unterstützt")
}
