package flow

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// findFiles: Modus "paths" (Globs je Zeile, optional Inhaltsfilter) oder "compose_image"
// (Compose-Dateien der Container, deren Image dem Muster entspricht; dazu .env, falls das
// Image dort steht).
func (r *runner) findFiles(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	contains := c.str("contains")
	var files []string
	switch c.str("mode") {
	case "compose_image":
		img := strings.TrimSpace(c.str("image"))
		if img == "" {
			return shared.NodeResult{Status: "fail", Output: "Image fehlt"}, st
		}
		projects, err := composeProjectsForImage(ctx, img)
		if err != nil {
			return shared.NodeResult{Status: "fail", Output: err.Error()}, st
		}
		seen := map[string]bool{}
		for _, p := range projects {
			cands := append([]string{}, p.Files...)
			cands = append(cands, filepath.Join(p.Dir, ".env"))
			for _, f := range cands {
				data, err := os.ReadFile(f)
				if err != nil || seen[f] || !containsAnyRepo(data, p.Images) {
					continue
				}
				if contains != "" && !bytes.Contains(data, []byte(contains)) {
					continue
				}
				seen[f] = true
				files = append(files, f)
			}
		}
		st.Projects = projects
	default:
		seen := map[string]bool{}
		for _, pat := range lines(c.str("paths")) {
			matches, err := filepath.Glob(pat)
			if err != nil {
				return shared.NodeResult{Status: "fail", Output: "ungültiges Muster: " + pat}, st
			}
			for _, f := range matches {
				if isRosterArtifact(f) {
					continue // eigene Backups/Zwischendateien nie als Treffer
				}
				fi, err := os.Stat(f)
				if err != nil || !fi.Mode().IsRegular() || seen[f] {
					continue
				}
				if contains != "" {
					data, err := os.ReadFile(f)
					if err != nil || !bytes.Contains(data, []byte(contains)) {
						continue
					}
				}
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	sort.Strings(files)
	st.Files = files
	out := fmt.Sprintf("%d Datei(en)", len(files))
	if len(st.Projects) > 0 {
		var ps []string
		for _, p := range st.Projects {
			ps = append(ps, fmt.Sprintf("%s (%s: %s)", p.Project, p.Dir, strings.Join(p.Services, ", ")))
		}
		out += "\nCompose-Projekte: " + strings.Join(ps, "; ")
	}
	if len(files) == 0 {
		return shared.NodeResult{Status: "empty", Output: "keine passenden Dateien – Gerät nicht betroffen"}, st
	}
	return shared.NodeResult{Status: "ok", Output: out, Files: files}, st
}

// isRosterArtifact erkennt die Backups (<datei>.roster-bak-<lauf>) und Zwischendateien
// (<datei>.roster-tmp), die Abläufe selbst anlegen.
func isRosterArtifact(path string) bool {
	b := filepath.Base(path)
	return strings.Contains(b, ".roster-bak-") || strings.HasSuffix(b, ".roster-tmp")
}

// imageMatches prüft, ob die Image-Referenz eines Containers zum Muster passt: ohne Tag im
// Muster zählt jedes Tag, mit Tag nur genau dieses; „docker.io/“ und „library/“ werden ignoriert.
func imageMatches(ref, pattern string) bool {
	norm := func(s string) string {
		s = strings.TrimPrefix(strings.TrimSpace(s), "docker.io/")
		return strings.TrimPrefix(s, "library/")
	}
	ref, pattern = norm(ref), norm(pattern)
	pr, rr := imageRepo(pattern), imageRepo(ref)
	if pr != pattern { // Muster mit Tag: Tag muss passen
		if imageTag(pattern) != imageTag(ref) {
			return false
		}
	}
	return repoMatches(rr, pr)
}

// repoMatches vergleicht ein Repository mit einem Muster: ganz, nur über den Namen (Muster ohne
// "/", z. B. php-fpm-prod für registry/…/php-fpm-prod) oder mit * und ? als Platzhalter.
func repoMatches(repo, pattern string) bool {
	last := repo
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		last = repo[i+1:]
	}
	if strings.ContainsAny(pattern, "*?") {
		re := "^" + strings.NewReplacer(`\*`, ".*", `\?`, ".").Replace(regexp.QuoteMeta(pattern)) + "$"
		m, err := regexp.MatchString(re, repo)
		if err == nil && !m {
			m, _ = regexp.MatchString(re, last)
		}
		return m
	}
	return repo == pattern || (!strings.Contains(pattern, "/") && last == pattern)
}

// containsAnyRepo prüft, ob die Datei eines der (Repositories der) Images nennt.
func containsAnyRepo(data []byte, images []string) bool {
	for _, im := range images {
		if r := imageRepo(im); r != "" && bytes.Contains(data, []byte(r)) {
			return true
		}
	}
	return false
}

type containerInfo struct {
	Image, Project, Dir, Service string
	Files                        []string
}

// composeProjectsForImage liest alle Container samt Compose-Labels und fasst die passenden
// je Projekt zusammen.
func composeProjectsForImage(ctx context.Context, pattern string) ([]composeProject, error) {
	ids, err := dockerOut(ctx, "ps", "-a", "-q", "--no-trunc")
	if err != nil {
		return nil, fmt.Errorf("docker ps: %v", err)
	}
	idList := strings.Fields(ids)
	if len(idList) == 0 {
		return nil, nil
	}
	args := append([]string{"inspect", "-f", `{{.Config.Image}}|{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}|{{index .Config.Labels "com.docker.compose.project.config_files"}}|{{index .Config.Labels "com.docker.compose.service"}}`}, idList...)
	out, err := dockerOut(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("docker inspect: %v", err)
	}
	return groupProjects(parseInspect(out), pattern), nil
}

// parseInspect liest die Zeilen aus composeProjectsForImage (rein, getestet).
func parseInspect(out string) []containerInfo {
	var list []containerInfo
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(l), "|")
		if len(f) != 5 {
			continue
		}
		ci := containerInfo{Image: f[0], Project: f[1], Dir: f[2], Service: f[4]}
		for _, cf := range strings.Split(f[3], ",") {
			if cf = strings.TrimSpace(cf); cf != "" {
				if !filepath.IsAbs(cf) && ci.Dir != "" {
					cf = filepath.Join(ci.Dir, cf)
				}
				ci.Files = append(ci.Files, cf)
			}
		}
		list = append(list, ci)
	}
	return list
}

func groupProjects(list []containerInfo, pattern string) []composeProject {
	byKey := map[string]*composeProject{}
	var order []string
	for _, ci := range list {
		if ci.Project == "" || ci.Dir == "" || !imageMatches(ci.Image, pattern) {
			continue
		}
		k := ci.Project + "\x00" + ci.Dir
		p := byKey[k]
		if p == nil {
			p = &composeProject{Project: ci.Project, Dir: ci.Dir, Files: ci.Files}
			byKey[k] = p
			order = append(order, k)
		}
		if ci.Service != "" && !contains(p.Services, ci.Service) {
			p.Services = append(p.Services, ci.Service)
		}
		if !contains(p.Images, ci.Image) {
			p.Images = append(p.Images, ci.Image)
		}
	}
	var out []composeProject
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// replace ersetzt Text (literal oder Regex) in den Dateien des Vorgängers bzw. "files".
func (r *runner) replace(c config) (shared.NodeResult, *step) {
	st := &step{}
	files := lines(c.str("files"))
	if len(files) == 0 && r.last != nil {
		files = r.last.Files
		st.Projects = r.last.Projects
	}
	if len(files) == 0 {
		return shared.NodeResult{Status: "fail", Output: "keine Dateien (Baustein „Dateien finden“ davor verbinden oder Pfade angeben)"}, st
	}
	if len(files) > maxReplFiles {
		return shared.NodeResult{Status: "fail", Output: fmt.Sprintf("zu viele Dateien (%d, max. %d)", len(files), maxReplFiles)}, st
	}
	search := c.str("search")
	if search == "" {
		return shared.NodeResult{Status: "fail", Output: "Suchtext fehlt"}, st
	}
	repl := c.str("replace")
	var re *regexp.Regexp
	if c.bool("regex") {
		var err error
		if re, err = regexp.Compile(search); err != nil {
			return shared.NodeResult{Status: "fail", Output: "ungültiger regulärer Ausdruck: " + err.Error()}, st
		}
	}
	filter := c.str("line_filter")
	var diffs, changed, notes []string
	total := 0
	for _, f := range files {
		if isRosterArtifact(f) {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			return shared.NodeResult{Status: "fail", Output: f + ": " + err.Error()}, st
		}
		newData, n := replaceLines(string(data), search, repl, re, filter)
		if n == 0 {
			notes = append(notes, f+": keine Treffer")
			continue
		}
		total += n
		changed = append(changed, f)
		diffs = append(diffs, unifiedDiff(f, string(data), newData))
		if r.opt.DryRun {
			continue
		}
		fi, _ := os.Stat(f)
		if !r.hasBackup(f) { // je Datei und Lauf nur der Originalstand
			bk := f + ".roster-bak-" + r.opt.RunID
			if err := os.WriteFile(bk, data, 0o600); err != nil {
				return shared.NodeResult{Status: "fail", Output: "Backup fehlgeschlagen: " + err.Error()}, st
			}
			r.backups = append(r.backups, backup{Path: f, Backup: bk})
		}
		if err := writeAtomic(f, []byte(newData), fi.Mode().Perm()); err != nil {
			return shared.NodeResult{Status: "fail", Output: f + ": " + err.Error()}, st
		}
	}
	st.Files = changed
	out := fmt.Sprintf("%d Ersetzung(en) in %d Datei(en)", total, len(changed))
	if r.opt.DryRun && total > 0 {
		out += " (Vorschau – nichts geändert)"
	}
	if len(notes) > 0 {
		out += "\n" + strings.Join(notes, "\n")
	}
	if total == 0 {
		switch {
		case c.bool("fail_if_unchanged"):
			return shared.NodeResult{Status: "fail", Output: "keine Treffer\n" + strings.Join(notes, "\n")}, st
		case !c.bool("continue_if_unchanged"):
			// Nichts zu ersetzen = Gerät schon umgestellt: Ablauf ohne Fehler beenden,
			// Folgebausteine (pull/up/Neustart) sind unnötig.
			st.Files = files
			return shared.NodeResult{Status: "empty", Output: "keine Treffer – nichts zu tun, Ablauf endet hier\n" + strings.Join(notes, "\n")}, st
		}
	}
	if len(changed) == 0 { // nichts zu tun: Folgebausteine sollen die Projekte trotzdem kennen
		st.Files = files
	}
	status := "ok"
	if r.opt.DryRun {
		status = "dry"
	}
	return shared.NodeResult{Status: status, Output: out, Diff: strings.Join(diffs, "\n"), Files: changed}, st
}

// replaceLines ersetzt zeilenweise; mit filter nur in Zeilen, die filter enthalten (rein).
func replaceLines(data, search, repl string, re *regexp.Regexp, filter string) (string, int) {
	ls := strings.Split(data, "\n")
	n := 0
	for i, l := range ls {
		if filter != "" && !strings.Contains(l, filter) {
			continue
		}
		var nl string
		if re != nil {
			cnt := len(re.FindAllStringIndex(l, -1))
			if cnt == 0 {
				continue
			}
			nl = re.ReplaceAllString(l, repl)
			n += cnt
		} else {
			cnt := strings.Count(l, search)
			if cnt == 0 {
				continue
			}
			nl = strings.ReplaceAll(l, search, repl)
			n += cnt
		}
		ls[i] = nl
	}
	return strings.Join(ls, "\n"), n
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".roster-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// dockerOut führt die docker-CLI aus (stdout; Fehler mit stderr).
func dockerOut(ctx context.Context, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return string(out), fmt.Errorf("%s", msg)
		}
		return string(out), err
	}
	return string(out), nil
}
