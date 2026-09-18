package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// Backup-Einträge der Richtlinien und ihre Läufe. Wichtig: Alle Zustandswechsel eines
// Laufs laufen über BEDINGTE Updates (… WHERE status IN (…)) und melden zurück, ob sie
// tatsächlich etwas geändert haben. Nur so lösen Checkin-Hook und Watchdog die
// Folgeaktion genau einmal aus – sonst legt der zweite Aufruf z. B. das Backup-Ziel
// mitten im nächsten Lauf schlafen.

const backupCols = `id, policy_id, name, type, enabled, config, script_id, weekdays, at_time,
	catch_up_minutes, wait_device_id, wait_minutes, timeout_minutes,
	after_device_id, after_script_id, after_when, last_run_at, created_at`

func scanBackup(row scanner) (model.PolicyBackup, error) {
	var b model.PolicyBackup
	var cfg string
	var scriptID, waitDev, afterDev, afterScript sql.NullString
	var lastRun sql.NullTime
	err := row.Scan(&b.ID, &b.PolicyID, &b.Name, &b.Type, &b.Enabled, &cfg, &scriptID, &b.Weekdays, &b.AtTime,
		&b.CatchUpMinutes, &waitDev, &b.WaitMinutes, &b.TimeoutMinutes,
		&afterDev, &afterScript, &b.AfterWhen, &lastRun, &b.CreatedAt)
	if err != nil {
		return b, err
	}
	_ = json.Unmarshal([]byte(cfg), &b.Config)
	b.ScriptID = strPtr(scriptID)
	b.WaitDeviceID = strPtr(waitDev)
	b.AfterDeviceID = strPtr(afterDev)
	b.AfterScriptID = strPtr(afterScript)
	if lastRun.Valid {
		t := lastRun.Time
		b.LastRunAt = &t
	}
	return b, nil
}

// strPtr wandelt eine optionale Textspalte in *string ("" und NULL → nil).
func strPtr(v sql.NullString) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

// backupsOf liefert die Backup-Einträge einer Richtlinie.
func (s *Store) backupsOf(ctx context.Context, policyID string) ([]model.PolicyBackup, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+backupCols+` FROM policy_backups WHERE policy_id=? ORDER BY name`), policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PolicyBackup
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBackup liefert einen einzelnen Eintrag.
func (s *Store) GetBackup(ctx context.Context, id string) (*model.PolicyBackup, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`SELECT `+backupCols+` FROM policy_backups WHERE id=?`), id)
	b, err := scanBackup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// EnabledBackups liefert alle aktiven Einträge (für den Zeitplan-Loop).
func (s *Store) EnabledBackups(ctx context.Context) ([]model.PolicyBackup, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+backupCols+` FROM policy_backups WHERE enabled=TRUE`))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PolicyBackup
	for rows.Next() {
		b, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SaveBackup legt einen Eintrag an oder aktualisiert ihn (ID leer = neu).
func (s *Store) SaveBackup(ctx context.Context, b *model.PolicyBackup) error {
	cfg, err := json.Marshal(b.Config)
	if err != nil {
		return err
	}
	if b.Config == nil {
		cfg = []byte("{}")
	}
	if b.ID == "" {
		b.ID = newID()
		b.CreatedAt = time.Now().UTC()
		_, err := s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO policy_backups (id, policy_id, name, type, enabled, config, script_id, weekdays, at_time,
				catch_up_minutes, wait_device_id, wait_minutes, timeout_minutes,
				after_device_id, after_script_id, after_when, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			b.ID, b.PolicyID, b.Name, b.Type, b.Enabled, string(cfg), b.ScriptID, b.Weekdays, b.AtTime,
			b.CatchUpMinutes, b.WaitDeviceID, b.WaitMinutes, b.TimeoutMinutes,
			b.AfterDeviceID, b.AfterScriptID, b.AfterWhen, b.CreatedAt)
		return err
	}
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE policy_backups SET name=?, type=?, enabled=?, config=?, script_id=?, weekdays=?, at_time=?,
			catch_up_minutes=?, wait_device_id=?, wait_minutes=?, timeout_minutes=?,
			after_device_id=?, after_script_id=?, after_when=?
		WHERE id=?`),
		b.Name, b.Type, b.Enabled, string(cfg), b.ScriptID, b.Weekdays, b.AtTime,
		b.CatchUpMinutes, b.WaitDeviceID, b.WaitMinutes, b.TimeoutMinutes,
		b.AfterDeviceID, b.AfterScriptID, b.AfterWhen, b.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBackup entfernt einen Eintrag samt Läufen (ON DELETE CASCADE).
func (s *Store) DeleteBackup(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM policy_backups WHERE id=?`), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DevicesForPolicy liefert die Geräte, auf die eine Richtlinie wirkt (Zuweisung direkt
// oder über Standort/Kunde – Gegenrichtung zu EffectivePolicy).
func (s *Store) DevicesForPolicy(ctx context.Context, policyID string) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+deviceCols+deviceFrom+`
		WHERE d.revoked=FALSE AND EXISTS (
			SELECT 1 FROM policy_assignments pa WHERE pa.policy_id=?
			  AND ((pa.target_type='device' AND pa.target_id=d.id)
			    OR (pa.target_type='site'   AND pa.target_id=d.site_id)
			    OR (pa.target_type='client' AND pa.target_id=c.id)))
		ORDER BY d.hostname`), policyID)
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
	return out, rows.Err()
}

// ClaimBackupSchedule markiert einen Eintrag als „für heute gestartet" – aber nur, wenn
// das nicht schon geschehen ist. Bedingter Update, damit zwei Serverinstanzen bzw. zwei
// Ticks nicht doppelt planen. Liefert false, wenn ein anderer zuerst war.
func (s *Store) ClaimBackupSchedule(ctx context.Context, backupID string, scheduledAt time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE policy_backups SET last_run_at=?
		WHERE id=? AND (last_run_at IS NULL OR last_run_at < ?)`),
		scheduledAt.UTC(), backupID, scheduledAt.UTC())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// InsertBackupRun legt einen Lauf an. Der UNIQUE-Index (backup_id, device_id,
// scheduled_at) verhindert Doppelläufe; ein zweiter Versuch scheitert dort.
func (s *Store) InsertBackupRun(ctx context.Context, r *model.BackupRun) error {
	if r.ID == "" {
		r.ID = newID()
	}
	r.CreatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO backup_runs (id, backup_id, device_id, trigger_type, status, scheduled_at, wait_until,
			started_at, deadline_at, command_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		r.ID, r.BackupID, r.DeviceID, r.TriggerType, r.Status, r.ScheduledAt.UTC(), r.WaitUntil,
		r.StartedAt, r.DeadlineAt, r.CommandID, r.CreatedAt)
	return err
}

// StartBackupRun schaltet einen wartenden Lauf auf „running" und hinterlegt den Befehl.
// Bedingt, damit nicht zwei Ticks denselben Lauf starten.
func (s *Store) StartBackupRun(ctx context.Context, runID, commandID string, deadline time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE backup_runs SET status='running', started_at=?, deadline_at=?, command_id=?
		WHERE id=? AND status='waiting'`),
		time.Now().UTC(), deadline.UTC(), commandID, runID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FinishBackupRun schließt einen offenen Lauf ab. changed=false heißt: ein anderer Pfad
// (Checkin-Hook oder Watchdog) war schneller – dann darf die Folgeaktion NICHT laufen.
func (s *Store) FinishBackupRun(ctx context.Context, runID, status string, exitCode int, summary, output string) (bool, error) {
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE backup_runs SET status=?, finished_at=?, exit_code=?, summary=?, output=?
		WHERE id=? AND status IN ('waiting','running')`),
		status, time.Now().UTC(), exitCode, summary, output, runID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetBackupRunFollow vermerkt die eingereihte Folgeaktion.
func (s *Store) SetBackupRunFollow(ctx context.Context, runID, commandID string) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`UPDATE backup_runs SET follow_command_id=? WHERE id=?`), commandID, runID)
	return err
}

const runCols = `r.id, r.backup_id, COALESCE(b.name,''), r.device_id, r.trigger_type, r.status, r.scheduled_at,
	r.wait_until, r.started_at, r.finished_at, r.deadline_at, r.exit_code, r.summary, r.output,
	r.command_id, r.follow_command_id, r.created_at`

func scanRun(row scanner) (model.BackupRun, error) {
	var r model.BackupRun
	var waitUntil, startedAt, finishedAt, deadlineAt sql.NullTime
	var cmdID, followID sql.NullString
	err := row.Scan(&r.ID, &r.BackupID, &r.BackupName, &r.DeviceID, &r.TriggerType, &r.Status, &r.ScheduledAt,
		&waitUntil, &startedAt, &finishedAt, &deadlineAt, &r.ExitCode, &r.Summary, &r.Output,
		&cmdID, &followID, &r.CreatedAt)
	if err != nil {
		return r, err
	}
	for _, p := range []struct {
		src sql.NullTime
		dst **time.Time
	}{{waitUntil, &r.WaitUntil}, {startedAt, &r.StartedAt}, {finishedAt, &r.FinishedAt}, {deadlineAt, &r.DeadlineAt}} {
		if p.src.Valid {
			t := p.src.Time
			*p.dst = &t
		}
	}
	r.CommandID = strPtr(cmdID)
	r.FollowID = strPtr(followID)
	return r, nil
}

// OpenBackupRuns liefert alle Läufe, die noch auf etwas warten (für den Watchdog).
func (s *Store) OpenBackupRuns(ctx context.Context) ([]model.BackupRun, error) {
	return s.queryRuns(ctx, `WHERE r.status IN ('waiting','running') ORDER BY r.created_at`)
}

// BackupRunByCommand findet den Lauf zu einem Agent-Befehl (Checkin-Hook).
func (s *Store) BackupRunByCommand(ctx context.Context, commandID string) (*model.BackupRun, error) {
	runs, err := s.queryRuns(ctx, `WHERE r.command_id=?`, commandID)
	if err != nil || len(runs) == 0 {
		return nil, err
	}
	return &runs[0], nil
}

// OpenRunsForFollowDevice zählt offene Läufe, deren Folgeaktion dasselbe Gerät betrifft.
// Damit wartet die Folgeaktion, bis wirklich alle Backups fertig sind (sonst legt der
// erste fertige Lauf das Backup-Ziel schlafen, während der zweite noch schreibt).
func (s *Store) OpenRunsForFollowDevice(ctx context.Context, afterDeviceID, exceptRunID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT COUNT(*) FROM backup_runs r JOIN policy_backups b ON b.id=r.backup_id
		WHERE b.after_device_id=? AND r.id<>? AND r.status IN ('waiting','running')`),
		afterDeviceID, exceptRunID).Scan(&n)
	return n, err
}

// BackupRunsForDevice liefert die jüngsten Läufe eines Geräts.
func (s *Store) BackupRunsForDevice(ctx context.Context, deviceID string, limit int) ([]model.BackupRun, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.queryRuns(ctx, `WHERE r.device_id=? ORDER BY r.created_at DESC LIMIT ?`, deviceID, limit)
}

// BackupRun liefert einen einzelnen Lauf (inkl. Ausgabe).
func (s *Store) BackupRun(ctx context.Context, id string) (*model.BackupRun, error) {
	runs, err := s.queryRuns(ctx, `WHERE r.id=?`, id)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, ErrNotFound
	}
	return &runs[0], nil
}

func (s *Store) queryRuns(ctx context.Context, where string, args ...any) ([]model.BackupRun, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT `+runCols+`
		FROM backup_runs r LEFT JOIN policy_backups b ON b.id=r.backup_id `+where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BackupRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneBackupRuns löscht alte Läufe (wie die übrige Historie).
func (s *Store) PruneBackupRuns(ctx context.Context, retention time.Duration) error {
	cutoff := time.Now().UTC().Add(-retention)
	_, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM backup_runs WHERE created_at < ? AND status NOT IN ('waiting','running')`), cutoff)
	return err
}
