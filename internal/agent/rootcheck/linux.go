//go:build linux

package rootcheck

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// knownRootkitPaths: Marker bekannter Linux-Rootkits (Reptile, Diamorphine, Jynx, BEURK, …).
var knownRootkitPaths = []string{
	"/reptile", "/reptile/*", "/usr/share/.diamorphine", "/lib/modules/*/kernel/drivers/diamorphine.ko",
	"/usr/lib/libncom.so*", "/etc/ld.so.hash", "/dev/.lib", "/dev/.static", "/usr/share/.zk", "/usr/lib/.ark",
	"/tmp/.font-unix/.x", "/var/lib/.libnet.so", "/lib/libselinux.so.3.*", "/usr/local/lib/libbeurk.so", "/dev/.ptmx",
	"/usr/include/gpm2.h", "/usr/bin/.etc", "/etc/rc.d/rc.sysinit.bak", "/dev/shm/.x", "/dev/shm/.kernel",
}

func run(ctx context.Context) []Finding {
	var out []Finding
	out = append(out, ldPreload()...)
	out = append(out, hiddenModules()...)
	out = append(out, promiscuous()...)
	out = append(out, knownPaths()...)
	out = append(out, devRegularFiles()...)
	out = append(out, tmpExecutables(ctx)...)
	out = append(out, hiddenPIDs(ctx)...)
	return out
}

func ldPreload() []Finding {
	b, err := os.ReadFile("/etc/ld.so.preload")
	if err != nil {
		return nil
	}
	var libs []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			libs = append(libs, l)
		}
	}
	if len(libs) == 0 {
		return nil
	}
	return []Finding{{Check: "ld.so.preload gesetzt", Item: strings.Join(libs, ", ")}}
}

// hiddenModules: Module mit /sys/module/<x>/initstate (geladen), die /proc/modules nicht nennt.
func hiddenModules() []Finding {
	b, err := os.ReadFile("/proc/modules")
	if err != nil {
		return nil
	}
	listed := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) > 0 {
			listed[f[0]] = true
		}
	}
	entries, err := os.ReadDir("/sys/module")
	if err != nil {
		return nil
	}
	var out []Finding
	for _, e := range entries {
		name := e.Name()
		if listed[name] {
			continue
		}
		if st, err := os.ReadFile("/sys/module/" + name + "/initstate"); err == nil && strings.TrimSpace(string(st)) == "live" {
			// Eingebaute Module haben kein initstate; "live" ohne /proc/modules-Eintrag ist versteckt.
			if _, err := os.Stat("/sys/module/" + name + "/refcnt"); err == nil {
				out = append(out, Finding{Check: "verstecktes Kernelmodul", Item: name})
			}
		}
	}
	return out
}

func promiscuous() []Finding {
	ifs, _ := filepath.Glob("/sys/class/net/*/flags")
	var out []Finding
	for _, f := range ifs {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(string(b)), "0x"), 16, 64)
		if err == nil && v&0x100 != 0 {
			out = append(out, Finding{Check: "Netzwerkschnittstelle im Promiscuous-Modus", Item: filepath.Base(filepath.Dir(f))})
		}
	}
	return out
}

func knownPaths() []Finding {
	var out []Finding
	seen := map[string]bool{}
	for _, pat := range knownRootkitPaths {
		m, _ := filepath.Glob(pat)
		for _, p := range m {
			if !seen[p] {
				seen[p] = true
				out = append(out, Finding{Check: "bekannter Rootkit-Pfad", Item: p})
			}
		}
	}
	return out
}

// devRegularFiles: reguläre Dateien direkt in /dev (dort gehören nur Geräteknoten hin).
func devRegularFiles() []Finding {
	skip := map[string]bool{"core": true, "initctl": true, "log": true, "MAKEDEV": true}
	entries, err := os.ReadDir("/dev")
	if err != nil {
		return nil
	}
	var out []Finding
	for _, e := range entries {
		if !e.Type().IsRegular() || skip[e.Name()] {
			continue
		}
		out = append(out, Finding{Check: "reguläre Datei in /dev", Item: "/dev/" + e.Name()})
	}
	return out
}

// tmpExecutables: ausführbare, setuid/setgid- oder ELF-Dateien in temporären Verzeichnissen.
func tmpExecutables(ctx context.Context) []Finding {
	var out []Finding
	count := 0
	for _, root := range []string{"/tmp", "/var/tmp", "/dev/shm"} {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			if count++; count > 20000 {
				return filepath.SkipAll
			}
			if d.IsDir() {
				if strings.Count(strings.TrimPrefix(p, root), "/") > 4 {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			mode := info.Mode()
			why := ""
			switch {
			case mode&(fs.ModeSetuid|fs.ModeSetgid) != 0:
				why = "setuid/setgid in Temp-Verzeichnis"
			case mode&0o111 != 0:
				why = "ausführbare Datei in Temp-Verzeichnis"
			case info.Size() >= 4 && isELF(p):
				why = "ELF-Binary in Temp-Verzeichnis"
			}
			if why != "" {
				out = append(out, Finding{Check: why, Item: p})
			}
			return nil
		})
	}
	return out
}

func isELF(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if n, _ := f.Read(magic[:]); n < 4 {
		return false
	}
	return string(magic[:]) == "\x7fELF"
}

// hiddenPIDs: Prozesse, die auf kill(pid,0) antworten (existieren), aber in /proc fehlen.
// Threads sind unter /proc/<tid> ebenfalls stat-bar, daher keine Fehlalarme durch TIDs.
func hiddenPIDs(ctx context.Context) []Finding {
	maxPID := int64(1 << 16)
	if b, err := os.ReadFile("/proc/sys/kernel/pid_max"); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
			maxPID = v
		}
	}
	if maxPID > 1<<22 {
		maxPID = 1 << 22
	}
	var suspects []int
	for pid := 2; int64(pid) <= maxPID; pid++ {
		if pid%4096 == 0 && ctx.Err() != nil {
			break
		}
		err := syscall.Kill(pid, 0)
		if err != nil && err != syscall.EPERM {
			continue // ESRCH: gibt es nicht
		}
		if _, err := os.Lstat("/proc/" + strconv.Itoa(pid)); err != nil {
			suspects = append(suspects, pid)
		}
	}
	if len(suspects) == 0 {
		return nil
	}
	// Kurzlebige Prozesse ausschließen: nach 200 ms erneut prüfen.
	time.Sleep(200 * time.Millisecond)
	var out []Finding
	for _, pid := range suspects {
		if err := syscall.Kill(pid, 0); err != nil && err != syscall.EPERM {
			continue
		}
		if _, err := os.Lstat("/proc/" + strconv.Itoa(pid)); err != nil {
			out = append(out, Finding{Check: "versteckter Prozess", Item: fmt.Sprintf("PID %d", pid)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Item < out[j].Item })
	return out
}
