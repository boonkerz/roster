package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// --- Skripte ---

func (s *Store) ListScripts(ctx context.Context) ([]model.Script, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, shell, platforms, content, check_only, created_at FROM scripts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Script
	for rows.Next() {
		var sc model.Script
		var plat string
		if err := rows.Scan(&sc.ID, &sc.Name, &sc.Shell, &plat, &sc.Content, &sc.CheckOnly, &sc.CreatedAt); err != nil {
			return nil, err
		}
		sc.Platforms = []string{}
		_ = json.Unmarshal([]byte(plat), &sc.Platforms)
		out = append(out, sc)
	}
	return out, rows.Err()
}

func platformsJSON(p []string) string {
	b, err := json.Marshal(p)
	if err != nil || len(b) == 0 {
		return "[]"
	}
	return string(b)
}

func (s *Store) CreateScript(ctx context.Context, sc *model.Script) error {
	sc.CreatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO scripts (id, name, shell, platforms, content, check_only, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`),
		sc.ID, sc.Name, sc.Shell, platformsJSON(sc.Platforms), sc.Content, sc.CheckOnly, sc.CreatedAt)
	return err
}

func (s *Store) UpdateScript(ctx context.Context, sc *model.Script) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`
		UPDATE scripts SET name=?, shell=?, platforms=?, content=?, check_only=? WHERE id=?`),
		sc.Name, sc.Shell, platformsJSON(sc.Platforms), sc.Content, sc.CheckOnly, sc.ID))
}

func (s *Store) DeleteScript(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM scripts WHERE id=?`), id))
}

// --- Policies ---

func (s *Store) ListPolicies(ctx context.Context) ([]model.Policy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description FROM policies ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Policy
	for rows.Next() {
		var p model.Policy
		if err := rows.Scan(&p.ID, &p.Name, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := s.loadPolicyChildren(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) loadPolicyChildren(ctx context.Context, p *model.Policy) error {
	checks, err := s.checksOf(ctx, p.ID)
	if err != nil {
		return err
	}
	p.Checks = checks
	tasks, err := s.tasksOf(ctx, p.ID)
	if err != nil {
		return err
	}
	p.Tasks = tasks
	asg, err := s.assignmentsOf(ctx, p.ID)
	if err != nil {
		return err
	}
	p.Assignments = asg
	return nil
}

func (s *Store) checksOf(ctx context.Context, policyID string) ([]model.PolicyCheck, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, name, type, config, script_id, severity, frequency, remediation_script_id FROM policy_checks WHERE policy_id=? ORDER BY name`), policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PolicyCheck
	for rows.Next() {
		var c model.PolicyCheck
		var cfg string
		var scriptID, remScriptID sql.NullString
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &cfg, &scriptID, &c.Severity, &c.Frequency, &remScriptID); err != nil {
			return nil, err
		}
		c.PolicyID = policyID
		_ = json.Unmarshal([]byte(cfg), &c.Config)
		if scriptID.Valid {
			v := scriptID.String
			c.ScriptID = &v
		}
		if remScriptID.Valid && remScriptID.String != "" {
			v := remScriptID.String
			c.RemediationScriptID = &v
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) tasksOf(ctx context.Context, policyID string) ([]model.PolicyTask, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, name, script_id, interval_minutes, schedule_type, daily_time, weekdays, collect_fields, frequency
		FROM policy_tasks WHERE policy_id=? ORDER BY name`), policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.PolicyTask
	for rows.Next() {
		var t model.PolicyTask
		var scriptID sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &scriptID, &t.IntervalMinutes, &t.ScheduleType, &t.DailyTime, &t.Weekdays, &t.CollectFields, &t.Frequency); err != nil {
			return nil, err
		}
		t.PolicyID = policyID
		if scriptID.Valid {
			v := scriptID.String
			t.ScriptID = &v
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) assignmentsOf(ctx context.Context, policyID string) ([]model.Assignment, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, target_type, target_id FROM policy_assignments WHERE policy_id=?`), policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Assignment
	for rows.Next() {
		a := model.Assignment{PolicyID: policyID}
		if err := rows.Scan(&a.ID, &a.TargetType, &a.TargetID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) CreatePolicy(ctx context.Context, p *model.Policy) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`INSERT INTO policies (id, name, description) VALUES (?, ?, ?)`),
		p.ID, p.Name, p.Description)
	return err
}

func (s *Store) UpdatePolicy(ctx context.Context, p *model.Policy) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`UPDATE policies SET name=?, description=? WHERE id=?`),
		p.Name, p.Description, p.ID))
}

func (s *Store) DeletePolicy(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM policies WHERE id=?`), id))
}

func (s *Store) AddCheck(ctx context.Context, c *model.PolicyCheck) error {
	cfg, _ := json.Marshal(c.Config)
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}
	var scriptID any
	if c.ScriptID != nil {
		scriptID = *c.ScriptID
	}
	var remID any
	if c.RemediationScriptID != nil && *c.RemediationScriptID != "" {
		remID = *c.RemediationScriptID
	}
	severity := c.Severity
	if severity != "warning" {
		severity = "critical"
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO policy_checks (id, policy_id, name, type, config, script_id, severity, frequency, remediation_script_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		c.ID, c.PolicyID, c.Name, c.Type, string(cfg), scriptID, severity, c.Frequency, remID)
	return err
}

// RemediationScript liefert das Remediation-Skript eines Checks (self-healing),
// oder ErrNotFound, wenn keines konfiguriert ist.
func (s *Store) RemediationScript(ctx context.Context, checkID string) (*model.Script, error) {
	var sc model.Script
	var plat string
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT sr.id, sr.name, sr.shell, sr.platforms, sr.content
		FROM policy_checks pc JOIN scripts sr ON sr.id = pc.remediation_script_id
		WHERE pc.id = ?`), checkID).Scan(&sc.ID, &sc.Name, &sc.Shell, &plat, &sc.Content)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sc.Platforms = []string{}
	_ = json.Unmarshal([]byte(plat), &sc.Platforms)
	return &sc, nil
}

// RemediationDue meldet, ob für Gerät+Check seit `cooldown` keine Remediation lief.
func (s *Store) RemediationDue(ctx context.Context, deviceID, checkID string, cooldown time.Duration) bool {
	var last time.Time
	err := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT last_run FROM remediation_runs WHERE device_id=? AND check_id=?`), deviceID, checkID).Scan(&last)
	if err != nil {
		return true // noch nie gelaufen
	}
	return time.Since(last) >= cooldown
}

// MarkRemediation vermerkt den Zeitpunkt der letzten Remediation (Upsert).
func (s *Store) MarkRemediation(ctx context.Context, deviceID, checkID string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, s.rebind(
		`UPDATE remediation_runs SET last_run=? WHERE device_id=? AND check_id=?`), at.UTC(), deviceID, checkID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(
			`INSERT INTO remediation_runs (device_id, check_id, last_run) VALUES (?, ?, ?)`), deviceID, checkID, at.UTC())
	}
	return err
}

// CheckSeverities liefert den Schweregrad ("warning"/"critical") je Check-ID.
func (s *Store) CheckSeverities(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	in, args := placeholders(ids)
	rows, err := s.db.QueryContext(ctx, s.rebind(`SELECT id, severity FROM policy_checks WHERE id IN (`+in+`)`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, sev string
		if err := rows.Scan(&id, &sev); err != nil {
			return nil, err
		}
		out[id] = sev
	}
	return out, rows.Err()
}

func (s *Store) DeleteCheck(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM policy_checks WHERE id=?`), id))
}

func (s *Store) AddTask(ctx context.Context, t *model.PolicyTask) error {
	var scriptID any
	if t.ScriptID != nil {
		scriptID = *t.ScriptID
	}
	if t.IntervalMinutes <= 0 {
		t.IntervalMinutes = 60
	}
	if t.ScheduleType == "" {
		t.ScheduleType = "interval"
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO policy_tasks (id, policy_id, name, script_id, interval_minutes, schedule_type, daily_time, weekdays, collect_fields, frequency)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		t.ID, t.PolicyID, t.Name, scriptID, t.IntervalMinutes, t.ScheduleType, t.DailyTime, t.Weekdays, t.CollectFields, t.Frequency)
	return err
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM policy_tasks WHERE id=?`), id))
}

func (s *Store) AddAssignment(ctx context.Context, a *model.Assignment) error {
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO policy_assignments (id, policy_id, target_type, target_id) VALUES (?, ?, ?, ?)`),
		a.ID, a.PolicyID, a.TargetType, a.TargetID)
	return err
}

func (s *Store) DeleteAssignment(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM policy_assignments WHERE id=?`), id))
}

// --- Effektive Policy für ein Gerät (Vererbung über device/site/client) ---

func (s *Store) EffectivePolicy(ctx context.Context, deviceID string) (*shared.PolicyBundle, error) {
	var siteID, clientID sql.NullString
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT d.site_id, s.client_id FROM devices d LEFT JOIN sites s ON s.id=d.site_id WHERE d.id=?`),
		deviceID).Scan(&siteID, &clientID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT DISTINCT policy_id FROM policy_assignments
		WHERE (target_type='device' AND target_id=?)
		   OR (target_type='site'   AND target_id=?)
		   OR (target_type='client' AND target_id=?)`),
		deviceID, siteID.String, clientID.String)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var policyIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		policyIDs = append(policyIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(policyIDs) == 0 {
		return nil, nil
	}

	bundle := &shared.PolicyBundle{}
	in, args := placeholders(policyIDs)

	cr, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT pc.id, pc.name, pc.type, pc.config, sc.shell, sc.content, pc.frequency, sc.platforms
		FROM policy_checks pc LEFT JOIN scripts sc ON sc.id=pc.script_id
		WHERE pc.policy_id IN (`+in+`)`), args...)
	if err != nil {
		return nil, err
	}
	for cr.Next() {
		var spec shared.CheckSpec
		var cfg string
		var shell, content, platforms sql.NullString
		if err := cr.Scan(&spec.ID, &spec.Name, &spec.Type, &cfg, &shell, &content, &spec.Frequency, &platforms); err != nil {
			cr.Close()
			return nil, err
		}
		_ = json.Unmarshal([]byte(cfg), &spec.Config)
		spec.Shell = shell.String
		spec.Script = content.String
		_ = json.Unmarshal([]byte(platforms.String), &spec.Platforms)
		bundle.Checks = append(bundle.Checks, spec)
	}
	cr.Close()

	tr, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT pt.id, pt.name, pt.interval_minutes, pt.schedule_type, pt.daily_time, pt.weekdays, sc.shell, sc.content, pt.frequency, sc.platforms
		FROM policy_tasks pt JOIN scripts sc ON sc.id=pt.script_id
		WHERE pt.policy_id IN (`+in+`)`), args...)
	if err != nil {
		return nil, err
	}
	defer tr.Close()
	for tr.Next() {
		var spec shared.TaskSpec
		var platforms sql.NullString
		if err := tr.Scan(&spec.ID, &spec.Name, &spec.IntervalMinutes, &spec.ScheduleType, &spec.DailyTime, &spec.Weekdays, &spec.Shell, &spec.Script, &spec.Frequency, &platforms); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(platforms.String), &spec.Platforms)
		bundle.Tasks = append(bundle.Tasks, spec)
	}
	if err := tr.Err(); err != nil {
		return nil, err
	}

	// Platzhalter {{agent.x}}/{{client.x}}/{{site.x}} in Skripten ersetzen (Werte
	// dieses Geräts). Erst nach dem Schließen der obigen Rows (eine SQLite-Verbindung).
	agent, client, site, err := s.FieldMapsForDevice(ctx, deviceID)
	if err == nil {
		for i := range bundle.Checks {
			bundle.Checks[i].Script = SubstituteFields(bundle.Checks[i].Script, agent, client, site)
			// Auch String-Werte der Check-Config (z.B. Host/URL bei Netzwerk-Checks)
			// ersetzen, damit dort {{agent.domains|first}} o.ä. funktioniert.
			for k, v := range bundle.Checks[i].Config {
				if s, ok := v.(string); ok {
					bundle.Checks[i].Config[k] = SubstituteFields(s, agent, client, site)
				}
			}
		}
		for i := range bundle.Tasks {
			bundle.Tasks[i].Script = SubstituteFields(bundle.Tasks[i].Script, agent, client, site)
		}
	}
	return bundle, nil
}

// --- Ergebnisse ---

// SaveCheckResults speichert die Ergebnisse (Upsert) und protokolliert jeden
// Statuswechsel als check_event. Zurückgegeben werden die Transitionen
// (für Alerting inkl. Recovery); der erste „gesunde" Bericht (unbekannt→passing)
// wird bewusst nicht als Ereignis gewertet.
func (s *Store) SaveCheckResults(ctx context.Context, deviceID string, results []shared.CheckResult) ([]model.CheckEvent, error) {
	now := time.Now().UTC()
	var events []model.CheckEvent
	for _, r := range results {
		var prev sql.NullString
		_ = s.db.QueryRowContext(ctx, s.rebind(
			`SELECT status FROM check_results WHERE device_id=? AND check_id=?`), deviceID, r.CheckID).Scan(&prev)
		old := prev.String
		if old == "" {
			old = "unknown"
		}
		// Statuswechsel? (Erstmeldung „unbekannt→gut" ignorieren – kein Ereignis.)
		if old != r.Status && !(prev.String == "" && r.Status == "passing") {
			ev := model.CheckEvent{
				ID: newID(), DeviceID: deviceID, CheckID: r.CheckID, CheckName: s.CheckName(ctx, r.CheckID),
				OldStatus: old, NewStatus: r.Status, Output: r.Output, CreatedAt: now,
			}
			if _, err := s.db.ExecContext(ctx, s.rebind(`
				INSERT INTO check_events (id, device_id, check_id, check_name, old_status, new_status, output, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`),
				ev.ID, ev.DeviceID, ev.CheckID, ev.CheckName, ev.OldStatus, ev.NewStatus, ev.Output, now); err != nil {
				return nil, err
			}
			events = append(events, ev)
		}
		// Upsert ohne ON CONFLICT (portabel): erst UPDATE, dann ggf. INSERT.
		res, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE check_results SET status=?, output=?, value=?, updated_at=? WHERE device_id=? AND check_id=?`),
			r.Status, r.Output, r.Value, now, deviceID, r.CheckID)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := s.db.ExecContext(ctx, s.rebind(`
				INSERT INTO check_results (device_id, check_id, status, output, value, updated_at) VALUES (?, ?, ?, ?, ?, ?)`),
				deviceID, r.CheckID, r.Status, r.Output, r.Value, now); err != nil {
				return nil, err
			}
		}
	}
	return events, nil
}

// MarkEventsNotified markiert die angegebenen Ereignisse als benachrichtigt.
func (s *Store) MarkEventsNotified(ctx context.Context, ids []string, at time.Time) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx, s.rebind(
			`UPDATE check_events SET notified=?, notified_at=? WHERE id=?`), true, at.UTC(), id); err != nil {
			return err
		}
	}
	return nil
}

// RecentCheckEvents liefert die jüngsten Statuswechsel über alle Geräte (für die
// Übersicht), inkl. Hostname.
func (s *Store) RecentCheckEvents(ctx context.Context, limit int) ([]model.CheckEvent, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT ce.id, ce.device_id, d.hostname, ce.check_name, ce.old_status, ce.new_status,
			ce.output, ce.notified, ce.notified_at, ce.created_at
		FROM check_events ce LEFT JOIN devices d ON d.id = ce.device_id
		ORDER BY ce.created_at DESC LIMIT ?`), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CheckEvent
	for rows.Next() {
		var ev model.CheckEvent
		var host sql.NullString
		var notifiedAt sql.NullTime
		if err := rows.Scan(&ev.ID, &ev.DeviceID, &host, &ev.CheckName, &ev.OldStatus, &ev.NewStatus,
			&ev.Output, &ev.Notified, &notifiedAt, &ev.CreatedAt); err != nil {
			return nil, err
		}
		ev.Hostname = host.String
		if notifiedAt.Valid {
			t := notifiedAt.Time
			ev.NotifiedAt = &t
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// CheckEventsFor liefert den jüngsten Statuswechsel-Verlauf eines Geräts.
func (s *Store) CheckEventsFor(ctx context.Context, deviceID string, limit int) ([]model.CheckEvent, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT id, check_id, check_name, old_status, new_status, output, notified, notified_at, created_at
		FROM check_events WHERE device_id=? ORDER BY created_at DESC LIMIT ?`), deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CheckEvent
	for rows.Next() {
		ev := model.CheckEvent{DeviceID: deviceID}
		var notifiedAt sql.NullTime
		if err := rows.Scan(&ev.ID, &ev.CheckID, &ev.CheckName, &ev.OldStatus, &ev.NewStatus,
			&ev.Output, &ev.Notified, &notifiedAt, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if notifiedAt.Valid {
			t := notifiedAt.Time
			ev.NotifiedAt = &t
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// CheckName liefert den Anzeigenamen eines Checks (für Alerts).
func (s *Store) CheckName(ctx context.Context, checkID string) string {
	var name sql.NullString
	_ = s.db.QueryRowContext(ctx, s.rebind(`SELECT name FROM policy_checks WHERE id=?`), checkID).Scan(&name)
	return name.String
}

func (s *Store) SaveTaskResults(ctx context.Context, deviceID string, results []shared.TaskResult) error {
	for _, r := range results {
		if _, err := s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO task_results (id, device_id, task_id, exit_code, output, ran_at) VALUES (?, ?, ?, ?, ?, ?)`),
			newID(), deviceID, r.TaskID, r.ExitCode, r.Output, r.RanAt.UTC()); err != nil {
			return err
		}
	}
	return nil
}

// CheckResultsFor liefert die aktuellen Check-Ergebnisse eines Geräts inkl. Name/Typ.
func (s *Store) CheckResultsFor(ctx context.Context, deviceID string) ([]model.CheckResult, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT cr.check_id, cr.status, cr.output, cr.value, cr.updated_at, pc.name, pc.type
		FROM check_results cr LEFT JOIN policy_checks pc ON pc.id=cr.check_id
		WHERE cr.device_id=? ORDER BY cr.status DESC, pc.name`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.CheckResult
	for rows.Next() {
		var r model.CheckResult
		var val sql.NullFloat64
		var name, typ sql.NullString
		if err := rows.Scan(&r.CheckID, &r.Status, &r.Output, &val, &r.UpdatedAt, &name, &typ); err != nil {
			return nil, err
		}
		r.Value = val.Float64
		r.Name = name.String
		r.Type = typ.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// TaskResultsFor liefert die letzten Task-Läufe eines Geräts inkl. Name (Historie).
func (s *Store) TaskResultsFor(ctx context.Context, deviceID string, limit int) ([]model.TaskResult, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT tr.id, tr.task_id, tr.exit_code, tr.output, tr.ran_at, pt.name
		FROM task_results tr LEFT JOIN policy_tasks pt ON pt.id=tr.task_id
		WHERE tr.device_id=? ORDER BY tr.ran_at DESC LIMIT ?`), deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TaskResult
	for rows.Next() {
		var r model.TaskResult
		var name sql.NullString
		if err := rows.Scan(&r.ID, &r.TaskID, &r.ExitCode, &r.Output, &r.RanAt, &name); err != nil {
			return nil, err
		}
		r.Name = name.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestTaskResultsFor liefert je Task nur den jüngsten Lauf eines Geräts (für die
// Task-Übersicht – ein Eintrag pro Task statt der gesamten Historie).
func (s *Store) LatestTaskResultsFor(ctx context.Context, deviceID string) ([]model.TaskResult, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT tr.id, tr.task_id, tr.exit_code, tr.output, tr.ran_at, pt.name
		FROM task_results tr LEFT JOIN policy_tasks pt ON pt.id=tr.task_id
		WHERE tr.device_id=? AND tr.ran_at = (
			SELECT MAX(tr2.ran_at) FROM task_results tr2
			WHERE tr2.device_id=tr.device_id AND tr2.task_id=tr.task_id)
		ORDER BY pt.name`), deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.TaskResult
	for rows.Next() {
		var r model.TaskResult
		var name sql.NullString
		if err := rows.Scan(&r.ID, &r.TaskID, &r.ExitCode, &r.Output, &r.RanAt, &name); err != nil {
			return nil, err
		}
		r.Name = name.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// PruneHistory löscht Task-Läufe und abgeschlossene Befehle, die vor cutoff liegen.
// Liefert die Anzahl gelöschter Task- bzw. Befehlszeilen. Check-Ergebnisse werden
// nicht gelöscht – sie halten je Check nur den aktuellen Stand.
func (s *Store) PruneHistory(ctx context.Context, cutoff, auditCutoff time.Time) (int64, int64, error) {
	cut := cutoff.UTC()
	tr, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM task_results WHERE ran_at < ?`), cut)
	if err != nil {
		return 0, 0, err
	}
	tasks, _ := tr.RowsAffected()
	cr, err := s.db.ExecContext(ctx, s.rebind(
		`DELETE FROM commands WHERE status = 'done' AND created_at < ?`), cut)
	if err != nil {
		return tasks, 0, err
	}
	cmds, _ := cr.RowsAffected()
	// Verlauf der Status- und Software-Änderungen ebenfalls beschneiden.
	_, _ = s.db.ExecContext(ctx, s.rebind(`DELETE FROM check_events WHERE created_at < ?`), cut)
	_, _ = s.db.ExecContext(ctx, s.rebind(`DELETE FROM software_events WHERE created_at < ?`), cut)
	// Audit-Log separat (sicherheitsrelevant, längere Aufbewahrung).
	_, _ = s.db.ExecContext(ctx, s.rebind(`DELETE FROM audit_log WHERE ts < ?`), auditCutoff.UTC())
	return tasks, cmds, nil
}

// placeholders baut "?, ?, ..." samt Argumentliste für ein IN().
func placeholders(values []string) (string, []any) {
	marks := make([]string, len(values))
	args := make([]any, len(values))
	for i, v := range values {
		marks[i] = "?"
		args[i] = v
	}
	return strings.Join(marks, ", "), args
}
