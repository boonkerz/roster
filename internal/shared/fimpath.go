package shared

import (
	"regexp"
	"strings"
)

// Registry-Pfade der Datei-Integritätsprüfung (Windows): "HKLM\SOFTWARE\…". Agent (Walker)
// und Server (Validierung) teilen sich Erkennung und Normalisierung.

var reRegistryPath = regexp.MustCompile(`^(?i)(HKLM|HKCU|HKU|HKCR|HKEY_LOCAL_MACHINE|HKEY_CURRENT_USER|HKEY_USERS|HKEY_CLASSES_ROOT)[\\/]`)

var registryHives = map[string]string{
	"HKEY_LOCAL_MACHINE": "HKLM", "HKEY_CURRENT_USER": "HKCU", "HKEY_USERS": "HKU", "HKEY_CLASSES_ROOT": "HKCR",
	"HKLM": "HKLM", "HKCU": "HKCU", "HKU": "HKU", "HKCR": "HKCR",
}

// IsRegistryPath meldet, ob p ein Registry-Pfad ist (Hive-Präfix mit Backslash oder Slash).
func IsRegistryPath(p string) bool { return reRegistryPath.MatchString(strings.TrimSpace(p)) }

// NormalizeRegistryPath vereinheitlicht Hive-Kurzname, Backslashes und entfernt
// abschließende Trenner: "hkey_local_machine/Software\Foo\" → "HKLM\Software\Foo".
func NormalizeRegistryPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "/", `\`))
	hive, rest, _ := strings.Cut(p, `\`)
	short, ok := registryHives[strings.ToUpper(hive)]
	if !ok {
		return p
	}
	rest = strings.Trim(rest, `\`)
	if rest == "" {
		return short
	}
	return short + `\` + rest
}

// SplitRegistryPath liefert Hive-Kurzname und Unterschlüssel eines normalisierten Pfads.
func SplitRegistryPath(p string) (hive, sub string) {
	hive, sub, _ = strings.Cut(NormalizeRegistryPath(p), `\`)
	return hive, sub
}
