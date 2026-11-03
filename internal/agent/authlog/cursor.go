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
	// Logs: Cursor je Windows-Protokoll (Security, System, PowerShell) – RecordIds sind je
	// Protokoll vergeben. LastTime/RecordID oben bleiben der Legacy-Cursor für "Security".
	Logs map[string]LogCursor `json:"logs,omitempty"`
}

const maxBatch = 500 // Ereignisse je Lauf; der Rest folgt beim nächsten

// LogCursor ist der Stand eines Windows-Protokolls.
type LogCursor struct {
	LastTime string `json:"last_time,omitempty"`
	RecordID int64  `json:"record_id,omitempty"`
}

// logCursor liefert den Cursor eines Protokolls; "Security" fällt auf die Legacy-Felder zurück.
func (c *Cursor) logCursor(log string) LogCursor {
	if lc, ok := c.Logs[log]; ok {
		return lc
	}
	if log == "Security" {
		return LogCursor{LastTime: c.LastTime, RecordID: c.RecordID}
	}
	return LogCursor{}
}

// setLogCursor setzt den Cursor eines Protokolls (Security zusätzlich in die Legacy-Felder).
func (c *Cursor) setLogCursor(log string, lc LogCursor) {
	if c.Logs == nil {
		c.Logs = map[string]LogCursor{}
	}
	c.Logs[log] = lc
	if log == "Security" {
		c.LastTime, c.RecordID = lc.LastTime, lc.RecordID
	}
}

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
