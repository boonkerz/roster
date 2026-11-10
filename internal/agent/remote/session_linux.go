//go:build linux

package remote

import (
	"bytes"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// guiSession ist eine grafische Sitzung, ermittelt aus den Umgebungen der Prozesse eines
// Nutzers (der Agent läuft als root-Dienst und kennt DISPLAY/WAYLAND_DISPLAY nicht).
type guiSession struct {
	uid        int
	user, home string
	wayland    string // Pfad zur Wayland-Socket
	display    string // X11-Display (:0)
	xauth      string
	procs      int
}

// findGUISessions liefert Sitzungen, die meistbenutzte zuerst.
func findGUISessions() []guiSession {
	byUID := map[int]*guiSession{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid := e.Name()
		if pid[0] < '0' || pid[0] > '9' {
			continue
		}
		uid := procUID(pid)
		if uid < 1000 {
			continue
		}
		env, err := os.ReadFile(filepath.Join("/proc", pid, "environ"))
		if err != nil || len(env) == 0 {
			continue
		}
		vars := parseEnviron(env)
		wd, rt, disp := vars["WAYLAND_DISPLAY"], vars["XDG_RUNTIME_DIR"], vars["DISPLAY"]
		if wd == "" && disp == "" {
			continue
		}
		s := byUID[uid]
		if s == nil {
			s = &guiSession{uid: uid, home: vars["HOME"]}
			if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
				s.user = u.Username
				if s.home == "" {
					s.home = u.HomeDir
				}
			}
			byUID[uid] = s
		}
		s.procs++
		if wd != "" && s.wayland == "" {
			p := wd
			if !filepath.IsAbs(p) {
				if rt == "" {
					rt = "/run/user/" + strconv.Itoa(uid)
				}
				p = filepath.Join(rt, wd)
			}
			if st, err := os.Stat(p); err == nil && st.Mode()&os.ModeSocket != 0 {
				s.wayland = p
			}
		}
		if disp != "" && s.display == "" {
			s.display = disp
		}
		if x := vars["XAUTHORITY"]; x != "" && s.xauth == "" {
			s.xauth = x
		}
	}
	var out []guiSession
	for _, s := range byUID {
		if s.wayland != "" || s.display != "" {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].procs > out[j].procs })
	return out
}

func procUID(pid string) int {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "status"))
	if err != nil {
		return -1
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "Uid:") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				n, _ := strconv.Atoi(f[1])
				return n
			}
		}
	}
	return -1
}

// parseEnviron liest /proc/<pid>/environ (NUL-getrennt, rein, getestet).
func parseEnviron(b []byte) map[string]string {
	m := map[string]string{}
	for _, kv := range bytes.Split(b, []byte{0}) {
		if k, v, ok := strings.Cut(string(kv), "="); ok {
			m[k] = v
		}
	}
	return m
}
