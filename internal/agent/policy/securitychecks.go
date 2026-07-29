package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/shared"
)

// Sicherheits-/Hardware-Checks auf Basis der vorhandenen Collectoren (Security-Tab).
// Jeder Check parst die Collector-JSON und wertet sie aus; „nicht verfügbar/nicht
// unterstützt" -> Status "unknown" (kein Fehlalarm).

// smartCheck: failing, wenn ein Datenträger laut SMART "Warnung" oder "Fehler" ist.
func smartCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	return evalSmart(c, collect.SmartJSON(ctx))
}

func evalSmart(c shared.CheckSpec, js string) shared.CheckResult {
	var d struct {
		Disks []collect.SmartDisk `json:"disks"`
		Info  string              `json:"info"`
		Error string              `json:"error"`
	}
	if json.Unmarshal([]byte(js), &d) != nil || d.Error != "" {
		return unknownCheck(c, "SMART nicht lesbar")
	}
	if len(d.Disks) == 0 {
		return unknownCheck(c, firstNonEmpty(d.Info, "keine Datenträger gemeldet"))
	}
	var bad []string
	for _, dk := range d.Disks {
		if dk.Health == "Warnung" || dk.Health == "Fehler" {
			bad = append(bad, fmt.Sprintf("%s (%s)", dk.Name, dk.Health))
		}
	}
	if len(bad) > 0 {
		return failCheck(c, float64(len(bad)), "Datenträger nicht OK: "+strings.Join(bad, ", "))
	}
	return passCheck(c, 0, fmt.Sprintf("%d Datenträger OK", len(d.Disks)))
}

// avCheck: failing, wenn Virenschutz aus, kein Echtzeitschutz oder Signaturen zu alt.
// Config: max_signature_age (Tage, Standard 7).
func avCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	return evalAV(c, collect.AVStatusJSON(ctx))
}

func evalAV(c shared.CheckSpec, js string) shared.CheckResult {
	var a struct {
		Product      string `json:"product"`
		Enabled      bool   `json:"enabled"`
		RealTime     bool   `json:"realtime"`
		SignatureAge int    `json:"signature_age_days"`
		Info         string `json:"info"`
		Error        string `json:"error"`
	}
	if json.Unmarshal([]byte(js), &a) != nil || a.Error != "" {
		return unknownCheck(c, "Virenschutz nicht lesbar")
	}
	if a.Info != "" || a.Product == "" {
		return unknownCheck(c, firstNonEmpty(a.Info, "kein Virenschutz gemeldet"))
	}
	maxAge := 7
	if v, ok := numConfig(c, "max_signature_age"); ok && v > 0 {
		maxAge = int(v)
	}
	var problems []string
	if !a.Enabled {
		problems = append(problems, "deaktiviert")
	}
	if !a.RealTime {
		problems = append(problems, "kein Echtzeitschutz")
	}
	if a.SignatureAge > maxAge {
		problems = append(problems, fmt.Sprintf("Signaturen %d Tage alt (max %d)", a.SignatureAge, maxAge))
	}
	if len(problems) > 0 {
		return failCheck(c, 1, a.Product+": "+strings.Join(problems, ", "))
	}
	return passCheck(c, 0, a.Product+": aktiv, Echtzeitschutz, Signaturen aktuell")
}

// bitlockerCheck: failing, wenn ein Volume unverschlüsselt ist (Protection=Off).
func bitlockerCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	return evalBitlocker(c, collect.BitLockerJSON(ctx))
}

func evalBitlocker(c shared.CheckSpec, js string) shared.CheckResult {
	var b struct {
		Volumes []collect.BitLockerVolume `json:"volumes"`
		Info    string                    `json:"info"`
		Error   string                    `json:"error"`
	}
	if json.Unmarshal([]byte(js), &b) != nil || b.Error != "" {
		return unknownCheck(c, "BitLocker nicht lesbar")
	}
	if len(b.Volumes) == 0 {
		return unknownCheck(c, firstNonEmpty(b.Info, "keine Volumes gemeldet"))
	}
	var unenc []string
	for _, v := range b.Volumes {
		if v.Protection == "Off" {
			unenc = append(unenc, v.MountPoint)
		}
	}
	if len(unenc) > 0 {
		return failCheck(c, float64(len(unenc)), "unverschlüsselt: "+strings.Join(unenc, ", "))
	}
	return passCheck(c, 0, fmt.Sprintf("%d Volume(s) verschlüsselt", len(b.Volumes)))
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func passCheck(c shared.CheckSpec, val float64, out string) shared.CheckResult {
	return shared.CheckResult{CheckID: c.ID, Status: "passing", Value: val, Output: out}
}

func failCheck(c shared.CheckSpec, val float64, out string) shared.CheckResult {
	return shared.CheckResult{CheckID: c.ID, Status: "failing", Value: val, Output: out}
}
