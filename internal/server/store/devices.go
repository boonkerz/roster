package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// CreateDevice legt ein neu enrolltes Gerät an.
func (s *Store) CreateDevice(ctx context.Context, d *model.Device, agentTokenHash string) error {
	now := time.Now().UTC()
	if d.FirstSeen.IsZero() {
		d.FirstSeen = now
	}
	d.EnrolledAt = now
	var site any
	if d.SiteID != nil {
		site = *d.SiteID
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO devices (id, hostname, os, os_version, first_seen, enrolled_at, agent_token_hash, revoked, site_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		d.ID, d.Hostname, d.OS, d.OSVersion, d.FirstSeen, d.EnrolledAt, agentTokenHash, false, site)
	return err
}

// DeviceByTokenHash findet ein nicht widerrufenes Gerät anhand des Agent-Token-Hashes.
func (s *Store) DeviceByTokenHash(ctx context.Context, tokenHash string) (*model.Device, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id, hostname, os, os_version, revoked FROM devices
		WHERE agent_token_hash = ?`), tokenHash)
	var d model.Device
	err := row.Scan(&d.ID, &d.Hostname, &d.OS, &d.OSVersion, &d.Revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// UpdateInventory schreibt einen Inventar-Snapshot, ersetzt die Interfaces,
// aktualisiert die denormalisierten Kernfelder und setzt last_seen. Alles in einer Transaktion.
func (s *Store) UpdateInventory(ctx context.Context, deviceID string, inv shared.Inventory) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	disksJSON, _ := json.Marshal(inv.Disks)
	physJSON, _ := json.Marshal(inv.PhysicalDisks)
	gpusJSON, _ := json.Marshal(inv.GPUs)
	if _, err := tx.ExecContext(ctx, s.rebind(`
		UPDATE devices SET hostname=?, os=?, os_version=?, vendor=?, model=?, serial=?,
			cpu_model=?, cpu_cores=?, cpu_sockets=?, cpu_threads=?, memory_bytes=?, agent_version=?,
			public_ip=?, disks=?, physical_disks=?, gpus=?, logged_in_users=?, last_seen=? WHERE id=?`),
		inv.Hostname, inv.OS, inv.OSVersion, inv.Vendor, inv.Model, inv.Serial,
		inv.CPUModel, inv.CPUCores, inv.CPUSockets, inv.CPUThreads, int64(inv.MemoryBytes), inv.AgentVersion,
		inv.PublicIP, string(disksJSON), string(physJSON), string(gpusJSON), join(inv.LoggedInUsers), now, deviceID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM interfaces WHERE device_id = ?`), deviceID); err != nil {
		return err
	}
	for _, iface := range inv.Interfaces {
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO interfaces (device_id, name, mac, ipv4, ipv6) VALUES (?, ?, ?, ?, ?)`),
			deviceID, iface.Name, iface.MAC, join(iface.IPv4), join(iface.IPv6)); err != nil {
			return err
		}
	}

	// Software-Änderungen protokollieren (Diff gegen den bisherigen Stand), dann
	// Software bei jedem Checkin vollständig ersetzen.
	if err := s.recordSoftwareChanges(ctx, tx, deviceID, inv.Software, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM software WHERE device_id = ?`), deviceID); err != nil {
		return err
	}
	for _, sw := range inv.Software {
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO software (device_id, name, version, publisher) VALUES (?, ?, ?, ?)`),
			deviceID, sw.Name, sw.Version, sw.Publisher); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM printers WHERE device_id = ?`), deviceID); err != nil {
		return err
	}
	for _, pr := range inv.Printers {
		if _, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO printers (device_id, name, driver, port, is_default) VALUES (?, ?, ?, ?, ?)`),
			deviceID, pr.Name, pr.Driver, pr.Port, pr.Default); err != nil {
			return err
		}
	}

	// OS-Updates nur aktualisieren, wenn der Agent bereits einen Check geliefert hat.
	if inv.OSUpdates != nil {
		if _, err := tx.ExecContext(ctx, s.rebind(`
			UPDATE devices SET updates_count=?, updates_checked_at=? WHERE id=?`),
			inv.OSUpdates.Count, inv.OSUpdates.CheckedAt.UTC(), deviceID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, s.rebind(`DELETE FROM os_updates WHERE device_id = ?`), deviceID); err != nil {
			return err
		}
		for _, it := range inv.OSUpdates.Items {
			sev := it.Severity
			if sev == "" {
				sev = "Other"
			}
			if _, err := tx.ExecContext(ctx, s.rebind(`
				INSERT INTO os_updates (device_id, package, severity, url) VALUES (?, ?, ?, ?)`),
				deviceID, it.Name, sev, it.URL); err != nil {
				return err
			}
		}
	}

	data, _ := json.Marshal(inv)
	if _, err := tx.ExecContext(ctx, s.rebind(`
		INSERT INTO inventory (id, device_id, collected_at, data) VALUES (?, ?, ?, ?)`),
		newID(), deviceID, now, string(data)); err != nil {
		return err
	}

	return tx.Commit()
}

// deviceCols/deviceFrom bilden die Auswahl inkl. Site-/Client-Namen (LEFT JOIN).
const deviceCols = `d.id, d.hostname, d.os, d.os_version, d.vendor, d.model, d.serial, d.cpu_model, d.cpu_cores,
	d.cpu_sockets, d.cpu_threads, d.public_ip,
	d.memory_bytes, d.agent_version, d.first_seen, d.last_seen, d.enrolled_at, d.revoked, d.logged_in_users,
	d.updates_count, d.updates_checked_at, d.notes, d.site_id, s.name, c.id, c.name,
	(SELECT COUNT(*) FROM check_results cr WHERE cr.device_id=d.id),
	(SELECT COUNT(*) FROM check_results cr WHERE cr.device_id=d.id AND cr.status='failing'),
	(SELECT COUNT(DISTINCT tr.task_id) FROM task_results tr WHERE tr.device_id=d.id),
	(SELECT COUNT(*) FROM task_results tr WHERE tr.device_id=d.id AND tr.exit_code <> 0
		AND tr.ran_at = (SELECT MAX(tr2.ran_at) FROM task_results tr2
			WHERE tr2.device_id=tr.device_id AND tr2.task_id=tr.task_id)),
	(SELECT COUNT(*) FROM vulnerabilities v WHERE v.device_id=d.id),
	d.managed`

const deviceFrom = ` FROM devices d
	LEFT JOIN sites s ON s.id = d.site_id
	LEFT JOIN clients c ON c.id = s.client_id`

// ListDevices liefert alle Geräte inkl. Interfaces (für die Übersicht).
func (s *Store) ListDevices(ctx context.Context) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceCols+deviceFrom+` ORDER BY d.hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ifaces, err := s.interfacesFor(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Interfaces = ifaces
	}
	return out, nil
}

// searchWhere durchsucht Stammdaten, Software, Schnittstellen und Custom-Field-Werte.
const searchWhere = ` WHERE lower(d.hostname) LIKE ? OR lower(d.os) LIKE ? OR lower(d.os_version) LIKE ?
	OR lower(d.vendor) LIKE ? OR lower(d.model) LIKE ? OR lower(d.serial) LIKE ?
	OR lower(d.public_ip) LIKE ? OR lower(d.logged_in_users) LIKE ? OR lower(d.notes) LIKE ?
	OR EXISTS (SELECT 1 FROM software sw WHERE sw.device_id=d.id AND (lower(sw.name) LIKE ? OR lower(sw.publisher) LIKE ?))
	OR EXISTS (SELECT 1 FROM interfaces i WHERE i.device_id=d.id AND (lower(i.ipv4) LIKE ? OR lower(i.mac) LIKE ?))
	OR EXISTS (SELECT 1 FROM custom_field_values cv WHERE cv.entity_id=d.id AND lower(cv.value) LIKE ?)`

// SearchDevices liefert Geräte, die den Suchbegriff irgendwo treffen (Hostname,
// OS, Hersteller/Modell/Seriennr., IP/MAC, installierte Software, Custom Fields).
func (s *Store) SearchDevices(ctx context.Context, q string) ([]model.Device, error) {
	pat := "%" + strings.ToLower(q) + "%"
	args := make([]any, strings.Count(searchWhere, "?"))
	for i := range args {
		args[i] = pat
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+deviceCols+deviceFrom+searchWhere+` ORDER BY d.hostname`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		ifaces, err := s.interfacesFor(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Interfaces = ifaces
	}
	return out, nil
}

// GetDevice lädt ein Gerät inkl. Interfaces und Gruppen.
func (s *Store) GetDevice(ctx context.Context, id string) (*model.Device, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`SELECT `+deviceCols+deviceFrom+` WHERE d.id = ?`), id)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if d.Interfaces, err = s.interfacesFor(ctx, id); err != nil {
		return nil, err
	}
	if d.Groups, err = s.groupsForDevice(ctx, id); err != nil {
		return nil, err
	}
	if d.Software, err = s.softwareFor(ctx, id); err != nil {
		return nil, err
	}
	if d.Printers, err = s.printersFor(ctx, id); err != nil {
		return nil, err
	}
	if d.AvailableUpdates, err = s.updatesFor(ctx, id); err != nil {
		return nil, err
	}
	if d.CheckResults, err = s.CheckResultsFor(ctx, id); err != nil {
		return nil, err
	}
	if d.TaskResults, err = s.LatestTaskResultsFor(ctx, id); err != nil {
		return nil, err
	}
	if d.Commands, err = s.CommandsFor(ctx, id, 20); err != nil {
		return nil, err
	}
	// Anzahl wirksamer Checks/Tasks (für die Statusmeldung "zugewiesen vs. ausgewertet").
	if bundle, err := s.EffectivePolicy(ctx, id); err == nil && bundle != nil {
		d.AssignedChecks = len(bundle.Checks)
		d.AssignedTasks = len(bundle.Tasks)
	}
	// Datenträger/GPUs aus den JSON-Spalten lesen.
	var disksJSON, physJSON, gpusJSON string
	if err := s.db.QueryRowContext(ctx, s.rebind(`SELECT disks, physical_disks, gpus FROM devices WHERE id=?`), id).
		Scan(&disksJSON, &physJSON, &gpusJSON); err == nil {
		_ = json.Unmarshal([]byte(emptyToNull(disksJSON)), &d.Disks)
		_ = json.Unmarshal([]byte(emptyToNull(physJSON)), &d.PhysicalDisks)
		_ = json.Unmarshal([]byte(emptyToNull(gpusJSON)), &d.GPUs)
	}
	return d, nil
}

// emptyToNull macht aus einem leeren String ein "null", damit json.Unmarshal nicht fehlschlägt.
func emptyToNull(s string) string {
	if s == "" {
		return "null"
	}
	return s
}

func (s *Store) updatesFor(ctx context.Context, deviceID string) ([]model.UpdateItem, error) {
	// Kritische/Wichtige zuerst, dann nach Name; Genehmigungsstatus per LEFT JOIN.
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT u.package, u.severity, u.url, CASE WHEN a.name IS NULL THEN 0 ELSE 1 END
		FROM os_updates u
		LEFT JOIN patch_approvals a ON a.device_id = u.device_id AND a.name = u.package
		WHERE u.device_id = ?
		ORDER BY CASE u.severity WHEN 'Critical' THEN 0 WHEN 'Important' THEN 1 ELSE 2 END, u.package`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.UpdateItem
	for rows.Next() {
		var it model.UpdateItem
		var approved int
		if err := rows.Scan(&it.Name, &it.Severity, &it.URL, &approved); err != nil {
			return nil, err
		}
		it.Approved = approved == 1
		out = append(out, it)
	}
	return out, rows.Err()
}

// ApprovePatch genehmigt einen Patch (idempotent) bzw. hebt die Genehmigung auf.
func (s *Store) ApprovePatch(ctx context.Context, deviceID, name string, approved bool) error {
	if !approved {
		_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM patch_approvals WHERE device_id=? AND name=?`), deviceID, name)
		return err
	}
	// Upsert ohne ON CONFLICT (portabel).
	var n int
	_ = s.db.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM patch_approvals WHERE device_id=? AND name=?`), deviceID, name).Scan(&n)
	if n > 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO patch_approvals (device_id, name) VALUES (?, ?)`), deviceID, name)
	return err
}

// ApprovedPatches liefert die genehmigten Patches, die aktuell auch ausstehend sind.
func (s *Store) ApprovedPatches(ctx context.Context, deviceID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT u.package FROM os_updates u
		JOIN patch_approvals a ON a.device_id = u.device_id AND a.name = u.package
		WHERE u.device_id = ?`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeviceSoftware liefert die installierte Software eines Geräts (für den CVE-Scan).
func (s *Store) DeviceSoftware(ctx context.Context, deviceID string) ([]model.SoftwarePackage, error) {
	return s.softwareFor(ctx, deviceID)
}

func (s *Store) softwareFor(ctx context.Context, deviceID string) ([]model.SoftwarePackage, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT name, version, publisher FROM software WHERE device_id = ? ORDER BY name`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SoftwarePackage
	for rows.Next() {
		var sw model.SoftwarePackage
		if err := rows.Scan(&sw.Name, &sw.Version, &sw.Publisher); err != nil {
			return nil, err
		}
		out = append(out, sw)
	}
	return out, rows.Err()
}

// recordSoftwareChanges vergleicht die neue Softwareliste mit dem gespeicherten
// Stand und schreibt added/removed/updated-Ereignisse. Übersprungen wird, wenn es
// noch keinen Stand gibt (Erstinventar = Baseline) oder die neue Liste leer ist
// (vermutlich Sammel-Aussetzer – kein Massen-„entfernt").
func (s *Store) recordSoftwareChanges(ctx context.Context, tx *sql.Tx, deviceID string, newSW []shared.SoftwarePackage, now time.Time) error {
	if len(newSW) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, s.rebind(`SELECT name, version FROM software WHERE device_id = ?`), deviceID)
	if err != nil {
		return err
	}
	old := map[string]string{}
	for rows.Next() {
		var name, ver string
		if err := rows.Scan(&name, &ver); err != nil {
			rows.Close()
			return err
		}
		old[name] = ver
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(old) == 0 {
		return nil // Baseline – kein Massen-„installiert"
	}
	add := func(change, name, version, oldVersion string) error {
		_, err := tx.ExecContext(ctx, s.rebind(`
			INSERT INTO software_events (id, device_id, change, name, version, old_version, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`),
			newID(), deviceID, change, name, version, oldVersion, now)
		return err
	}
	seen := map[string]bool{}
	for _, sw := range newSW {
		seen[sw.Name] = true
		prev, ok := old[sw.Name]
		if !ok {
			if err := add("added", sw.Name, sw.Version, ""); err != nil {
				return err
			}
		} else if prev != sw.Version {
			if err := add("updated", sw.Name, sw.Version, prev); err != nil {
				return err
			}
		}
	}
	for name, ver := range old {
		if !seen[name] {
			if err := add("removed", name, ver, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

// SoftwareEventsSince liefert die Software-Änderungen eines Geräts ab einem Zeitpunkt
// (für Alerting direkt nach einem Checkin).
func (s *Store) SoftwareEventsSince(ctx context.Context, deviceID string, since time.Time) ([]model.SoftwareEvent, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, change, name, version, old_version, created_at
		FROM software_events WHERE device_id=? AND created_at >= ? ORDER BY created_at DESC`), deviceID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SoftwareEvent
	for rows.Next() {
		ev := model.SoftwareEvent{DeviceID: deviceID}
		if err := rows.Scan(&ev.ID, &ev.Change, &ev.Name, &ev.Version, &ev.OldVersion, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// SoftwareEventsFor liefert die letzten Software-Änderungen eines Geräts.
func (s *Store) SoftwareEventsFor(ctx context.Context, deviceID string, limit int) ([]model.SoftwareEvent, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, change, name, version, old_version, created_at
		FROM software_events WHERE device_id = ? ORDER BY created_at DESC LIMIT ?`), deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SoftwareEvent
	for rows.Next() {
		ev := model.SoftwareEvent{DeviceID: deviceID}
		if err := rows.Scan(&ev.ID, &ev.Change, &ev.Name, &ev.Version, &ev.OldVersion, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) printersFor(ctx context.Context, deviceID string) ([]model.Printer, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT name, driver, port, is_default FROM printers WHERE device_id = ? ORDER BY name`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Printer
	for rows.Next() {
		var pr model.Printer
		if err := rows.Scan(&pr.Name, &pr.Driver, &pr.Port, &pr.Default); err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

// SetDeviceNotes speichert die Freitext-Notizen eines Geräts.
func (s *Store) SetDeviceNotes(ctx context.Context, id, notes string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE devices SET notes=? WHERE id=?`), notes, id))
}

// DeleteDevice entfernt ein Gerät vollständig.
func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM devices WHERE id = ?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// InventorySnapshot ist ein historischer Inventar-Stand (rohes JSON).
type InventorySnapshot struct {
	ID          string          `json:"id"`
	CollectedAt time.Time       `json:"collected_at"`
	Data        json.RawMessage `json:"data"`
}

// InventoryHistory liefert die letzten n Inventar-Snapshots eines Geräts (neueste zuerst).
func (s *Store) InventoryHistory(ctx context.Context, deviceID string, limit int) ([]InventorySnapshot, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, collected_at, data FROM inventory
		WHERE device_id = ? ORDER BY collected_at DESC LIMIT ?`), deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InventorySnapshot
	for rows.Next() {
		var snap InventorySnapshot
		var data string
		if err := rows.Scan(&snap.ID, &snap.CollectedAt, &data); err != nil {
			return nil, err
		}
		snap.Data = json.RawMessage(data)
		out = append(out, snap)
	}
	return out, rows.Err()
}

// DevicesForTarget liefert die IDs aller (nicht widerrufenen) Geräte eines Ziels
// für Sammelaktionen: device | site | client | group | all.
func (s *Store) DevicesForTarget(ctx context.Context, targetType, targetID string, offlineCutoff time.Time) ([]string, error) {
	var query string
	var args []any
	// Nicht verwaltete Geräte (ohne Agent) sind nie Ziel einer Sammelaktion.
	switch targetType {
	case "device":
		query, args = `SELECT id FROM devices WHERE id=? AND revoked=0 AND managed=1`, []any{targetID}
	case "site":
		query, args = `SELECT id FROM devices WHERE site_id=? AND revoked=0 AND managed=1`, []any{targetID}
	case "client":
		query, args = `SELECT d.id FROM devices d JOIN sites s ON s.id=d.site_id WHERE s.client_id=? AND d.revoked=0 AND d.managed=1`, []any{targetID}
	case "group":
		// Smart Group (Regel gesetzt) -> dynamisch auflösen; sonst statische Zuordnung.
		var rule string
		if err := s.db.QueryRowContext(ctx, s.rebind(`SELECT rule FROM groups WHERE id=?`), targetID).Scan(&rule); err != nil {
			return nil, err
		}
		if where, wargs, ok := smartWhere(rule, offlineCutoff); ok {
			query = `SELECT d.id FROM devices d WHERE d.revoked=0 AND d.managed=1 AND ` + where
			args = wargs
		} else {
			query, args = `SELECT dg.device_id FROM device_groups dg JOIN devices d ON d.id=dg.device_id WHERE dg.group_id=? AND d.revoked=0 AND d.managed=1`, []any{targetID}
		}
	case "all":
		query, args = `SELECT id FROM devices WHERE revoked=0 AND managed=1`, nil
	default:
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// OnlineNeighborInSite liefert die ID eines online (last_seen >= cutoff), nicht
// widerrufenen Geräts im selben Standort – zum Senden eines WoL-Pakets an ein
// offlines Gerät im selben Netz. ErrNotFound, wenn keins verfügbar ist.
func (s *Store) OnlineNeighborInSite(ctx context.Context, siteID, excludeID string, cutoff time.Time) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT id FROM devices
		WHERE site_id=? AND id<>? AND revoked=0 AND last_seen IS NOT NULL AND last_seen >= ?
		ORDER BY last_seen DESC LIMIT 1`), siteID, excludeID, cutoff.UTC()).Scan(&id)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return id, err
}

// RevokeDevice sperrt das Agent-Token eines Geräts (Agent kann sich nicht mehr melden).
func (s *Store) RevokeDevice(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE devices SET revoked = ?, agent_token_hash = '' WHERE id = ?`), true, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) interfacesFor(ctx context.Context, deviceID string) ([]model.Interface, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT name, mac, ipv4, ipv6 FROM interfaces WHERE device_id = ? ORDER BY name`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Interface
	for rows.Next() {
		var iface model.Interface
		if err := rows.Scan(&iface.Name, &iface.MAC, &iface.IPv4, &iface.IPv6); err != nil {
			return nil, err
		}
		out = append(out, iface)
	}
	return out, rows.Err()
}

func scanDevice(row scanner) (*model.Device, error) {
	var d model.Device
	var lastSeen, updatesCheckedAt sql.NullTime
	var mem int64
	var users string
	var updatesCount sql.NullInt64
	var siteID, siteName, clientID, clientName sql.NullString
	err := row.Scan(&d.ID, &d.Hostname, &d.OS, &d.OSVersion, &d.Vendor, &d.Model, &d.Serial,
		&d.CPUModel, &d.CPUCores, &d.CPUSockets, &d.CPUThreads, &d.PublicIP,
		&mem, &d.AgentVersion, &d.FirstSeen, &lastSeen, &d.EnrolledAt, &d.Revoked, &users,
		&updatesCount, &updatesCheckedAt, &d.Notes, &siteID, &siteName, &clientID, &clientName,
		&d.ChecksTotal, &d.ChecksFailing, &d.TasksTotal, &d.TasksFailing, &d.VulnCount, &d.Managed)
	if err != nil {
		return nil, err
	}
	if siteID.Valid {
		v := siteID.String
		d.SiteID = &v
		d.SiteName = siteName.String
		d.ClientID = clientID.String
		d.ClientName = clientName.String
	}
	d.MemoryBytes = uint64(mem)
	if lastSeen.Valid {
		t := lastSeen.Time
		d.LastSeen = &t
	}
	if users != "" {
		d.LoggedInUsers = strings.Split(users, ",")
	}
	if updatesCount.Valid {
		n := int(updatesCount.Int64)
		d.UpdatesCount = &n
	}
	if updatesCheckedAt.Valid {
		t := updatesCheckedAt.Time
		d.UpdatesCheckedAt = &t
	}
	return &d, nil
}

// join verbindet IP-Strings kommasepariert (Interfaces werden bei jedem Checkin ersetzt).
func join(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
