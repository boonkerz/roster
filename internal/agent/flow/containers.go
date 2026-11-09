package flow

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/shared"
)

// Bausteine rund um Container: Befehl im Container, Datenbank sichern, Docker aufräumen.

type containerRef struct{ ID, Name, Image string }

// parsePS liest "ID|Name|Image"-Zeilen (rein, getestet).
func parsePS(out string) []containerRef {
	var list []containerRef
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(l), "|", 3)
		if len(f) == 3 && f[0] != "" {
			list = append(list, containerRef{ID: f[0], Name: f[1], Image: f[2]})
		}
	}
	return list
}

// resolveContainers findet laufende Container: Compose-Dienst (im Projekt des vorigen
// „Dateien finden“, sonst projektübergreifend) oder Namensteil.
func (r *runner) resolveContainers(ctx context.Context, service, name string) ([]containerRef, error) {
	format := "{{.ID}}|{{.Names}}|{{.Image}}"
	var out []containerRef
	switch {
	case service != "":
		var projects []string
		if r.last != nil {
			for _, p := range r.last.Projects {
				if p.Project != "" {
					projects = append(projects, p.Project)
				}
			}
		}
		if len(projects) == 0 {
			projects = []string{""}
		}
		for _, p := range projects {
			args := []string{"ps", "--format", format, "--filter", "label=com.docker.compose.service=" + service}
			if p != "" {
				args = append(args, "--filter", "label=com.docker.compose.project="+p)
			}
			o, err := dockerOut(ctx, args...)
			if err != nil {
				return nil, err
			}
			out = append(out, parsePS(o)...)
		}
	case name != "":
		o, err := dockerOut(ctx, "ps", "--format", format)
		if err != nil {
			return nil, err
		}
		for _, c := range parsePS(o) {
			if strings.Contains(c.Name, name) {
				out = append(out, c)
			}
		}
	default:
		return nil, fmt.Errorf("Dienst oder Container-Name angeben")
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("kein laufender Container gefunden (%s)", orDefault(service, name))
	}
	return out, nil
}

// --- Befehl im Container ---

func (r *runner) containerExec(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	if r.last != nil {
		st.Files, st.Projects = r.last.Files, r.last.Projects
	}
	cmdText := strings.TrimSpace(c.str("command"))
	if cmdText == "" {
		return shared.NodeResult{Status: "fail", Output: "Befehl fehlt"}, st
	}
	targets, err := r.resolveContainers(ctx, strings.TrimSpace(c.str("service")), strings.TrimSpace(c.str("container")))
	if err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	if !c.bool("all") {
		targets = targets[:1]
	}
	var outs []string
	for _, t := range targets {
		args := execArgs(t.ID, c.str("user"), c.str("workdir"), cmdText)
		if r.opt.DryRun {
			outs = append(outs, fmt.Sprintf("würde in %s ausführen: %s", t.Name, cmdText))
			continue
		}
		tctx, cancel := context.WithTimeout(ctx, time.Duration(c.num("timeout_s", 600))*time.Second)
		cmd := exec.CommandContext(tctx, "docker", args...)
		o, err := cmd.CombinedOutput()
		cancel()
		st.Out += string(o)
		outs = append(outs, fmt.Sprintf("$ [%s] %s\n%s", t.Name, cmdText, strings.TrimSpace(string(o))))
		if err != nil {
			return shared.NodeResult{Status: "fail", Output: strings.Join(outs, "\n\n") + "\n" + err.Error()}, st
		}
	}
	status := "ok"
	if r.opt.DryRun {
		status = "dry"
	}
	return shared.NodeResult{Status: status, Output: strings.Join(outs, "\n\n")}, st
}

// execArgs baut die Argumente für docker exec (rein, getestet).
func execArgs(id, user, workdir, command string) []string {
	args := []string{"exec"}
	if strings.TrimSpace(user) != "" {
		args = append(args, "-u", strings.TrimSpace(user))
	}
	if strings.TrimSpace(workdir) != "" {
		args = append(args, "-w", strings.TrimSpace(workdir))
	}
	return append(args, id, "sh", "-c", command)
}

// --- Datenbank sichern ---

// dumpScripts schreiben den Dump auf stdout; Zugangsdaten aus der Container-Umgebung,
// DBS (leer = alle) kommt per docker exec -e.
var dumpScripts = map[string]string{
	"mysql": `P="${MARIADB_ROOT_PASSWORD:-${MYSQL_ROOT_PASSWORD:-}}"; D=mysqldump; command -v mariadb-dump >/dev/null 2>&1 && D=mariadb-dump
A="--single-transaction --routines --events"; if [ -n "$DBS" ]; then A="$A --databases $DBS"; else A="$A --all-databases"; fi
if [ -n "$P" ]; then MYSQL_PWD="$P" exec $D -uroot $A; else exec $D -uroot $A; fi`,
	"mongo": `U="${MONGO_INITDB_ROOT_USERNAME:-}"; P="${MONGO_INITDB_ROOT_PASSWORD:-}"; A="--archive --quiet"; [ -n "$DBS" ] && A="$A --db=$DBS"
if [ -n "$U" ]; then exec mongodump $A -u "$U" -p "$P" --authenticationDatabase admin; else exec mongodump $A; fi`,
	"postgres": `U="${POSTGRES_USER:-postgres}"; if [ -n "$DBS" ]; then exec pg_dump -U "$U" "$DBS"; else exec pg_dumpall -U "$U"; fi`,
}

var dumpClients = map[string]string{
	"mysql":    "command -v mariadb-dump >/dev/null 2>&1 || command -v mysqldump >/dev/null 2>&1",
	"mongo":    "command -v mongodump >/dev/null 2>&1",
	"postgres": "command -v pg_dumpall >/dev/null 2>&1",
}

var dumpExt = map[string]string{"mysql": ".sql.gz", "postgres": ".sql.gz", "mongo": ".archive.gz"}

func (r *runner) dbBackup(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	if r.last != nil {
		st.Files, st.Projects = r.last.Files, r.last.Projects
	}
	targets, err := r.resolveContainers(ctx, strings.TrimSpace(c.str("service")), strings.TrimSpace(c.str("container")))
	if err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	t := targets[0]
	engine := c.str("engine")
	if engine == "" || engine == "auto" {
		engine = collect.DBEngine(t.Image)
	}
	script, ok := dumpScripts[engine]
	if !ok {
		return shared.NodeResult{Status: "fail", Output: fmt.Sprintf("Datenbank-Art von %s (%s) nicht erkannt – Engine angeben (mysql, mongo, postgres)", t.Name, t.Image)}, st
	}
	dir := orDefault(strings.TrimSpace(c.str("dir")), "/var/backups/roster")
	name := safeName(t.Name)
	file := filepath.Join(dir, name+"-"+time.Now().Format("20060102-150405")+dumpExt[engine])
	st.Value = file
	if r.opt.DryRun {
		probe := exec.CommandContext(ctx, "docker", "exec", t.ID, "sh", "-c", dumpClients[engine])
		if err := probe.Run(); err != nil {
			return shared.NodeResult{Status: "fail", Output: fmt.Sprintf("kein Dump-Programm für %s im Container %s", engine, t.Name)}, st
		}
		return shared.NodeResult{Status: "dry", Output: fmt.Sprintf("würde %s (%s) nach %s sichern", t.Name, engine, file)}, st
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	tmp := file + ".part"
	n, stderr, err := dumpTo(ctx, tmp, t.ID, script, strings.TrimSpace(c.str("databases")), time.Duration(c.num("timeout_s", 3600))*time.Second)
	if err == nil && n < 1024 {
		err = fmt.Errorf("Dump ist verdächtig klein (%d Bytes)", n)
	}
	if err != nil {
		os.Remove(tmp)
		return shared.NodeResult{Status: "fail", Output: fmt.Sprintf("Sicherung von %s fehlgeschlagen: %v\n%s", t.Name, err, clip(stderr, 2000))}, st
	}
	if err := os.Rename(tmp, file); err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	removed := pruneDumps(dir, name+"-", int(c.num("keep", 5)))
	fi, _ := os.Stat(file)
	out := fmt.Sprintf("%s (%s) gesichert: %s – %s unkomprimiert, %s Datei", t.Name, engine, file, humanBytes(n), humanBytes(fi.Size()))
	if len(removed) > 0 {
		out += fmt.Sprintf("\nalte Sicherungen entfernt: %s", strings.Join(removed, ", "))
	}
	return shared.NodeResult{Status: "ok", Output: out, Files: []string{file}}, st
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// dumpTo streamt den Dump gzip-komprimiert in die Datei; liefert die unkomprimierte Größe.
func dumpTo(ctx context.Context, path, id, script, dbs string, timeout time.Duration) (int64, string, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	cw := &countWriter{w: gz}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(tctx, "docker", "exec", "-e", "DBS="+dbs, id, "sh", "-c", script)
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = cw, &stderr
	runErr := cmd.Run()
	if err := gz.Close(); err != nil && runErr == nil {
		runErr = err
	}
	return cw.n, stderr.String(), runErr
}

// pruneDumps behält die neuesten keep Dateien mit dem Präfix (Name sortiert = Zeit sortiert).
func pruneDumps(dir, prefix string, keep int) []string {
	if keep < 1 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && !strings.HasSuffix(e.Name(), ".part") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var removed []string
	for len(names) > keep {
		if os.Remove(filepath.Join(dir, names[0])) == nil {
			removed = append(removed, names[0])
		}
		names = names[1:]
	}
	return removed
}

func safeName(s string) string {
	s = strings.TrimPrefix(s, "/")
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, s)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// --- Docker aufräumen ---

type imageInfo struct {
	ID, Repo, Tag, Size string
	Created             time.Time
}

// pruneCandidates wählt die Images, die prune entfernen würde (rein, getestet).
func pruneCandidates(images []imageInfo, used map[string]bool, mode string, olderThan time.Duration, now time.Time) []imageInfo {
	var out []imageInfo
	for _, im := range images {
		if used[im.ID] || now.Sub(im.Created) < olderThan {
			continue
		}
		dangling := im.Repo == "<none>" || im.Tag == "<none>"
		if mode != "unused" && !dangling {
			continue
		}
		out = append(out, im)
	}
	return out
}

func parseImageList(out string) []imageInfo {
	var list []imageInfo
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(l), "|", 5)
		if len(f) != 5 {
			continue
		}
		created, _ := time.Parse("2006-01-02 15:04:05 -0700 MST", f[3])
		list = append(list, imageInfo{ID: shortImageID(f[0]), Repo: f[1], Tag: f[2], Created: created, Size: f[4]})
	}
	return list
}

func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		id = id[:12]
	}
	return id
}

func (r *runner) dockerPrune(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	if r.last != nil {
		st.Files, st.Projects = r.last.Files, r.last.Projects
	}
	mode := orDefault(c.str("mode"), "dangling")
	days := c.num("older_than_days", 7)
	older := time.Duration(days*24) * time.Hour
	imgOut, err := dockerOut(ctx, "image", "ls", "-a", "--no-trunc", "--format", "{{.ID}}|{{.Repository}}|{{.Tag}}|{{.CreatedAt}}|{{.Size}}")
	if err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	used := map[string]bool{}
	if ids, err := dockerOut(ctx, "ps", "-aq"); err == nil && strings.TrimSpace(ids) != "" {
		if o, err := dockerOut(ctx, append([]string{"inspect", "-f", "{{.Image}}"}, strings.Fields(ids)...)...); err == nil {
			for _, id := range strings.Fields(o) {
				used[shortImageID(id)] = true
			}
		}
	}
	cand := pruneCandidates(parseImageList(imgOut), used, mode, older, time.Now())
	var lines []string
	for _, im := range cand {
		lines = append(lines, fmt.Sprintf("%s:%s (%s, %s)", im.Repo, im.Tag, im.ID, im.Size))
	}
	if r.opt.DryRun {
		return shared.NodeResult{Status: "dry", Output: fmt.Sprintf("würde %d Image(s) entfernen:\n%s", len(cand), strings.Join(lines, "\n"))}, st
	}
	if len(cand) == 0 {
		return shared.NodeResult{Status: "ok", Output: "nichts aufzuräumen"}, st
	}
	args := []string{"image", "prune", "-f", "--filter", fmt.Sprintf("until=%dh", int(days*24))}
	if mode == "unused" {
		args = append(args, "-a")
	}
	o, err := dockerOut(ctx, args...)
	if err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	return shared.NodeResult{Status: "ok", Output: strings.TrimSpace(o)}, st
}

// --- Benachrichtigen ---

func (r *runner) notify(c config) (shared.NodeResult, *step) {
	st := &step{}
	if r.last != nil {
		st.Files, st.Projects = r.last.Files, r.last.Projects
	}
	n := shared.FlowNotification{Subject: strings.TrimSpace(c.str("subject")), Message: strings.TrimSpace(c.str("message")), Severity: orDefault(c.str("severity"), "info")}
	if n.Message == "" && n.Subject == "" {
		return shared.NodeResult{Status: "fail", Output: "Betreff oder Nachricht angeben"}, st
	}
	if r.opt.DryRun {
		return shared.NodeResult{Status: "dry", Output: fmt.Sprintf("würde melden (%s): %s\n%s", n.Severity, n.Subject, n.Message)}, st
	}
	r.notes = append(r.notes, n)
	return shared.NodeResult{Status: "ok", Output: fmt.Sprintf("Meldung (%s): %s\n%s", n.Severity, n.Subject, n.Message)}, st
}
