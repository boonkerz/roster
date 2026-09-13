package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Proxmox-Hosts (Geräte, deren Agent auf einem PVE-Host läuft) lassen sich in der
// Liste aufklappen: darunter erscheinen VMs und Container mit Backup-Plakette und
// Start/Herunterfahren/Neustart/Stopp. Die Aktionen laufen als Agent-Befehl; der
// neue Status kommt mit dem Checkin, der unmittelbar auf die Ausführung folgt.

// pveStaleAfter: ab diesem Alter gilt ein Backup in der Anzeige als veraltet (wie im
// Web-UI). Verbindliche Schwellen und Alarme regelt der Check „Proxmox-Backups".
const pveStaleAfter = 26 * time.Hour

// confirmWindow: so lange bleibt ein scharf geschalteter Stopp-/Neustart-Knopf rot.
const confirmWindow = 4 * time.Second

// backupBadge beschreibt den Backup-Stand eines Gasts als Plakette.
func backupBadge(g pveGuest, now time.Time) badgeInfo {
	switch {
	case g.BackupTaskStatus == "failed":
		return badgeInfo{"Backup fehlgeschlagen", colRed}
	case g.BackupAt == nil:
		return badgeInfo{"kein Backup", colRed}
	case now.Sub(*g.BackupAt) > pveStaleAfter:
		return badgeInfo{"Backup " + humanAge(*g.BackupAt), colAmber}
	case g.BackupJob != nil && !*g.BackupJob:
		return badgeInfo{"kein Backup-Job", colAmber}
	default:
		return badgeInfo{"Backup " + humanAge(*g.BackupAt), colGreen}
	}
}

// guestBackupOK meldet, ob der Backup-Stand eines Gasts in Ordnung ist.
func guestBackupOK(g pveGuest, now time.Time) bool {
	return backupBadge(g, now).color == colGreen
}

// pveSummary zählt die (Nicht-Vorlagen-)Gäste und die ohne aktuelles Backup.
func pveSummary(d device, now time.Time) (guests, noBackup int) {
	for _, g := range d.ProxmoxGuests {
		if g.Template {
			continue
		}
		guests++
		if !guestBackupOK(g, now) {
			noBackup++
		}
	}
	return
}

// guestMatches prüft den Suchbegriff gegen VMID, Name und Node eines Gasts.
func guestMatches(g pveGuest, f string) bool {
	hay := strings.ToLower(strconv.Itoa(g.VMID) + " " + g.Name + " " + g.Node)
	return strings.Contains(hay, f)
}

// guestKey identifiziert einen Gast eindeutig in der App.
func guestKey(deviceID string, vmid int) string { return deviceID + ":" + strconv.Itoa(vmid) }

// parseGuestHit zerlegt „pve:<aktion>:<gerät>:<vmid>".
func parseGuestHit(id string) (action, deviceID string, vmid int, ok bool) {
	parts := strings.SplitN(id, ":", 4)
	if len(parts) != 4 || parts[0] != "pve" {
		return "", "", 0, false
	}
	n, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, false
	}
	return parts[1], parts[2], n, true
}

// toggleHost klappt die Gäste eines Proxmox-Hosts auf/zu und merkt sich das.
func (a *app) toggleHost(deviceID string) {
	if a.expanded[deviceID] {
		delete(a.expanded, deviceID)
	} else {
		a.expanded[deviceID] = true
	}
	ids := make([]string, 0, len(a.expanded))
	for id := range a.expanded {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	a.mu.Lock()
	a.cfg.ExpandedHosts = ids
	_ = a.cfg.save()
	a.mu.Unlock()
	a.markDirty()
}

// guestByID sucht Gerät und Gast zu einer Aktion.
func (a *app) guestByID(deviceID string, vmid int) (device, pveGuest, bool) {
	d, ok := a.deviceByID(deviceID)
	if !ok {
		return device{}, pveGuest{}, false
	}
	for _, g := range d.ProxmoxGuests {
		if g.VMID == vmid {
			return d, g, true
		}
	}
	return device{}, pveGuest{}, false
}

// clickGuest verarbeitet einen Knopf einer Gast-Zeile. Stopp und Neustart wollen
// eine Bestätigung: der erste Klick schaltet den Knopf scharf (rot), erst ein
// zweiter Klick innerhalb weniger Sekunden löst aus.
func (a *app) clickGuest(hit string) {
	action, deviceID, vmid, ok := parseGuestHit(hit)
	if !ok {
		return
	}
	d, g, ok := a.guestByID(deviceID, vmid)
	if !ok {
		return
	}
	if action == "stop" || action == "reboot" {
		if a.armed != hit || time.Since(a.armedAt) > confirmWindow {
			a.armed, a.armedAt = hit, time.Now()
			verb := map[string]string{"stop": "hart ausschalten", "reboot": "neu starten"}[action]
			a.setStatus(fmt.Sprintf("%s %d nochmal klicken zum Bestätigen (%s).", guestKind(g), g.VMID, verb), false)
			return
		}
		a.armed = ""
	}
	a.actProxmox(d, g, action)
}

func guestKind(g pveGuest) string {
	if g.Type == "lxc" {
		return "Container"
	}
	return "VM"
}

// actProxmox schickt die Aktion ab und verfolgt den Befehl bis zum Ergebnis.
func (a *app) actProxmox(d device, g pveGuest, action string) {
	key := guestKey(d.ID, g.VMID)
	a.mu.Lock()
	cli := a.cli
	if cli == nil || a.guestBusy[key] {
		a.mu.Unlock()
		return
	}
	a.guestBusy[key] = true
	a.mu.Unlock()

	what := fmt.Sprintf("%s %d", guestKind(g), g.VMID)
	if g.Name != "" {
		what += " (" + g.Name + ")"
	}
	verb := map[string]string{"start": "starten", "shutdown": "herunterfahren", "reboot": "neu starten", "stop": "stoppen"}[action]
	a.setStatus(what+": "+verb+" …", false)

	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.guestBusy, key)
			a.mu.Unlock()
			a.markDirty()
			a.requestRefresh() // der Agent hat mit dem Ergebnis frisches Inventar geschickt
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		cmdID, err := cli.proxmoxControl(ctx, d.ID, g, action)
		if err != nil {
			a.setStatus(what+": "+err.Error(), true)
			return
		}
		for {
			select {
			case <-ctx.Done():
				a.setStatus(what+": läuft noch – Status kommt mit dem nächsten Checkin.", false)
				return
			case <-time.After(time.Second):
			}
			st, err := cli.command(ctx, cmdID)
			if err != nil || st.Status != "done" {
				continue
			}
			if st.ExitCode != 0 {
				msg := strings.TrimSpace(st.Output)
				if msg == "" {
					msg = "fehlgeschlagen"
				}
				a.setStatus(what+": "+lastLine(msg), true)
				return
			}
			a.setStatus(what+": "+lastLine(st.Output), false)
			return
		}
	}()
}

// lastLine liefert die letzte Zeile – bei pvesh-Ausgaben die aussagekräftige (Ergebnis bzw. Fehler).
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
