package shared

import "encoding/json"

// Datei-Sitzung: direkter Kanal Viewer ↔ Agent durch den Tunnel (WebSocket, vom Server
// nur weitergereicht). Anfragen und Antworten sind JSON-Textnachrichten, Dateiinhalte
// laufen als Binärnachrichten (FileChunk groß). Es läuft immer genau eine Operation.
//
//	hello                     → {ok}
//	list  path                → {ok, listing}            (Format wie browse_dir)
//	walk  path                → {ok, entries}            (rekursiv, relative Pfade mit "/")
//	stat  path                → {ok, exists, dir, size, part}
//	get   path offset         → {ok, size} + Binär ab offset … + {ok, eof}
//	put   path size offset    → {ok, offset} + Binär (size-offset Bytes) → {ok}
//	mkdir path / delete path  → {ok}
//
// put schreibt in „<path>.roster-part“ und benennt erst nach vollständigem Empfang um –
// bricht die Verbindung ab, setzt der nächste put mit offset = Größe der Teildatei fort.

const (
	FileChunk      = 512 << 10
	FilePartSuffix = ".roster-part"
	FileWalkMax    = 200000
)

type FileReq struct {
	Op     string `json:"op"`
	Path   string `json:"path,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Offset int64  `json:"offset,omitempty"`
}

type FileResp struct {
	OK      bool            `json:"ok"`
	Error   string          `json:"error,omitempty"`
	Listing json.RawMessage `json:"listing,omitempty"`
	Entries []FileWalkEntry `json:"entries,omitempty"`
	Exists  bool            `json:"exists,omitempty"`
	Dir     bool            `json:"dir,omitempty"`
	Size    int64           `json:"size,omitempty"`
	Part    int64           `json:"part,omitempty"` // Größe einer vorhandenen Teildatei
	Offset  int64           `json:"offset,omitempty"`
	EOF     bool            `json:"eof,omitempty"`
	Version string          `json:"version,omitempty"`
}

type FileWalkEntry struct {
	Rel  string `json:"rel"`
	Dir  bool   `json:"dir,omitempty"`
	Size int64  `json:"size,omitempty"`
}
