//go:build linux

package authlog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// journalIdents sind die Programme, deren Meldungen gelesen werden.
var journalIdents = []string{"sshd", "sshd-session", "sudo", "su", "su-l", "useradd", "userdel", "usermod", "groupadd", "gpasswd", "passwd", "chpasswd", "login"}

// Collect liest neue Ereignisse: journald bevorzugt (Cursor), sonst /var/log/auth.log bzw.
// /var/log/secure (Offset + Inode; ROSTER_AUTHLOG_PATH überschreibt den Pfad, z. B. für Tests).
func Collect(ctx context.Context, stateDir string) []shared.AuthEvent {
	cur := loadCursor(stateDir)
	if path := os.Getenv("ROSTER_AUTHLOG_PATH"); path != "" {
		return collectFile(path, stateDir, &cur)
	}
	if _, err := exec.LookPath("journalctl"); err == nil {
		if ev, ok := collectJournal(ctx, stateDir, &cur); ok {
			return ev
		}
	}
	for _, path := range []string{"/var/log/auth.log", "/var/log/secure"} {
		if _, err := os.Stat(path); err == nil {
			return collectFile(path, stateDir, &cur)
		}
	}
	return nil
}

// collectJournal ruft journalctl mit Disjunktion über die Programme auf. ok=false, wenn
// journald nicht nutzbar ist (dann Datei-Fallback).
func collectJournal(ctx context.Context, stateDir string, cur *Cursor) ([]shared.AuthEvent, bool) {
	args := []string{"-o", "json", "--no-pager", "-q"}
	if cur.Journal != "" {
		args = append(args, "--after-cursor="+cur.Journal)
	} else {
		args = append(args, "--since=-1h")
	}
	for i, id := range journalIdents {
		if i > 0 {
			args = append(args, "+")
		}
		args = append(args, "SYSLOG_IDENTIFIER="+id)
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return nil, false
	}
	var events []shared.AuthEvent
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	lastCursor := cur.Journal
	for sc.Scan() {
		var rec struct {
			Cursor  string          `json:"__CURSOR"`
			TS      string          `json:"__REALTIME_TIMESTAMP"`
			Ident   string          `json:"SYSLOG_IDENTIFIER"`
			Comm    string          `json:"_COMM"`
			Message json.RawMessage `json:"MESSAGE"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		lastCursor = rec.Cursor
		us, _ := strconv.ParseInt(rec.TS, 10, 64)
		ts := time.UnixMicro(us)
		ident := rec.Ident
		if ident == "" {
			ident = rec.Comm
		}
		msg := journalMessage(rec.Message)
		if ev, ok := ParseMessage(ident, msg, ts); ok {
			ev.RecordID = "j:" + shortHash(rec.Cursor)
			events = append(events, ev)
			if len(events) >= maxBatch {
				break
			}
		}
	}
	_ = out.Close()
	_ = cmd.Wait()
	if sc.Err() != nil && len(events) == 0 && lastCursor == cur.Journal {
		return nil, false
	}
	if lastCursor != cur.Journal {
		cur.Journal = lastCursor
		saveCursor(stateDir, *cur)
	}
	return events, true
}

// journalMessage: MESSAGE ist String oder Byte-Array (bei ungültigem UTF-8).
func journalMessage(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b []byte
	if json.Unmarshal(raw, &b) == nil {
		return string(b)
	}
	return ""
}

// collectFile liest neue Zeilen ab dem gemerkten Offset; bei Rotation (Inode anders oder
// Datei kürzer) von vorn. Beim allerersten Lauf wird ans Ende gesprungen (kein Altbestand).
func collectFile(path, stateDir string, cur *Cursor) []shared.AuthEvent {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	var inode uint64
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		inode = st.Ino
	}
	offset := cur.Offset
	switch {
	case cur.File != path || cur.Inode != inode || offset > fi.Size():
		if cur.File == "" {
			offset = fi.Size() // erster Lauf: nur Neues ab jetzt
		} else {
			offset = 0 // Rotation
		}
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil
	}
	var events []shared.AuthEvent
	r := bufio.NewReader(f)
	now := time.Now()
	pos := offset
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			break // unvollständige letzte Zeile beim nächsten Lauf
		}
		lineStart := pos
		pos += int64(len(line))
		if ev, ok := ParseSyslogLine(strings.TrimRight(line, "\n"), now); ok {
			ev.RecordID = fmt.Sprintf("f:%d:%d", inode, lineStart)
			events = append(events, ev)
			if len(events) >= maxBatch {
				break
			}
		}
	}
	cur.File, cur.Inode, cur.Offset = path, inode, pos
	saveCursor(stateDir, *cur)
	return events
}
