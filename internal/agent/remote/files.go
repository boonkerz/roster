package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/agent/transport"
	"github.com/boonkerz/roster/internal/shared"
)

// fileIdle beendet eine Datei-Sitzung, in der nichts mehr passiert.
const fileIdle = 30 * time.Minute

// handleFiles bedient eine Datei-Sitzung des nativen Viewers (siehe shared.FileReq).
func handleFiles(ctx context.Context, client *transport.Client, agentToken, session string, log *slog.Logger) {
	conn, err := client.DialFiles(ctx, agentToken, session)
	if err != nil {
		log.Warn("datei-sitzung: verbinden fehlgeschlagen", "err", err)
		return
	}
	defer conn.CloseNow()
	ServeFileSession(ctx, conn, log)
}

// ServeFileSession bedient die Operationen einer verbundenen Datei-Sitzung bis zum
// Verbindungsende (auch für Tests exportiert).
func ServeFileSession(ctx context.Context, conn *websocket.Conn, log *slog.Logger) {
	fs := &fileSession{conn: conn, log: log}
	for {
		rctx, cancel := context.WithTimeout(ctx, fileIdle)
		typ, data, err := conn.Read(rctx)
		cancel()
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue // verirrte Daten (abgebrochener Upload) ignorieren
		}
		var req shared.FileReq
		if err := json.Unmarshal(data, &req); err != nil {
			return
		}
		if err := fs.handle(ctx, req); err != nil {
			log.Info("datei-sitzung beendet", "op", req.Op, "err", err)
			return
		}
	}
}

type fileSession struct {
	conn *websocket.Conn
	log  *slog.Logger
}

func (f *fileSession) reply(ctx context.Context, r shared.FileResp) error {
	b, _ := json.Marshal(r)
	return f.conn.Write(ctx, websocket.MessageText, b)
}

func (f *fileSession) fail(ctx context.Context, err error) error {
	return f.reply(ctx, shared.FileResp{Error: err.Error()})
}

// handle führt eine Operation aus. Ein zurückgegebener Fehler beendet die Sitzung
// (Verbindung kaputt); fachliche Fehler gehen als {error} an den Viewer.
func (f *fileSession) handle(ctx context.Context, req shared.FileReq) error {
	switch req.Op {
	case "hello":
		return f.reply(ctx, shared.FileResp{OK: true})
	case "list":
		return f.reply(ctx, shared.FileResp{OK: true, Listing: json.RawMessage(collect.BrowseDir(req.Path))})
	case "walk":
		entries, err := walkTree(req.Path)
		if err != nil {
			return f.fail(ctx, err)
		}
		return f.reply(ctx, shared.FileResp{OK: true, Entries: entries})
	case "stat":
		r := shared.FileResp{OK: true}
		if fi, err := os.Stat(req.Path); err == nil {
			r.Exists, r.Dir, r.Size = true, fi.IsDir(), fi.Size()
		}
		if fi, err := os.Stat(req.Path + shared.FilePartSuffix); err == nil {
			r.Part = fi.Size()
		}
		return f.reply(ctx, r)
	case "mkdir":
		if err := collect.MakeDir(req.Path); err != nil {
			return f.fail(ctx, err)
		}
		return f.reply(ctx, shared.FileResp{OK: true})
	case "delete":
		if !isSafeDelete(req.Path) {
			return f.fail(ctx, errors.New("dieser Pfad wird nicht gelöscht"))
		}
		if err := collect.DeletePath(req.Path); err != nil {
			return f.fail(ctx, err)
		}
		return f.reply(ctx, shared.FileResp{OK: true})
	case "get":
		return f.get(ctx, req)
	case "put":
		return f.put(ctx, req)
	}
	return f.fail(ctx, fmt.Errorf("unbekannte operation %q", req.Op))
}

// walkTree listet einen Ordner rekursiv (relative Pfade mit "/", Symlinks nicht verfolgt).
func walkTree(root string) ([]shared.FileWalkEntry, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil // fehlendes Ziel = leer
		}
		return nil, err
	}
	var out []shared.FileWalkEntry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unlesbare Teile überspringen
		}
		if p == root {
			return nil
		}
		if len(out) >= shared.FileWalkMax {
			return errors.New("zu viele Einträge")
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		e := shared.FileWalkEntry{Rel: filepath.ToSlash(rel), Dir: d.IsDir()}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !e.Dir {
			if fi, ierr := d.Info(); ierr == nil {
				e.Size = fi.Size()
			}
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

func (f *fileSession) get(ctx context.Context, req shared.FileReq) error {
	file, size, err := collect.OpenForTransfer(req.Path)
	if err != nil {
		return f.fail(ctx, err)
	}
	defer file.Close()
	if req.Offset < 0 || req.Offset > size {
		return f.fail(ctx, errors.New("ungültiger offset"))
	}
	if _, err := file.Seek(req.Offset, io.SeekStart); err != nil {
		return f.fail(ctx, err)
	}
	if err := f.reply(ctx, shared.FileResp{OK: true, Size: size}); err != nil {
		return err
	}
	buf := make([]byte, shared.FileChunk)
	remaining := size - req.Offset
	for remaining > 0 {
		n, rerr := io.ReadFull(file, buf[:min(int64(len(buf)), remaining)])
		if n > 0 {
			if err := f.conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
				return err
			}
			remaining -= int64(n)
		}
		if rerr != nil && remaining > 0 {
			return f.reply(ctx, shared.FileResp{Error: "lesen: " + rerr.Error()})
		}
	}
	return f.reply(ctx, shared.FileResp{OK: true, EOF: true})
}

func (f *fileSession) put(ctx context.Context, req shared.FileReq) error {
	if req.Path == "" || req.Size < 0 || req.Size > collect.MaxFileTransfer {
		return f.fail(ctx, errors.New("ungültiger pfad oder größe"))
	}
	if fi, err := os.Stat(req.Path); err == nil && fi.IsDir() {
		return f.fail(ctx, errors.New("Ziel ist ein Verzeichnis"))
	}
	if err := os.MkdirAll(filepath.Dir(req.Path), 0o755); err != nil {
		return f.fail(ctx, err)
	}
	part := req.Path + shared.FilePartSuffix
	offset := int64(0)
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if req.Offset > 0 {
		if fi, err := os.Stat(part); err == nil && fi.Size() == req.Offset && req.Offset <= req.Size {
			offset, flags = req.Offset, os.O_WRONLY|os.O_APPEND
		}
	}
	out, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return f.fail(ctx, err)
	}
	if err := f.reply(ctx, shared.FileResp{OK: true, Offset: offset}); err != nil {
		out.Close()
		return err
	}
	remaining := req.Size - offset
	var werr error
	for remaining > 0 {
		typ, data, err := f.conn.Read(ctx)
		if err != nil {
			out.Close() // Teildatei bleibt für das Fortsetzen
			return err
		}
		if typ != websocket.MessageBinary || int64(len(data)) > remaining {
			out.Close()
			return errors.New("protokollfehler beim upload")
		}
		if werr == nil {
			_, werr = out.Write(data)
		}
		remaining -= int64(len(data))
	}
	if cerr := out.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return f.fail(ctx, werr)
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(req.Path); err == nil {
		mode = fi.Mode().Perm()
	}
	_ = os.Chmod(part, mode)
	if err := os.Rename(part, req.Path); err != nil {
		return f.fail(ctx, err)
	}
	return f.reply(ctx, shared.FileResp{OK: true})
}

// isSafeDelete schützt vor versehentlichem Löschen ganzer Wurzeln.
func isSafeDelete(p string) bool {
	c := filepath.Clean(p)
	return c != "/" && c != "." && !(len(c) <= 3 && strings.HasSuffix(c, `:\`))
}
