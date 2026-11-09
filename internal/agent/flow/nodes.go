package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// --- Docker Compose ---

// composeTargets ermittelt die Projekte: explizites Verzeichnis (dir, optional files), die
// Projekte des letzten Schritts mit Compose-Infos oder – wenn „Dateien finden“ per Pfad lief –
// die Compose-Dateien unter dessen Treffern.
func (r *runner) composeTargets(ctx context.Context, c config) []composeProject {
	if dir := strings.TrimSpace(c.str("dir")); dir != "" {
		p := composeProject{Dir: dir}
		for _, f := range lines(c.str("compose_files")) {
			if !filepath.IsAbs(f) {
				f = filepath.Join(dir, f)
			}
			p.Files = append(p.Files, f)
		}
		return []composeProject{p}
	}
	if r.last != nil {
		if len(r.last.Projects) > 0 {
			return r.last.Projects
		}
		return projectsFromFiles(r.last.Files, composeLabels(ctx))
	}
	return nil
}

// isComposeFile erkennt Compose-Dateien am Namen (docker-compose*.yml, compose*.yaml …).
func isComposeFile(path string) bool {
	b := strings.ToLower(filepath.Base(path))
	if !(strings.HasSuffix(b, ".yml") || strings.HasSuffix(b, ".yaml")) {
		return false
	}
	return strings.HasPrefix(b, "docker-compose") || strings.HasPrefix(b, "compose")
}

// composeLabels liest die Compose-Labels aller Container (Fehler = keine Infos).
func composeLabels(ctx context.Context) []containerInfo {
	ids, err := dockerOut(ctx, "ps", "-a", "-q", "--no-trunc")
	if err != nil || strings.TrimSpace(ids) == "" {
		return nil
	}
	args := append([]string{"inspect", "-f", `{{.Config.Image}}|{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}|{{index .Config.Labels "com.docker.compose.project.config_files"}}|{{index .Config.Labels "com.docker.compose.service"}}`}, strings.Fields(ids)...)
	out, err := dockerOut(ctx, args...)
	if err != nil {
		return nil
	}
	return parseInspect(out)
}

// projectsFromFiles bildet Projekte aus Compose-Dateien: je Verzeichnis ein Projekt; Name aus
// den Labels laufender Container dieses Verzeichnisses (sonst Compose-Standard = Ordnername).
// Dienste bleiben leer, d. h. pull/up betrifft das ganze Projekt (rein, getestet).
func projectsFromFiles(files []string, labels []containerInfo) []composeProject {
	byDir := map[string]*composeProject{}
	var order []string
	for _, f := range files {
		if !isComposeFile(f) {
			continue
		}
		dir := filepath.Dir(f)
		p := byDir[dir]
		if p == nil {
			p = &composeProject{Dir: dir}
			for _, ci := range labels {
				if filepath.Clean(ci.Dir) == filepath.Clean(dir) && ci.Project != "" {
					p.Project = ci.Project
					break
				}
			}
			byDir[dir] = p
			order = append(order, dir)
		}
		p.Files = append(p.Files, f)
	}
	var out []composeProject
	for _, d := range order {
		out = append(out, *byDir[d])
	}
	return out
}

func (r *runner) compose(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	action := c.str("action")
	switch action {
	case "pull", "up", "restart", "down":
	default:
		return shared.NodeResult{Status: "fail", Output: "Aktion muss pull, up, restart oder down sein"}, st
	}
	projects := r.composeTargets(ctx, c)
	if len(projects) == 0 {
		return shared.NodeResult{Status: "fail", Output: "kein Compose-Projekt bekannt – im Baustein ein Verzeichnis angeben oder davor „Dateien finden“ im Modus Compose-Image bzw. mit einer docker-compose.yml/compose.yaml als Treffer"}, st
	}
	if r.last != nil {
		st.Files, st.Projects = r.last.Files, projects
	} else {
		st.Projects = projects
	}
	services := lines(strings.ReplaceAll(c.str("services"), ",", "\n"))
	allServices := c.bool("all_services")
	var outs []string
	for _, p := range projects {
		args := composeArgs(p, action, services, allServices)
		line := "docker compose " + strings.Join(args, " ")
		if r.opt.DryRun {
			outs = append(outs, "würde ausführen: "+line)
			continue
		}
		code, out := runCompose(ctx, p.Dir, args)
		outs = append(outs, "$ "+line+"\n"+strings.TrimSpace(out))
		if code != 0 {
			return shared.NodeResult{Status: "fail", Output: strings.Join(outs, "\n\n")}, st
		}
	}
	status := "ok"
	if r.opt.DryRun {
		status = "dry"
	}
	return shared.NodeResult{Status: status, Output: strings.Join(outs, "\n\n")}, st
}

// composeArgs baut die Argumente für docker compose (rein, getestet).
func composeArgs(p composeProject, action string, services []string, all bool) []string {
	var args []string
	if p.Project != "" {
		args = append(args, "-p", p.Project)
	}
	for _, f := range p.Files {
		args = append(args, "-f", f)
	}
	if p.Dir != "" {
		args = append(args, "--project-directory", p.Dir)
	}
	switch action {
	case "up":
		args = append(args, "up", "-d")
	default:
		args = append(args, action)
	}
	if action == "down" {
		return args
	}
	svc := services
	if len(svc) == 0 && !all {
		svc = p.Services
	}
	return append(args, svc...)
}

// runCompose nutzt „docker compose“ (v2) und fällt auf docker-compose (v1) zurück.
func runCompose(ctx context.Context, dir string, args []string) (int, string) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	try := func(name string, a ...string) (int, string, bool) {
		if _, err := exec.LookPath(name); err != nil {
			return 0, "", false
		}
		cmd := exec.CommandContext(cctx, name, a...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return ee.ExitCode(), string(out), true
			}
			return 1, string(out) + err.Error(), true
		}
		return 0, string(out), true
	}
	if _, err := exec.Command("docker", "compose", "version").Output(); err == nil {
		code, out, _ := try("docker", append([]string{"compose"}, args...)...)
		return code, out
	}
	if code, out, ok := try("docker-compose", args...); ok {
		return code, out
	}
	return 1, "weder „docker compose“ noch docker-compose gefunden"
}

// --- Befehl ---

func (r *runner) command(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	script := c.str("script")
	if strings.TrimSpace(script) == "" {
		return shared.NodeResult{Status: "fail", Output: "Befehl fehlt"}, st
	}
	shell := c.str("shell")
	if r.opt.DryRun {
		return shared.NodeResult{Status: "dry", Output: "würde ausführen (" + orDefault(shell, "sh") + "):\n" + script}, st
	}
	if r.opt.RunScript == nil {
		return shared.NodeResult{Status: "fail", Output: "Skriptausführung nicht verfügbar"}, st
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(c.num("timeout_s", 600))*time.Second)
	defer cancel()
	code, out := r.opt.RunScript(tctx, shell, script)
	st.Out = out
	st.Value = strconv.Itoa(code)
	if code != 0 {
		return shared.NodeResult{Status: "fail", Output: fmt.Sprintf("Exit %d\n%s", code, out)}, st
	}
	return shared.NodeResult{Status: "ok", Output: out}, st
}

// --- HTTP / API ---

func (r *runner) httpNode(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	method := strings.ToUpper(orDefault(c.str("method"), "GET"))
	url := strings.TrimSpace(c.str("url"))
	if url == "" {
		return shared.NodeResult{Status: "fail", Output: "URL fehlt"}, st
	}
	if r.opt.DryRun && method != "GET" && method != "HEAD" {
		return shared.NodeResult{Status: "dry", Output: "würde " + method + " " + url + " senden (in der Vorschau nur GET/HEAD)"}, st
	}
	code, body, err := doHTTP(ctx, method, url, c.str("headers"), c.str("body"), c.bool("insecure"), time.Duration(c.num("timeout_s", 30))*time.Second)
	if err != nil {
		return shared.NodeResult{Status: "fail", Output: err.Error()}, st
	}
	st.Out = body
	var js any
	if json.Unmarshal([]byte(body), &js) == nil {
		st.JSON = js
	}
	out := fmt.Sprintf("HTTP %d\n%s", code, clip(body, 2000))
	if p := strings.TrimSpace(c.str("json_path")); p != "" {
		st.Value = jsonString(jsonPath(st.JSON, p))
		out = fmt.Sprintf("HTTP %d · %s = %q\n%s", code, p, st.Value, clip(body, 1500))
		if st.Value == "" && c.bool("require_value") {
			return shared.NodeResult{Status: "fail", Output: out + "\n(kein Wert unter " + p + ")"}, st
		}
	}
	if !statusOK(code, c.str("expect_status")) {
		return shared.NodeResult{Status: "fail", Output: out}, st
	}
	return shared.NodeResult{Status: "ok", Output: out}, st
}

func doHTTP(ctx context.Context, method, url, headers, body string, insecure bool, timeout time.Duration) (int, string, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return 0, "", err
	}
	for _, h := range lines(headers) {
		if k, v, ok := strings.Cut(h, ":"); ok {
			req.Header.Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	cl := &http.Client{Timeout: timeout}
	if insecure {
		cl.Transport = insecureTransport()
	}
	resp, err := cl.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b), nil
}

// statusOK prüft den HTTP-Status gegen "200", "200,301" oder "2xx" (leer = 2xx).
func statusOK(code int, expect string) bool {
	expect = strings.TrimSpace(expect)
	if expect == "" {
		expect = "2xx"
	}
	for _, e := range strings.Split(expect, ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if len(e) == 3 && strings.HasSuffix(e, "xx") && e[0] >= '1' && e[0] <= '5' {
			if code/100 == int(e[0]-'0') {
				return true
			}
			continue
		}
		if n, err := strconv.Atoi(e); err == nil && n == code {
			return true
		}
	}
	return false
}

// --- Prüfen ---

func (r *runner) check(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	kind := c.str("kind")
	if r.opt.DryRun && kind != "expr" && kind != "file_contains" && kind != "exists" {
		return shared.NodeResult{Status: "dry", Output: "Prüfung „" + kind + "“ läuft erst bei der Ausführung"}, st
	}
	retries := int(c.num("retries", 0))
	interval := time.Duration(c.num("interval_s", 5)) * time.Second
	var ok bool
	var msg string
	for attempt := 0; ; attempt++ {
		ok, msg = r.checkOnce(ctx, kind, c)
		if ok || attempt >= retries || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
	}
	st.Value = strconv.FormatBool(ok)
	if !ok && c.bool("false_ends") {
		// „Nein → nichts tun“: Ablauf ohne Fehler beenden (Gerät nicht betroffen).
		return shared.NodeResult{Status: "empty", Output: msg + "\nBedingung nicht erfüllt – Ablauf endet hier ohne Änderung"}, st
	}
	if !ok {
		return shared.NodeResult{Status: "fail", Output: msg}, st
	}
	return shared.NodeResult{Status: "ok", Output: msg}, st
}

func (r *runner) checkOnce(ctx context.Context, kind string, c config) (bool, string) {
	switch kind {
	case "http":
		code, _, err := doHTTP(ctx, "GET", c.str("url"), c.str("headers"), "", c.bool("insecure"), 15*time.Second)
		if err != nil {
			return false, err.Error()
		}
		return statusOK(code, c.str("expect_status")), fmt.Sprintf("HTTP %d", code)
	case "tcp":
		addr := net.JoinHostPort(orDefault(c.str("host"), "127.0.0.1"), c.str("port"))
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			return false, addr + ": " + err.Error()
		}
		conn.Close()
		return true, addr + " erreichbar"
	case "container":
		name := strings.TrimSpace(c.str("container"))
		if name == "" && r.last != nil && len(r.last.Projects) > 0 {
			// Ohne Namen: die betroffenen Dienste der Compose-Projekte prüfen.
			var msgs []string
			for _, p := range r.last.Projects {
				args := []string{"ps", "-a", "--format", "{{.Names}}|{{.State}}|{{.Status}}", "--filter", "label=com.docker.compose.project=" + p.Project}
				out, err := dockerOut(ctx, args...)
				if err != nil {
					return false, err.Error()
				}
				for _, svc := range orAll(p.Services) {
					filtered := out
					if svc != "" {
						filtered = filterServiceLines(ctx, p.Project, svc)
					}
					ok, msg := containerState(filtered, "", c.bool("healthy"))
					msgs = append(msgs, p.Project+"/"+orDefault(svc, "*")+": "+msg)
					if !ok {
						return false, strings.Join(msgs, "\n")
					}
				}
			}
			return true, strings.Join(msgs, "\n")
		}
		if name == "" {
			return false, "Container-Name fehlt (oder „Dateien finden“ im Modus Compose-Image davor)"
		}
		out, err := dockerOut(ctx, "ps", "-a", "--format", "{{.Names}}|{{.State}}|{{.Status}}")
		if err != nil {
			return false, err.Error()
		}
		return containerState(out, name, c.bool("healthy"))
	case "exists":
		p := c.str("path")
		fi, err := os.Stat(p)
		if err != nil {
			return false, p + " fehlt"
		}
		switch c.str("path_type") {
		case "dir":
			if !fi.IsDir() {
				return false, p + " ist kein Verzeichnis"
			}
		case "file":
			if !fi.Mode().IsRegular() {
				return false, p + " ist keine Datei"
			}
		}
		return true, p + " vorhanden"
	case "file_contains":
		data, err := os.ReadFile(c.str("path"))
		if err != nil {
			return false, err.Error()
		}
		if bytes.Contains(data, []byte(c.str("text"))) {
			return true, "Text gefunden"
		}
		return false, "Text nicht gefunden"
	case "expr":
		return compare(c.str("value"), c.str("op"), c.str("expect"))
	}
	return false, "unbekannte Prüfung: " + kind
}

// containerState wertet docker ps aus: alle Container, deren Name name enthält, müssen laufen
// (healthy: und dürfen nicht unhealthy/starting sein) (rein, getestet).
func containerState(out, name string, healthy bool) (bool, string) {
	found := 0
	for _, l := range strings.Split(out, "\n") {
		f := strings.SplitN(strings.TrimSpace(l), "|", 3)
		if len(f) != 3 || !strings.Contains(f[0], name) {
			continue
		}
		found++
		if f[1] != "running" {
			return false, f[0] + ": " + f[2]
		}
		if healthy && (strings.Contains(f[2], "unhealthy") || strings.Contains(f[2], "health: starting")) {
			return false, f[0] + ": " + f[2]
		}
	}
	if found == 0 {
		return false, "kein Container mit „" + name + "“ im Namen"
	}
	return true, fmt.Sprintf("%d Container laufen", found)
}

// compare wertet value op expect aus (eq, ne, contains, gt, lt, empty, notempty).
func compare(value, op, expect string) (bool, string) {
	msg := fmt.Sprintf("%q %s %q", value, op, expect)
	switch op {
	case "", "eq":
		return value == expect, msg
	case "ne":
		return value != expect, msg
	case "contains":
		return strings.Contains(value, expect), msg
	case "empty":
		return strings.TrimSpace(value) == "", msg
	case "notempty":
		return strings.TrimSpace(value) != "", msg
	case "version_lt", "version_le", "version_gt", "version_ge", "version_eq":
		c, ok := compareVersions(value, expect)
		if !ok {
			return false, msg + " (keine vergleichbare Version)"
		}
		switch op {
		case "version_lt":
			return c < 0, msg
		case "version_le":
			return c <= 0, msg
		case "version_gt":
			return c > 0, msg
		case "version_ge":
			return c >= 0, msg
		}
		return c == 0, msg
	case "gt", "lt":
		a, err1 := strconv.ParseFloat(strings.TrimSpace(value), 64)
		b, err2 := strconv.ParseFloat(strings.TrimSpace(expect), 64)
		if err1 != nil || err2 != nil {
			return false, msg + " (keine Zahl)"
		}
		if op == "gt" {
			return a > b, msg
		}
		return a < b, msg
	}
	return false, "unbekannter Vergleich " + op
}

// --- Zurückrollen / Warten ---

func (r *runner) rollbackNode(ctx context.Context, c config) (shared.NodeResult, *step) {
	st := &step{}
	if r.opt.DryRun {
		return shared.NodeResult{Status: "dry", Output: "würde alle in diesem Lauf geänderten Dateien wiederherstellen"}, st
	}
	restored := r.rollback()
	out := fmt.Sprintf("%d Änderung(en) zurückgenommen", len(restored))
	if len(restored) > 0 {
		out += ":\n" + strings.Join(restored, "\n")
	}
	if c.bool("compose_up") {
		// Ganzes Projekt hochfahren: auch Dienste, deren Image der Ablauf geändert hat, die aber
		// nicht zu den gefundenen gehörten (z. B. web/cron neben php-fpm), müssen zurück.
		for _, p := range r.composeTargets(ctx, config{}) {
			args := composeArgs(p, "up", nil, true)
			code, o := runCompose(ctx, p.Dir, args)
			out += "\n$ docker compose " + strings.Join(args, " ") + "\n" + strings.TrimSpace(o)
			if code != 0 {
				return shared.NodeResult{Status: "fail", Output: out, Files: restored}, st
			}
		}
	}
	return shared.NodeResult{Status: "ok", Output: out, Files: restored}, st
}

func (r *runner) wait(ctx context.Context, c config) (shared.NodeResult, *step) {
	d := time.Duration(c.num("seconds", 10)) * time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	if r.opt.DryRun {
		return shared.NodeResult{Status: "dry", Output: "würde " + d.String() + " warten"}, &step{}
	}
	select {
	case <-ctx.Done():
		return shared.NodeResult{Status: "fail", Output: "abgebrochen"}, &step{}
	case <-time.After(d):
	}
	return shared.NodeResult{Status: "ok", Output: d.String() + " gewartet"}, &step{}
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func orAll(list []string) []string {
	if len(list) == 0 {
		return []string{""}
	}
	return list
}

// filterServiceLines liefert die docker-ps-Zeilen eines Compose-Dienstes.
func filterServiceLines(ctx context.Context, project, service string) string {
	out, _ := dockerOut(ctx, "ps", "-a", "--format", "{{.Names}}|{{.State}}|{{.Status}}",
		"--filter", "label=com.docker.compose.project="+project, "--filter", "label=com.docker.compose.service="+service)
	return out
}
