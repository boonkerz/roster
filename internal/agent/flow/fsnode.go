package flow

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/boonkerz/roster/internal/shared"
	"github.com/boonkerz/roster/internal/shared/fsops"
	"github.com/boonkerz/roster/internal/shared/yamledit"
)

// Baustein „Dateisystem“: mkdir/chmod/chown/file – idempotent, mit Vorschau und Rückbau.

func (r *runner) fsNode(c config) (shared.NodeResult, *step) {
	st := &step{}
	if r.last != nil {
		st.Files, st.Projects = r.last.Files, r.last.Projects
	}
	ops, err := fsops.ParseOps(c.str("ops"))
	if err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	var log []string
	changes := 0
	for _, op := range ops {
		for _, p := range yamledit.ExpandBraces(op.Path) {
			p = filepath.Clean(p)
			if !filepath.IsAbs(p) || p == filepath.Dir(p) {
				return shared.NodeResult{Status: "fail", Output: fmt.Sprintf("Zeile %d: nur absolute Pfade (nicht die Wurzel): %s", op.Line, p)}, st
			}
			msg, changed, err := r.fsApply(op, p)
			if err != nil {
				return shared.NodeResult{Status: "fail", Output: strings.Join(append(log, fmt.Sprintf("Zeile %d (%s %s): %v", op.Line, op.Op, p, err)), "\n")}, st
			}
			if changed {
				changes++
			}
			log = append(log, fmt.Sprintf("%s %s: %s", op.Op, p, msg))
		}
	}
	out := fmt.Sprintf("%d Änderung(en)", changes)
	if r.opt.DryRun && changes > 0 {
		out += " (Vorschau – nichts geändert)"
	}
	out += "\n" + strings.Join(log, "\n")
	if changes == 0 && c.bool("stop_if_unchanged") {
		return shared.NodeResult{Status: "empty", Output: "nichts zu tun – Ablauf endet hier\n" + out}, st
	}
	status := "ok"
	if r.opt.DryRun {
		status = "dry"
	}
	return shared.NodeResult{Status: status, Output: out}, st
}

func (r *runner) fsApply(op fsops.Op, p string) (string, bool, error) {
	dry := r.opt.DryRun
	switch op.Op {
	case "mkdir":
		missing := missingDirs(p)
		if dry && len(missing) > 0 && r.planned[p] {
			return "wird von einer früheren Zeile angelegt", false, nil
		}
		if len(missing) == 0 {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return "", false, fmt.Errorf("existiert, ist aber kein Verzeichnis")
			}
			return "vorhanden", false, nil
		}
		mode := fs.FileMode(0o755)
		if m, ok := op.Opts["mode"]; ok {
			v, _ := fsops.ParseMode(m)
			mode = fs.FileMode(v)
		}
		desc := fmt.Sprintf("angelegt (%04o", mode)
		if o := op.Opts["owner"]; o != "" {
			desc += ", " + o
		}
		desc += ")"
		if dry {
			if r.planned == nil {
				r.planned = map[string]bool{}
			}
			for _, d := range missing {
				r.planned[d] = true
			}
			return "würde " + strings.Join(missing, ", ") + " anlegen" + desc[len("angelegt"):], true, nil
		}
		var uid, gid int
		owner := op.Opts["owner"]
		if owner != "" {
			var err error
			if uid, gid, err = resolveOwner(owner); err != nil {
				return "", false, err
			}
		}
		for _, d := range missing { // von außen nach innen
			if err := os.Mkdir(d, mode); err != nil && !os.IsExist(err) {
				return "", false, err
			}
			_ = os.Chmod(d, mode) // umask ausgleichen
			if owner != "" {
				if err := chownPath(d, uid, gid); err != nil {
					return "", false, err
				}
			}
			dir := d
			r.undo = append(r.undo, undoEntry{desc: "Verzeichnis " + dir + " entfernt", fn: func() error { return os.Remove(dir) }})
		}
		return desc, true, nil
	case "chmod":
		v, _ := fsops.ParseMode(op.Arg)
		want := fs.FileMode(v)
		if dry && r.planned[p] {
			return fmt.Sprintf("würde nach dem Anlegen %04o setzen", v), true, nil
		}
		fi, err := os.Stat(p)
		if err != nil {
			return "", false, err
		}
		old := fi.Mode().Perm() | (fi.Mode() & (fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky))
		if old.Perm() == want.Perm() && v <= 0o777 {
			return fmt.Sprintf("schon %04o", want.Perm()), false, nil
		}
		msg := fmt.Sprintf("%04o → %04o", old.Perm(), v)
		if dry {
			return "würde " + msg, true, nil
		}
		if err := os.Chmod(p, fileModeFromOctal(v)); err != nil {
			return "", false, err
		}
		r.undo = append(r.undo, undoEntry{desc: "Rechte von " + p + " zurück auf " + fmt.Sprintf("%04o", old.Perm()), fn: func() error { return os.Chmod(p, old) }})
		return msg, true, nil
	case "chown":
		uid, gid, err := resolveOwner(op.Arg)
		if err != nil {
			return "", false, err
		}
		if dry && r.planned[p] {
			return "würde nach dem Anlegen Besitzer " + op.Arg + " setzen", true, nil
		}
		ou, og, err := fileOwner(p)
		if err != nil {
			return "", false, err
		}
		if gid < 0 {
			gid = og
		}
		if ou == uid && og == gid {
			return "schon " + op.Arg, false, nil
		}
		msg := fmt.Sprintf("%d:%d → %d:%d", ou, og, uid, gid)
		if dry {
			return "würde " + msg, true, nil
		}
		if err := chownPath(p, uid, gid); err != nil {
			return "", false, err
		}
		r.undo = append(r.undo, undoEntry{desc: fmt.Sprintf("Besitzer von %s zurück auf %d:%d", p, ou, og), fn: func() error { return chownPath(p, ou, og) }})
		return msg, true, nil
	case "file":
		if _, err := os.Lstat(p); err == nil {
			return "vorhanden", false, nil
		}
		if dry && r.planned[p] {
			return "vorhanden (in dieser Vorschau schon geplant)", false, nil
		}
		if fi, err := os.Stat(filepath.Dir(p)); (err != nil || !fi.IsDir()) && !(dry && r.planned[filepath.Dir(p)]) {
			return "", false, fmt.Errorf("Verzeichnis %s fehlt (vorher mkdir)", filepath.Dir(p))
		}
		msg := fmt.Sprintf("angelegt (%d Bytes)", len(op.Content))
		if dry {
			if r.planned == nil {
				r.planned = map[string]bool{}
			}
			r.planned[p] = true
			return "würde " + msg, true, nil
		}
		if err := os.WriteFile(p, []byte(op.Content), 0o644); err != nil {
			return "", false, err
		}
		r.undo = append(r.undo, undoEntry{desc: "Datei " + p + " entfernt", fn: func() error { return os.Remove(p) }})
		return msg, true, nil
	}
	return "", false, fmt.Errorf("unbekannte Operation %s", op.Op)
}

// missingDirs liefert die fehlenden Verzeichnisse von außen nach innen (rein, getestet).
func missingDirs(p string) []string {
	var missing []string
	for d := filepath.Clean(p); ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil {
			break
		}
		missing = append([]string{d}, missing...)
		if d == filepath.Dir(d) {
			break
		}
	}
	return missing
}

func fileModeFromOctal(v uint32) fs.FileMode {
	m := fs.FileMode(v & 0o777)
	if v&0o4000 != 0 {
		m |= fs.ModeSetuid
	}
	if v&0o2000 != 0 {
		m |= fs.ModeSetgid
	}
	if v&0o1000 != 0 {
		m |= fs.ModeSticky
	}
	return m
}

// resolveOwner liest "benutzer[:gruppe]" (Namen oder Zahlen); gid -1 = Gruppe unverändert.
func resolveOwner(s string) (int, int, error) {
	u, g, hasG := strings.Cut(strings.TrimSpace(s), ":")
	uid, err := strconv.Atoi(u)
	if err != nil {
		usr, lerr := user.Lookup(u)
		if lerr != nil {
			return 0, 0, fmt.Errorf("Benutzer %q unbekannt", u)
		}
		uid, _ = strconv.Atoi(usr.Uid)
	}
	gid := -1
	if hasG && g != "" {
		if gid, err = strconv.Atoi(g); err != nil {
			grp, lerr := user.LookupGroup(g)
			if lerr != nil {
				return 0, 0, fmt.Errorf("Gruppe %q unbekannt", g)
			}
			gid, _ = strconv.Atoi(grp.Gid)
		}
	}
	return uid, gid, nil
}
