package policy

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/agent/collect"
	"github.com/boonkerz/roster/internal/shared"
)

// proxmoxBackupCheck prüft den Backup-Stand der Proxmox-Gäste. Konfiguration:
//
//	max_age_hours      – letztes Backup höchstens so alt (Standard 26 = tägliche Sicherung + Puffer)
//	vmids              – nur diese Gäste, kommagetrennt (leer = alle)
//	exclude_vmids      – diese Gäste auslassen, kommagetrennt
//	include_stopped    – auch gestoppte Gäste prüfen (Standard true)
//	include_templates  – auch Vorlagen prüfen (Standard false)
//	require_job        – Gast muss in einem Backup-Job stecken (Standard true)
//
// Rot, sobald ein geprüfter Gast kein Backup hat, sein letztes Backup zu alt ist,
// der letzte vzdump-Lauf für ihn fehlschlug oder er in keinem Backup-Job steckt.
func proxmoxBackupCheck(ctx context.Context, c shared.CheckSpec) shared.CheckResult {
	if !collect.ProxmoxAvailable() {
		return unknownCheck(c, "kein Proxmox-VE-Host (pvesh nicht gefunden)")
	}
	info := collect.Proxmox(ctx)
	if info == nil {
		return unknownCheck(c, "Proxmox-Gäste nicht abrufbar (pvesh-Fehler)")
	}
	return evalProxmoxBackup(c, info.Guests, time.Now())
}

// evalProxmoxBackup wertet den Backup-Stand gegen die Konfiguration aus (rein, testbar).
func evalProxmoxBackup(c shared.CheckSpec, guests []shared.ProxmoxGuest, now time.Time) shared.CheckResult {
	maxAge := 26.0
	if v, ok := numConfig(c, "max_age_hours"); ok && v > 0 {
		maxAge = v
	}
	only := vmidSet(strConfig(c, "vmids"))
	exclude := vmidSet(strConfig(c, "exclude_vmids"))
	includeStopped := boolConfig(c, "include_stopped", true)
	includeTemplates := boolConfig(c, "include_templates", false)
	requireJob := boolConfig(c, "require_job", true)

	var problems []string
	checked := 0
	var oldest time.Duration
	for _, g := range guests {
		switch {
		case len(only) > 0 && !only[g.VMID], exclude[g.VMID]:
			continue
		case g.Template && !includeTemplates:
			continue
		case g.Status != "running" && !includeStopped:
			continue
		}
		checked++
		label := strconv.Itoa(g.VMID)
		if g.Name != "" {
			label += " " + g.Name
		}
		switch {
		case g.BackupTaskStatus == "failed":
			msg := "letzter Lauf fehlgeschlagen"
			if g.BackupTaskMsg != "" {
				msg += ": " + g.BackupTaskMsg
			}
			problems = append(problems, label+" ("+msg+")")
		case g.BackupAt == nil:
			problems = append(problems, label+" (kein Backup)")
		case now.Sub(*g.BackupAt) > time.Duration(maxAge*float64(time.Hour)):
			problems = append(problems, fmt.Sprintf("%s (letztes Backup vor %s)", label, ageText(now.Sub(*g.BackupAt))))
		case requireJob && g.BackupJob != nil && !*g.BackupJob:
			problems = append(problems, label+" (in keinem Backup-Job)")
		default:
			if age := now.Sub(*g.BackupAt); age > oldest {
				oldest = age
			}
		}
	}

	if checked == 0 {
		return unknownCheck(c, "keine passenden Gäste gefunden")
	}
	if len(problems) > 0 {
		out := fmt.Sprintf("%d von %d Gästen ohne aktuelles Backup: %s", len(problems), checked, strings.Join(problems, ", "))
		return shared.CheckResult{CheckID: c.ID, Status: "failing", Value: float64(len(problems)), Output: trunc(out)}
	}
	out := fmt.Sprintf("%d Gäste gesichert, ältestes Backup vor %s", checked, ageText(oldest))
	return shared.CheckResult{CheckID: c.ID, Status: "passing", Value: 0, Output: out}
}

// vmidSet liest eine kommagetrennte VMID-Liste.
func vmidSet(s string) map[int]bool {
	out := map[int]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if id, err := strconv.Atoi(part); err == nil {
			out[id] = true
		}
	}
	return out
}

// boolConfig liest einen Wahrheitswert mit Standard.
func boolConfig(c shared.CheckSpec, key string, def bool) bool {
	if v, ok := c.Config[key].(bool); ok {
		return v
	}
	return def
}

// ageText formatiert ein Alter knapp (Minuten, Stunden, Tage).
func ageText(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	default:
		return fmt.Sprintf("%d Tagen", int(d.Hours()/24))
	}
}
