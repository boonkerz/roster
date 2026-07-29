//go:build !linux && !windows

package collect

// RebootPending ist auf sonstigen Systemen (z. B. macOS) nicht ermittelbar.
func RebootPending() (pending, known bool) { return false, false }
