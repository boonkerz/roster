package fim

import (
	"encoding/hex"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Who-Data: Wer hat eine überwachte Datei geändert? Linux über auditd (Watch-Regeln mit
// Schlüssel roster_fim, ausearch --raw), Windows über SACLs und die Ereignisse 4663/4660.
// Die reinen Parser stehen hier (testbar auf jeder Plattform), die Aufrufe in
// whodata_linux.go / whodata_windows.go.

// Who ist der Verursacher einer Änderung.
type Who struct {
	User    string
	Process string
	Time    time.Time
}

// auditKey ist der Schlüssel der auditd-Regeln des Agents.
const auditKey = "roster_fim"

var (
	reAuditMsg = regexp.MustCompile(`msg=audit\((\d+)\.(\d+):(\d+)\)`)
	reAuditKV  = regexp.MustCompile(`(\w+)=("(?:[^"\\]|\\.)*"|\S+)`)
)

// auditFields zerlegt eine Audit-Zeile in Felder (Werte ohne Anführungszeichen).
func auditFields(line string) map[string]string {
	out := map[string]string{}
	for _, m := range reAuditKV.FindAllStringSubmatch(line, -1) {
		v := m[2]
		if len(v) >= 2 && v[0] == '"' {
			v = v[1 : len(v)-1]
		}
		out[m[1]] = v
	}
	return out
}

// auditName dekodiert einen PATH-Namen: in Anführungszeichen roh, sonst hex-kodiert
// (Namen mit Leer- oder Sonderzeichen), "(null)" leer.
func auditName(v string) string {
	if v == "" || v == "(null)" {
		return ""
	}
	if b, err := hex.DecodeString(v); err == nil && len(v) >= 2 && isHex(v) {
		return string(b)
	}
	return v
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return len(s)%2 == 0
}

// ParseAusearch wertet `ausearch --raw`-Ausgabe aus: je Ereignis SYSCALL (auid, exe,
// comm), CWD und PATH-Einträge (CREATE/DELETE/NORMAL). Liefert den jüngsten Verursacher je
// absolutem Pfad. lookupUID bildet eine UID auf einen Namen ab (nil = numerisch).
func ParseAusearch(raw string, lookupUID func(uid string) string) map[string]Who {
	type ev struct {
		who   Who
		cwd   string
		paths []string
		ok    bool
	}
	events := map[string]*ev{}
	var order []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "----" || !strings.HasPrefix(line, "type=") {
			continue
		}
		m := reAuditMsg.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := m[3]
		e := events[id]
		if e == nil {
			e = &ev{}
			events[id] = e
			order = append(order, id)
			if sec, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				ms, _ := strconv.Atoi(m[2])
				e.who.Time = time.Unix(sec, int64(ms)*int64(time.Millisecond))
			}
		}
		f := auditFields(line)
		switch f["type"] {
		case "SYSCALL":
			if f["key"] != auditKey && !strings.Contains(f["key"], auditKey) {
				continue
			}
			e.ok = f["success"] != "no"
			uid := f["auid"]
			if uid == "" || uid == "4294967295" || uid == "-1" {
				uid = f["uid"]
			}
			name := uid
			if lookupUID != nil {
				if n := lookupUID(uid); n != "" {
					name = n
				}
			}
			e.who.User = name
			proc := auditName(f["exe"])
			if c := auditName(f["comm"]); c != "" && c != path.Base(proc) {
				proc += " (" + c + ")"
			}
			e.who.Process = proc
		case "CWD":
			e.cwd = auditName(f["cwd"])
		case "PATH":
			nt := f["nametype"]
			if nt == "PARENT" || nt == "UNKNOWN" {
				continue
			}
			if n := auditName(f["name"]); n != "" {
				e.paths = append(e.paths, n)
			}
		}
	}
	out := map[string]Who{}
	for _, id := range order {
		e := events[id]
		if !e.ok || e.who.User == "" {
			continue
		}
		for _, p := range e.paths {
			if !path.IsAbs(p) && e.cwd != "" {
				p = path.Join(e.cwd, p)
			}
			p = path.Clean(p)
			if prev, had := out[p]; !had || !e.who.Time.Before(prev.Time) {
				out[p] = e.who
			}
		}
	}
	return out
}

// ParseAuditRules liefert die überwachten Pfade aus `auditctl -l`, die den Roster-Schlüssel tragen.
func ParseAuditRules(out string) map[string]bool {
	paths := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		var p, key string
		for i := 0; i+1 < len(f); i++ {
			switch f[i] {
			case "-w":
				p = f[i+1]
			case "-k":
				key = f[i+1]
			}
		}
		if p != "" && key == auditKey {
			paths[p] = true
		}
	}
	return paths
}

// auditWatchPaths sind die Regelpfade, die eine Watch-Regel bekommen: absolute Datei-/
// Verzeichnispfade ohne Glob und ohne Registry.
func auditWatchPaths(rules []ruleLike) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rules {
		p := r.path()
		if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "*?[") || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

type ruleLike interface{ path() string }

// WinWhoRow ist eine Zeile der Windows-Ereignisabfrage (4663 Objektzugriff, 4660 Löschen).
type WinWhoRow struct {
	ID         int    `json:"Id"`
	Time       string `json:"Time"`
	ObjectName string `json:"ObjectName"`
	Domain     string `json:"SubjectDomainName"`
	User       string `json:"SubjectUserName"`
	Process    string `json:"ProcessName"`
	AccessMask string `json:"AccessMask"`
	HandleID   string `json:"HandleId"`
}

// MapWindowsWho bildet 4663/4660 auf Pfad → Verursacher ab (Pfade kleingeschrieben
// verglichen; 4660 ohne Objektname wird über HandleId dem vorherigen 4663 zugeordnet).
func MapWindowsWho(rows []WinWhoRow) map[string]Who {
	out := map[string]Who{}
	byHandle := map[string]string{}
	for _, r := range rows {
		ts, _ := time.Parse(time.RFC3339Nano, r.Time)
		user := r.User
		if r.Domain != "" && r.Domain != "-" {
			user = r.Domain + `\` + r.User
		}
		obj := r.ObjectName
		if obj == "" && r.ID == 4660 {
			obj = byHandle[r.HandleID]
		}
		if obj == "" || strings.HasSuffix(user, "$") {
			continue
		}
		if r.HandleID != "" {
			byHandle[r.HandleID] = obj
		}
		key := strings.ToLower(obj)
		w := Who{User: user, Process: r.Process, Time: ts}
		if prev, had := out[key]; !had || !ts.Before(prev.Time) {
			out[key] = w
		}
	}
	return out
}
