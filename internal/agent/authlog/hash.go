package authlog

import (
	"crypto/sha256"
	"encoding/hex"
)

// shortHash kürzt lange Kennungen (journald-Cursor) auf 16 Hex-Zeichen.
func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}
