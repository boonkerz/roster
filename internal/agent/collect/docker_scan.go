package collect

import (
	"bytes"
	"context"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/boonkerz/roster/internal/shared"
)

// Container-Sicherheit: Paketlisten von Images (für den OSV-Abgleich) und Datenbank-
// Bestände in DB-Containern (für die Abweichungs-Erkennung). Beides läuft per
// `docker exec` im laufenden Container mit dessen eigener Umgebung – Zugangsdaten
// verlassen den Container nie.

// imageScanScript liest os-release und die Paketliste des gängigen Paketmanagers.
// POSIX-sh (busybox/dash-tauglich); fehlt eine Shell (distroless/scratch), schlägt
// docker exec fehl.
const imageScanScript = `cat /etc/os-release 2>/dev/null || cat /usr/lib/os-release 2>/dev/null; echo '@@PKG@@'
if command -v apk >/dev/null 2>&1; then echo apk; apk info -v 2>/dev/null
elif command -v dpkg-query >/dev/null 2>&1; then echo dpkg; dpkg-query -W -f '${Package} ${Version} ${db:Status-Status}\n' 2>/dev/null
elif command -v rpm >/dev/null 2>&1; then echo rpm; rpm -qa --qf '%{NAME} %|EPOCH?{%{EPOCH}:}|%{VERSION}-%{RELEASE}\n' 2>/dev/null
else echo none; fi`

const maxImagePackages = 3000

// dockerExec führt ein sh-Skript im Container aus (stdout; bei Fehler stderr/Fehlertext).
func dockerExec(ctx context.Context, containerID, script string, timeout time.Duration) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", "exec", containerID, "sh", "-c", script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if ee, ok := err.(*exec.ExitError); ok && (ee.ExitCode() == 126 || ee.ExitCode() == 127) && !strings.Contains(msg, "Authentication") && !strings.Contains(msg, "denied") {
			// 126/127: keine Shell bzw. Befehl nicht ausführbar (distroless/scratch).
			return string(out), errString("executable file not found: " + msg)
		}
		if msg != "" {
			return string(out), errString(msg)
		}
		return string(out), err
	}
	return string(out), nil
}

type errString string

func (e errString) Error() string { return string(e) }

// containerImageIDs liefert je Container-ID die (kurze) Image-ID (eine inspect-Abfrage).
func containerImageIDs(ctx context.Context, ids []string) map[string]string {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"inspect", "-f", "{{.Id}} {{.Image}}"}, ids...)
	out := dockerRun(ctx, args...)
	m := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) == 2 {
			m[shortID(f[0])] = shortID(f[1])
		}
	}
	return m
}

// ScanImages liest die Paketlisten der Images aller laufenden Container, je Image einmal.
// skip(imageID) erlaubt dem Aufrufer, kürzlich gescannte Images auszulassen.
func ScanImages(ctx context.Context, containers []shared.DockerContainer, skip func(imageID string) bool) []shared.ImageScan {
	if !DockerAvailable() {
		return nil
	}
	var running []string
	byID := map[string]shared.DockerContainer{}
	for _, c := range containers {
		if c.State == "running" && c.ContainerID != "" {
			running = append(running, c.ContainerID)
			byID[c.ContainerID] = c
		}
	}
	imgOf := containerImageIDs(ctx, running)
	done := map[string]bool{}
	var out []shared.ImageScan
	for _, cid := range running {
		img := imgOf[cid]
		if img == "" || done[img] || (skip != nil && skip(img)) {
			continue
		}
		done[img] = true
		raw, err := dockerExec(ctx, cid, imageScanScript, 90*time.Second)
		scan := parseImageScan(raw)
		scan.ImageID, scan.Image = img, byID[cid].Image
		if err != nil && scan.Pkg == "" {
			msg := err.Error()
			if strings.Contains(msg, "executable file not found") || strings.Contains(msg, "no such file") {
				msg = "keine Shell im Image (distroless/scratch) – nicht prüfbar"
			}
			scan.Error = truncate(msg, 300)
		}
		out = append(out, scan)
		if ctx.Err() != nil {
			break
		}
	}
	return out
}

var apkVerRe = regexp.MustCompile(`^(.+?)-([0-9][^-]*-r[0-9]+)$`)

// apkSplit trennt "name-1.2.3-r4" in Name und Version (Namen dürfen Bindestriche enthalten).
func apkSplit(s string) (string, string, bool) {
	m := apkVerRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// serviceHints ordnet Paketnamen erkennbaren Netzdiensten zu (Hinweis in der Oberfläche).
var serviceHints = map[string]string{
	"openssh-server": "sshd", "openssh": "sshd", "dropbear": "sshd", "openssh-sftp-server": "sftp",
	"vsftpd": "ftpd", "proftpd": "ftpd", "proftpd-basic": "ftpd", "pure-ftpd": "ftpd",
	"telnetd": "telnetd", "busybox-extras": "telnetd/ftpd (busybox-extras)",
	"samba": "smbd", "redis": "redis", "redis-server": "redis", "memcached": "memcached",
}

// parseImageScan wertet die Ausgabe von imageScanScript aus (rein, getestet).
func parseImageScan(raw string) shared.ImageScan {
	var sc shared.ImageScan
	osPart, pkgPart, _ := strings.Cut(raw, "@@PKG@@")
	for _, l := range strings.Split(osPart, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "ID":
			sc.OS = strings.ToLower(v)
		case "VERSION_ID":
			sc.OSVersion = v
		case "PRETTY_NAME":
			sc.OSName = v
		}
	}
	lines := strings.Split(strings.TrimSpace(pkgPart), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return sc
	}
	sc.Pkg = strings.TrimSpace(lines[0])
	seen := map[string]bool{}
	services := map[string]bool{}
	for _, l := range lines[1:] {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		var name, ver string
		switch sc.Pkg {
		case "apk":
			n, v, ok := apkSplit(l)
			if !ok {
				continue
			}
			name, ver = n, v
		case "dpkg":
			f := strings.Fields(l)
			if len(f) < 2 || (len(f) >= 3 && f[2] != "installed") {
				continue
			}
			name, ver = f[0], f[1]
			if i := strings.Index(name, ":"); i > 0 { // Multiarch-Suffix (libc6:amd64)
				name = name[:i]
			}
		case "rpm":
			f := strings.Fields(l)
			if len(f) < 2 || strings.HasPrefix(f[0], "gpg-pubkey") {
				continue
			}
			name, ver = f[0], f[1]
		default:
			continue
		}
		if name == "" || ver == "" || seen[name] {
			continue
		}
		seen[name] = true
		if h := serviceHints[name]; h != "" {
			services[h] = true
		}
		if len(sc.Packages) < maxImagePackages {
			sc.Packages = append(sc.Packages, shared.ImagePackage{Name: name, Version: ver})
		}
	}
	for h := range services {
		sc.Services = append(sc.Services, h)
	}
	sort.Strings(sc.Services)
	return sc
}

// --- Datenbanken in Containern ---

// dbEngine erkennt die Datenbank-Engine am Image-Namen ("" = kein DB-Container).
func dbEngine(image string) string {
	s := strings.ToLower(image)
	if i := strings.LastIndex(s, "/"); i >= 0 { // Registry/Namespace abschneiden
		s = s[i+1:]
	}
	switch {
	case strings.Contains(s, "mongo-express"), strings.Contains(s, "adminer"), strings.Contains(s, "phpmyadmin"), strings.Contains(s, "pgadmin"), strings.Contains(s, "exporter"), strings.Contains(s, "backup"):
		return ""
	case strings.Contains(s, "mongo"):
		return "mongo"
	case strings.Contains(s, "mariadb"), strings.Contains(s, "mysql"), strings.Contains(s, "percona"):
		return "mysql"
	case strings.Contains(s, "postgres"), strings.Contains(s, "pgsql"), strings.Contains(s, "timescale"):
		return "postgres"
	}
	return ""
}

// dbListScripts liefern je Engine "name<TAB>collections" je Zeile; Zugangsdaten kommen
// aus den Standard-Umgebungsvariablen der offiziellen Images.
var dbListScripts = map[string]string{
	"mongo": `E='db.adminCommand({listDatabases:1}).databases.forEach(function(d){var n=d.name;var c=db.getSiblingDB(n).getCollectionNames().length;print(n+"\t"+c)})'
S=mongosh; command -v mongosh >/dev/null 2>&1 || S=mongo
U="${MONGO_INITDB_ROOT_USERNAME:-}"; P="${MONGO_INITDB_ROOT_PASSWORD:-}"
if [ -n "$U" ]; then "$S" --quiet -u "$U" -p "$P" --authenticationDatabase admin --eval "$E"; else "$S" --quiet --eval "$E"; fi`,
	"mysql": `P="${MARIADB_ROOT_PASSWORD:-${MYSQL_ROOT_PASSWORD:-}}"; C=mysql; command -v mariadb >/dev/null 2>&1 && C=mariadb
Q="SELECT s.schema_name, COUNT(t.table_name) FROM information_schema.schemata s LEFT JOIN information_schema.tables t ON t.table_schema=s.schema_name WHERE s.schema_name NOT IN ('information_schema','performance_schema','mysql','sys') GROUP BY s.schema_name"
if [ -n "$P" ]; then "$C" -uroot -p"$P" -N -B -e "$Q"; else "$C" -uroot -N -B -e "$Q"; fi`,
	"postgres": `U="${POSTGRES_USER:-postgres}"
for d in $(psql -U "$U" -At -c "select datname from pg_database where not datistemplate"); do
  c=$(psql -U "$U" -At -d "$d" -c "select count(*) from information_schema.tables where table_schema not in ('pg_catalog','information_schema')")
  printf '%s\t%s\n' "$d" "$c"
done`,
}

// parseDBList wandelt "name<TAB>count"-Zeilen in Datenbanken um (rein, getestet).
func parseDBList(c shared.DockerContainer, engine, out string) []shared.DockerDatabase {
	var list []shared.DockerDatabase
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		name, cnt, _ := strings.Cut(l, "\t")
		if name == "" || strings.ContainsAny(name, " ") { // Warnzeilen überspringen
			continue
		}
		n, _ := strconv.Atoi(strings.TrimSpace(cnt))
		list = append(list, shared.DockerDatabase{ContainerID: c.ContainerID, Container: c.Name, Engine: engine, Name: name, Collections: n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

var dbCache struct {
	sync.Mutex
	at   time.Time
	list []shared.DockerDatabase
}

const dbCacheTTL = 10 * time.Minute

// DockerDatabases liest den Datenbank-Bestand aller laufenden DB-Container (10 min gecacht,
// damit Checkin und Check sich den Aufruf teilen). Fehlgeschlagene Abfragen erscheinen als
// eine Zeile mit Error je Container.
func DockerDatabases(ctx context.Context, containers []shared.DockerContainer) []shared.DockerDatabase {
	if !DockerAvailable() {
		return nil
	}
	dbCache.Lock()
	defer dbCache.Unlock()
	if time.Since(dbCache.at) < dbCacheTTL {
		return dbCache.list
	}
	var out []shared.DockerDatabase
	for _, c := range containers {
		engine := dbEngine(c.Image)
		if engine == "" || c.State != "running" || c.ContainerID == "" {
			continue
		}
		raw, err := dockerExec(ctx, c.ContainerID, dbListScripts[engine], 20*time.Second)
		list := parseDBList(c, engine, raw)
		if err != nil && len(list) == 0 {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "Authentication failed"), strings.Contains(msg, "Access denied"), strings.Contains(msg, "authentication failed"):
				msg = "Zugriff verweigert – keine passenden Zugangsdaten in der Container-Umgebung"
			case strings.Contains(msg, "executable file not found"), strings.Contains(msg, "not found"):
				msg = "kein Datenbank-Client im Container"
			}
			out = append(out, shared.DockerDatabase{ContainerID: c.ContainerID, Container: c.Name, Engine: engine, Error: truncate(msg, 300)})
			continue
		}
		out = append(out, list...)
	}
	dbCache.at, dbCache.list = time.Now(), out
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
