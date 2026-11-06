// Package persist macht die Agent-Installation upgrade-fest. Auf OPNsense registriert sich
// der Agent als Plugin „os-roster-agent“: lokales FreeBSD-Paket (mit dem pkg der Box selbst
// gebaut), lokales Paket-Repo unter /var/lib/roster/pkg (überlebt Upgrades) und Eintrag in
// config.xml (system.firmware.plugins). Fehlen Dateien nach einem Firmware-Upgrade, zeigt
// OPNsense das Plugin unter System → Firmware → Plugins als fehlend und installiert es mit
// einem Klick („fehlende Plugins installieren“ / sync) aus dem lokalen Repo neu.
package persist

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	PkgName     = "os-roster-agent"
	BinPath     = "/usr/local/bin/roster-agent"
	RCPath      = "/usr/local/etc/rc.d/rosteragent"
	RCConfPath  = "/etc/rc.conf.d/rosteragent"
	VersionPath = "/usr/local/opnsense/version/roster-agent"
	RepoDir     = "/var/lib/roster/pkg"
	RepoConf    = "/usr/local/etc/pkg/repos/roster.conf"
	RegisterPHP = "/usr/local/opnsense/scripts/firmware/register.php"
)

// RCScript ist der rc.d-Dienst (hyphenfreier Name wegen rc-/Shell-Variablen).
const RCScript = `#!/bin/sh
# PROVIDE: rosteragent
# REQUIRE: NETWORKING
# KEYWORD: shutdown
. /etc/rc.subr
name=rosteragent
rcvar=rosteragent_enable
pidfile="/var/run/${name}.pid"
command="/usr/sbin/daemon"
command_args="-P ${pidfile} -r -f /usr/local/bin/roster-agent -config /etc/roster/agent.yaml run"
load_rc_config $name
run_rc_command "$1"
`

// RCConf aktiviert den Dienst über /etc/rc.conf.d (OPNsense liest es in rc.freebsd ein).
const RCConf = "rosteragent_enable=\"YES\"\n"

// pkgVersion macht aus der Agent-Version eine gültige pkg-Version ("dev" -> "0.0.0.d<hash>").
func pkgVersion(v, binSum string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	ok := v != ""
	for _, r := range v {
		if !(r >= '0' && r <= '9' || r == '.') {
			ok = false
			break
		}
	}
	if !ok {
		return "0.0.0.d" + binSum[:8]
	}
	return v
}

// VersionJSON ist die OPNsense-Metadatei, an der register.php das Plugin erkennt.
func VersionJSON(version string) string {
	return fmt.Sprintf(`{"product_id":"%s","product_name":"%s","product_version":"%s","product_arch":"amd64","product_website":"https://github.com/boonkerz/roster","product_copyright_owner":"Roster","product_copyright_url":"https://github.com/boonkerz/roster","product_copyright_years":"2026","product_hash":"","product_abi":"","product_tier":"4"}`+"\n",
		PkgName, PkgName, version)
}

// Manifest ist das pkg-Manifest (UCL/JSON). abi "FreeBSD:*:amd64": das statische Go-Binary
// läuft über FreeBSD-Majors hinweg, das Paket soll nach einem ABI-Wechsel installierbar bleiben.
func Manifest(version, abi string) string {
	return fmt.Sprintf(`{
  "name": "%s",
  "origin": "sysutils/%s",
  "version": "%s",
  "comment": "Roster agent (inventory, monitoring, remote access)",
  "desc": "Roster agent registered as OPNsense plugin so firmware upgrades can reinstall it from the local repository /var/lib/roster/pkg.",
  "maintainer": "roster@localhost",
  "www": "https://github.com/boonkerz/roster",
  "prefix": "/usr/local",
  "abi": "%s",
  "categories": ["sysutils"],
  "licenselogic": "single",
  "licenses": ["BSD2CLAUSE"]
}
`, PkgName, PkgName, version, abi)
}

// Plist listet die Paketdateien relativ zum Präfix /usr/local.
const Plist = "bin/roster-agent\netc/rc.d/rosteragent\nopnsense/version/roster-agent\n"

// RepoConfText ist die pkg-Repo-Definition für das lokale Repo.
const RepoConfText = `# Roster: lokales Repo für os-roster-agent (vom Agent gepflegt)
roster: {
  url: "file:///var/lib/roster/pkg",
  signature_type: "none",
  priority: 0,
  enabled: yes
}
`

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
