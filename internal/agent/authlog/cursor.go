package authlog

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Cursor merkt sich, bis wohin gelesen wurde (journald-Cursor, Datei-Offset/Inode,
// Windows RecordId/Zeit, macOS-Zeit). Persistiert als auth-cursor.json im Zustandsverzeichnis.
type Cursor struct {
	Journal  string `json:"journal,omitempty"`
	File     string `json:"file,omitempty"`
	Offset   int64  `json:"offset,omitempty"`
	Inode    uint64 `json:"inode,omitempty"`
	LastTime string `json:"last_time,omitempty"` // RFC3339 (Windows/macOS)
	RecordID int64  `json:"record_id,omitempty"`
}

const maxBatch = 500 // Ereignisse je Lauf; der Rest folgt beim nächsten

func cursorPath(stateDir string) string { return filepath.Join(stateDir, "auth-cursor.json") }

func loadCursor(stateDir string) Cursor {
	var c Cursor
	if b, err := os.ReadFile(cursorPath(stateDir)); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func saveCursor(stateDir string, c Cursor) {
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	tmp := cursorPath(stateDir) + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, cursorPath(stateDir))
	}
}
