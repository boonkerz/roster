package statemon

import (
	"encoding/csv"
	"strings"
)

// Reine Parser (plattformunabhängig testbar).

// ParseCron liefert die wirksamen Zeilen einer Cron-Datei als "datei: zeile".
func ParseCron(file, content string) []string {
	var out []string
	for _, l := range strings.Split(content, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		out = append(out, file+": "+l)
	}
	return out
}

// ParsePasswdUID0 liefert die Konten mit UID 0 aus /etc/passwd.
func ParsePasswdUID0(content string) []string {
	var out []string
	for _, l := range strings.Split(content, "\n") {
		f := strings.Split(l, ":")
		if len(f) >= 3 && f[2] == "0" {
			out = append(out, f[0])
		}
	}
	return out
}

// ParseGroupMembers liefert "gruppe: mitglied" für die Admin-Gruppen aus /etc/group; mit
// passwd werden Konten ergänzt, deren primäre Gruppe GID 0 ist.
func ParseGroupMembers(group, passwd string, admins map[string]bool) []string {
	var out []string
	for _, l := range strings.Split(group, "\n") {
		f := strings.Split(l, ":")
		if len(f) < 4 || !admins[f[0]] {
			continue
		}
		for _, m := range strings.Split(f[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				out = append(out, f[0]+": "+m)
			}
		}
	}
	for _, l := range strings.Split(passwd, "\n") {
		f := strings.Split(l, ":")
		if len(f) >= 4 && f[3] == "0" && f[0] != "root" {
			out = append(out, "root: "+f[0]+" (primäre Gruppe)")
		}
	}
	return out
}

// ParseNetLocalgroup liefert die Mitglieder aus `net localgroup <Gruppe>` (DE/EN): die
// Zeilen zwischen der Trennlinie und „The command completed“/„Der Befehl wurde“.
func ParseNetLocalgroup(out string) []string {
	var members []string
	in := false
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimRight(l, "\r ")
		if strings.HasPrefix(l, "----") {
			in = true
			continue
		}
		if !in {
			continue
		}
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "The command") || strings.HasPrefix(t, "Der Befehl") {
			if t != "" {
				break
			}
			continue
		}
		members = append(members, t)
	}
	return members
}

// ParseSchtasksCSV liefert die Aufgabennamen aus `schtasks /query /fo csv /nh` (erste Spalte).
func ParseSchtasksCSV(out string) []string {
	r := csv.NewReader(strings.NewReader(out))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, _ := r.ReadAll()
	var names []string
	for _, rec := range recs {
		if len(rec) == 0 {
			continue
		}
		n := strings.TrimSpace(rec[0])
		if n == "" || strings.EqualFold(n, "TaskName") || strings.EqualFold(n, "Aufgabenname") {
			continue
		}
		names = append(names, n)
	}
	return names
}

// ParseUnitFiles liefert die Unit-Namen aus `systemctl list-unit-files --no-legend --plain`.
func ParseUnitFiles(out string) []string {
	var names []string
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 1 && strings.Contains(f[0], ".") {
			names = append(names, f[0])
		}
	}
	return names
}

// ParseLaunchctl liefert die Labels aus `launchctl list` (dritte Spalte).
func ParseLaunchctl(out string) []string {
	var names []string
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) >= 3 && f[2] != "Label" {
			names = append(names, f[2])
		}
	}
	return names
}
