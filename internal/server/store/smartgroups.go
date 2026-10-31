package store

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Smart Groups: dynamische Mitgliedschaft. Die Regel wird zu einer parametrisierten
// WHERE-Bedingung über die devices-Tabelle (Alias d) übersetzt – nur Felder aus einer
// Whitelist, Werte immer als Parameter (kein SQL-Injection-Risiko).

type ruleCond struct {
	Field string `json:"field"`
	Op    string `json:"op"` // eq | ne | contains | gt | lt
	Value string `json:"value"`
}

type groupRule struct {
	Match      string     `json:"match"` // all | any
	Conditions []ruleCond `json:"conditions"`
}

// textFields sind direkt vergleichbare Spalten der devices-Tabelle.
var textFields = map[string]string{
	"os": "d.os", "os_version": "d.os_version", "hostname": "d.hostname",
	"agent_version": "d.agent_version", "vendor": "d.vendor", "model": "d.model", "serial": "d.serial",
	"os_codename": "d.os_codename", "os_edition": "d.os_edition", "os_build": "d.os_build",
	"auto_updates": "d.auto_updates", "kernel_running": "d.kernel_running",
}

// smartWhere baut die WHERE-Bedingung einer Smart-Group-Regel. ok=false bei leerer/
// ungültiger Regel (dann ist es keine Smart Group).
func smartWhere(ruleJSON string, offlineCutoff time.Time) (string, []any, bool) {
	if strings.TrimSpace(ruleJSON) == "" {
		return "", nil, false
	}
	var r groupRule
	if err := json.Unmarshal([]byte(ruleJSON), &r); err != nil || len(r.Conditions) == 0 {
		return "", nil, false
	}
	var parts []string
	var args []any
	for _, c := range r.Conditions {
		if frag, a, ok := condSQL(c, offlineCutoff); ok {
			parts = append(parts, frag)
			args = append(args, a...)
		}
	}
	if len(parts) == 0 {
		return "", nil, false
	}
	sep := " AND "
	if r.Match == "any" {
		sep = " OR "
	}
	return "(" + strings.Join(parts, sep) + ")", args, true
}

func condSQL(c ruleCond, offlineCutoff time.Time) (string, []any, bool) {
	if col, ok := textFields[c.Field]; ok {
		switch c.Op {
		case "eq":
			return col + " = ?", []any{c.Value}, true
		case "ne":
			return col + " <> ?", []any{c.Value}, true
		case "contains":
			return col + " LIKE ?", []any{"%" + c.Value + "%"}, true
		}
		return "", nil, false
	}
	switch c.Field {
	case "site_id":
		if c.Op == "ne" {
			return "d.site_id <> ?", []any{c.Value}, true
		}
		return "d.site_id = ?", []any{c.Value}, true
	case "client_id":
		if c.Op == "ne" {
			return "d.site_id NOT IN (SELECT id FROM sites WHERE client_id = ?)", []any{c.Value}, true
		}
		return "d.site_id IN (SELECT id FROM sites WHERE client_id = ?)", []any{c.Value}, true
	case "updates_count", "security_updates_count":
		n, err := strconv.Atoi(c.Value)
		if err != nil {
			return "", nil, false
		}
		col := "d." + c.Field
		switch c.Op {
		case "gt":
			return col + " > ?", []any{n}, true
		case "lt":
			return col + " < ?", []any{n}, true
		case "eq":
			return col + " = ?", []any{n}, true
		}
	case "kernel_pending":
		// true = neuerer Kernel installiert als läuft (Neustart nötig), false = aktuell.
		cond := "(d.kernel_installed <> '' AND d.kernel_running <> '' AND d.kernel_installed <> d.kernel_running)"
		if c.Value == "false" || c.Op == "ne" && c.Value != "false" {
			return "NOT " + cond, nil, true
		}
		return cond, nil, true
	case "status":
		switch c.Value {
		case "online":
			return "(d.last_seen IS NOT NULL AND d.last_seen >= ?)", []any{offlineCutoff.UTC()}, true
		case "offline":
			return "(d.last_seen IS NULL OR d.last_seen < ?)", []any{offlineCutoff.UTC()}, true
		}
	}
	return "", nil, false
}
