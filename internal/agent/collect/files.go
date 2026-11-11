package collect

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// FileEntry ist ein Eintrag einer Verzeichnisauflistung.
type FileEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"` // Unix-Sekunden
}

// FileListing ist das Ergebnis von BrowseDir.
type FileListing struct {
	Path    string      `json:"path"`
	Parent  string      `json:"parent"`
	Entries []FileEntry `json:"entries"`
	Error   string      `json:"error,omitempty"`
}

// MaxFileTransfer begrenzt Datei-Downloads/-Uploads (wie der Server; gestreamt, also
// ohne Speicherbedarf).
const MaxFileTransfer = 4 << 30 // 4 GiB

const maxListEntries = 2000

// BrowseDir listet die direkten Einträge eines Verzeichnisses (ohne Rekursion,
// schnell). Leerer Pfad -> Wurzeln (Laufwerke bzw. "/"). Liefert JSON.
func BrowseDir(path string) string {
	if path == "" {
		return rootsJSON()
	}
	l := FileListing{Path: path, Parent: filepath.Dir(path)}
	if l.Parent == path {
		l.Parent = ""
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		l.Error = err.Error()
		b, _ := json.Marshal(l)
		return string(b)
	}
	for _, e := range entries {
		info, ierr := e.Info()
		fe := FileEntry{Name: e.Name(), Path: filepath.Join(path, e.Name()), Dir: e.IsDir()}
		if ierr == nil {
			fe.Size = info.Size()
			fe.Modified = info.ModTime().Unix()
		}
		l.Entries = append(l.Entries, fe)
	}
	// Ordner zuerst, dann alphabetisch.
	sort.Slice(l.Entries, func(i, j int) bool {
		if l.Entries[i].Dir != l.Entries[j].Dir {
			return l.Entries[i].Dir
		}
		return l.Entries[i].Name < l.Entries[j].Name
	})
	if len(l.Entries) > maxListEntries {
		l.Entries = l.Entries[:maxListEntries]
	}
	b, _ := json.Marshal(l)
	return string(b)
}

// OpenForTransfer öffnet eine Datei zum Hochladen (bis MaxFileTransfer).
func OpenForTransfer(path string) (*os.File, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	if fi.IsDir() {
		f.Close()
		return nil, 0, errors.New("ist ein Verzeichnis")
	}
	if fi.Size() > MaxFileTransfer {
		f.Close()
		return nil, 0, errTooLarge
	}
	return f, fi.Size(), nil
}

// WriteFromReader schreibt einen Datenstrom nach path: zuerst in eine Zwischendatei im
// selben Verzeichnis, nach vollständigem Empfang umbenennen. Bricht die Übertragung ab,
// bleibt eine vorhandene Datei unverändert. Rechte einer überschriebenen Datei bleiben.
func WriteFromReader(path string, r io.Reader, size int64) (int64, error) {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		if fi.IsDir() {
			return 0, errors.New("Ziel ist ein Verzeichnis")
		}
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".roster-upload-*")
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(tmp, io.LimitReader(r, MaxFileTransfer+1))
	if err == nil && n > MaxFileTransfer {
		err = errTooLarge
	}
	if err == nil && size >= 0 && n != size {
		err = fmt.Errorf("unvollständig: %d von %d Bytes", n, size)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return 0, err
	}
	return n, nil
}

type countingReader struct {
	r  io.Reader
	n  int64
	on func(int64)
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n += int64(n)
	if c.on != nil {
		c.on(c.n)
	}
	return n, err
}

// CountingReader ruft on mit der bisher gelesenen Byte-Zahl auf (für Fortschritt).
func CountingReader(r io.Reader, on func(int64)) io.Reader { return &countingReader{r: r, on: on} }

// FormatTransfer formatiert einen Übertragungsstand („120 / 1024 MB (12 %)“).
func FormatTransfer(n, total int64) string {
	const mb = 1 << 20
	if total <= 0 {
		return fmt.Sprintf("%d MB", n/mb)
	}
	return fmt.Sprintf("%d / %d MB (%d %%)", n/mb, (total+mb-1)/mb, n*100/total)
}

// MakeDir legt ein Verzeichnis an (inkl. fehlender Elternverzeichnisse).
func MakeDir(path string) error {
	if path == "" {
		return os.ErrInvalid
	}
	return os.MkdirAll(path, 0755)
}

// DeletePath entfernt eine Datei oder ein (leeres oder gefülltes) Verzeichnis.
func DeletePath(path string) error {
	if path == "" {
		return os.ErrInvalid
	}
	return os.RemoveAll(path)
}
