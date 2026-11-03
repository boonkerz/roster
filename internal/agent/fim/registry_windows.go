//go:build windows

package fim

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io/fs"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/boonkerz/roster/internal/shared"
)

// Registry-Überwachung: Schlüssel werden wie Verzeichnisse, Werte wie Dateien geführt.
// Der Hash eines Werts ist SHA-256 über Typ und Rohdaten, der Pfad "HKLM\Key\Wert".
// HKCU gibt es für den Dienst (SYSTEM) nicht sinnvoll → HKU\* (alle geladenen Profile).

var hiveRoots = map[string]registry.Key{"HKLM": registry.LOCAL_MACHINE, "HKU": registry.USERS, "HKCR": registry.CLASSES_ROOT}

// regInfo erfüllt fs.FileInfo für Schlüssel und Werte.
type regInfo struct {
	name string
	size int64
	dir  bool
	mod  time.Time
	hash string
}

func (i regInfo) Name() string { return i.name }
func (i regInfo) Size() int64  { return i.size }
func (i regInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i regInfo) ModTime() time.Time { return i.mod }
func (i regInfo) IsDir() bool        { return i.dir }
func (i regInfo) Sys() any           { return regValue{hash: i.hash} }

// regValue transportiert den fertigen Hash eines Werts zum Scan (statt hashFile).
type regValue struct{ hash string }

func walkRegistry(r shared.FIMRule, visit func(r shared.FIMRule, p string, info fs.FileInfo) bool) {
	hive, sub := shared.SplitRegistryPath(r.Path)
	if hive == "HKCU" {
		hive, sub = "HKU", `*\`+sub
	}
	root, ok := hiveRoots[hive]
	if !ok {
		return
	}
	for _, s := range expandUsers(root, sub) {
		if !walkKey(r, root, hive, s, visit, r.Recursive) {
			return
		}
	}
}

// expandUsers ersetzt ein führendes "*" (HKU\*) durch die geladenen Profil-SIDs.
func expandUsers(root registry.Key, sub string) []string {
	if !strings.HasPrefix(sub, `*`) {
		return []string{sub}
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(sub, `*`), `\`)
	k, err := registry.OpenKey(root, "", registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, _ := k.ReadSubKeyNames(-1)
	var out []string
	for _, n := range names {
		if strings.HasSuffix(n, "_Classes") {
			continue
		}
		if rest == "" {
			out = append(out, n)
		} else {
			out = append(out, n+`\`+rest)
		}
	}
	return out
}

// walkKey besucht einen Schlüssel: Schlüssel-Eintrag, Werte, Unterschlüssel (rekursiv oder
// nur als Einträge, damit neue Unterschlüssel – z. B. neue Dienste – auffallen).
func walkKey(r shared.FIMRule, root registry.Key, hive, sub string, visit func(r shared.FIMRule, p string, info fs.FileInfo) bool, recurse bool) bool {
	k, err := registry.OpenKey(root, sub, registry.READ|registry.WOW64_64KEY)
	if err != nil {
		return true
	}
	defer k.Close()
	keyPath := hive + `\` + sub
	if excluded(r, keyPath) {
		return true
	}
	mod := time.Time{}
	if ki, err := k.Stat(); err == nil {
		mod = ki.ModTime()
	}
	if !visit(r, keyPath, regInfo{name: lastSeg(sub), dir: true, mod: mod}) {
		return false
	}
	names, _ := k.ReadValueNames(-1)
	for _, name := range names {
		n, typ, err := k.GetValue(name, nil)
		if err != nil && n <= 0 {
			continue
		}
		buf := make([]byte, n)
		if n > 0 {
			if _, _, err := k.GetValue(name, buf); err != nil {
				continue
			}
		}
		display := name
		if display == "" {
			display = "(Default)"
		}
		p := keyPath + `\` + display
		if excluded(r, p) {
			continue
		}
		h := sha256.New()
		var tb [4]byte
		binary.LittleEndian.PutUint32(tb[:], typ)
		h.Write(tb[:])
		h.Write(buf[:n])
		if !visit(r, p, regInfo{name: display, size: int64(n), mod: mod, hash: hex.EncodeToString(h.Sum(nil))}) {
			return false
		}
	}
	subs, _ := k.ReadSubKeyNames(-1)
	for _, s := range subs {
		child := sub + `\` + s
		if recurse {
			if !walkKey(r, root, hive, child, visit, true) {
				return false
			}
			continue
		}
		cp := hive + `\` + child
		if excluded(r, cp) {
			continue
		}
		cmod := time.Time{}
		if ck, err := registry.OpenKey(root, child, registry.QUERY_VALUE|registry.WOW64_64KEY); err == nil {
			if ki, err := ck.Stat(); err == nil {
				cmod = ki.ModTime()
			}
			ck.Close()
		}
		if !visit(r, cp, regInfo{name: s, dir: true, mod: cmod}) {
			return false
		}
	}
	return true
}

func lastSeg(sub string) string {
	if i := strings.LastIndexByte(sub, '\\'); i >= 0 {
		return sub[i+1:]
	}
	return sub
}
