package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
)

// --- Feld-Definitionen ---

// CustomFields liefert die Definitionen für ein Modell (model=="" => alle).
func (s *Store) CustomFields(ctx context.Context, mdl string) ([]model.CustomField, error) {
	q := `SELECT id, model, name, type, options, default_value, required, managed, link, selection_field FROM custom_fields`
	var args []any
	if mdl != "" {
		q += ` WHERE model=?`
		args = append(args, mdl)
	}
	q += ` ORDER BY model, name`
	rows, err := s.db.QueryContext(ctx, s.rebind(q), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CustomField{}
	for rows.Next() {
		f, opts := model.CustomField{Options: []string{}}, ""
		if err := rows.Scan(&f.ID, &f.Model, &f.Name, &f.Type, &opts, &f.Default, &f.Required, &f.Managed, &f.Link, &f.SelectionField); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(opts), &f.Options)
		out = append(out, f)
	}
	return out, rows.Err()
}

// CreateCustomField legt eine Definition an.
func (s *Store) CreateCustomField(ctx context.Context, f *model.CustomField) error {
	opts, _ := json.Marshal(f.Options)
	if len(opts) == 0 {
		opts = []byte("[]")
	}
	_, err := s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO custom_fields (id, model, name, type, options, default_value, required, managed, link, selection_field)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		f.ID, f.Model, f.Name, f.Type, string(opts), f.Default, f.Required, f.Managed, f.Link, f.SelectionField)
	return err
}

// UpdateCustomField ändert Name/Typ/Optionen/Default/Pflicht (Modell bleibt fix).
func (s *Store) UpdateCustomField(ctx context.Context, f *model.CustomField) error {
	opts, _ := json.Marshal(f.Options)
	if len(opts) == 0 {
		opts = []byte("[]")
	}
	return s.affect(s.db.ExecContext(ctx, s.rebind(`
		UPDATE custom_fields SET name=?, type=?, options=?, default_value=?, required=?, managed=?, link=?, selection_field=? WHERE id=?`),
		f.Name, f.Type, string(opts), f.Default, f.Required, f.Managed, f.Link, f.SelectionField, f.ID))
}

// DeleteCustomField entfernt eine Definition inkl. ihrer Werte (FK CASCADE).
func (s *Store) DeleteCustomField(ctx context.Context, id string) error {
	return s.affect(s.db.ExecContext(ctx, s.rebind(`DELETE FROM custom_fields WHERE id=?`), id))
}

// --- Werte ---

// CustomFieldValues liefert je Definition eines Modells den Wert einer Entität
// (Default, falls kein Wert gesetzt ist). Eine einzige JOIN-Query (kein
// verschachteltes Lesen -> kein SQLite-Verbindungs-Deadlock).
func (s *Store) CustomFieldValues(ctx context.Context, mdl, entityID string) ([]model.CustomFieldValue, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(`
		SELECT f.id, f.model, f.name, f.type, f.options, f.default_value, f.required, f.managed, f.link, f.selection_field, COALESCE(v.value, '')
		FROM custom_fields f
		LEFT JOIN custom_field_values v ON v.field_id=f.id AND v.entity_id=?
		WHERE f.model=?
		ORDER BY f.name`), entityID, mdl)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.CustomFieldValue{}
	for rows.Next() {
		var cv model.CustomFieldValue
		var opts string
		cv.Field.Options = []string{}
		if err := rows.Scan(&cv.Field.ID, &cv.Field.Model, &cv.Field.Name, &cv.Field.Type, &opts,
			&cv.Field.Default, &cv.Field.Required, &cv.Field.Managed, &cv.Field.Link, &cv.Field.SelectionField, &cv.Value); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(opts), &cv.Field.Options)
		if cv.Value == "" {
			cv.Value = cv.Field.Default
		}
		out = append(out, cv)
	}
	return out, rows.Err()
}

// SetCustomFieldValue setzt (Upsert) den Wert eines Feldes für eine Entität.
func (s *Store) SetCustomFieldValue(ctx context.Context, fieldID, entityID, value string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`
		UPDATE custom_field_values SET value=? WHERE field_id=? AND entity_id=?`), value, fieldID, entityID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO custom_field_values (field_id, entity_id, value) VALUES (?, ?, ?)`), fieldID, entityID, value)
	}
	return err
}

// EnsureCustomField liefert die ID einer Definition (model,name) und legt sie bei
// Bedarf mit dem angegebenen Typ an.
func (s *Store) EnsureCustomField(ctx context.Context, mdl, name, typ string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT id FROM custom_fields WHERE model=? AND name=?`), mdl, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = newID()
	_, err = s.db.ExecContext(ctx, s.rebind(`
		INSERT INTO custom_fields (id, model, name, type, options, default_value, required)
		VALUES (?, ?, ?, ?, '[]', '', FALSE)`), id, mdl, name, typ)
	return id, err
}

// --- JSON-Collector ---

// ApplyCollected parst die JSON-Ausgabe eines Collector-Tasks und schreibt die
// Werte in die Felder von Gerät (agent), Client und Site. Unbekannte Felder werden
// mit aus dem Wert abgeleitetem Typ automatisch angelegt.
func (s *Store) ApplyCollected(ctx context.Context, deviceID, output string) error {
	var doc map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &doc); err != nil {
		return err // kein JSON -> ignorieren (Aufrufer loggt)
	}

	// Ziel-Entitäten auflösen.
	var siteID, clientID sql.NullString
	_ = s.db.QueryRowContext(ctx, s.rebind(`
		SELECT d.site_id, s.client_id FROM devices d LEFT JOIN sites s ON s.id=d.site_id WHERE d.id=?`),
		deviceID).Scan(&siteID, &clientID)

	targets := map[string]string{"agent": deviceID, "device": deviceID, "site": siteID.String, "client": clientID.String}
	for key, entityID := range targets {
		sub, ok := doc[key].(map[string]any)
		if !ok || entityID == "" {
			continue
		}
		mdl := key
		if key == "agent" {
			mdl = "device"
		}
		for name, raw := range sub {
			fieldID, err := s.EnsureCustomField(ctx, mdl, name, inferFieldType(raw))
			if err != nil {
				return err
			}
			if err := s.SetCustomFieldValue(ctx, fieldID, entityID, normalizeValue(raw)); err != nil {
				return err
			}
		}
	}
	return nil
}

func inferFieldType(v any) string {
	switch val := v.(type) {
	case bool:
		return "checkbox"
	case float64, int, int64, json.Number:
		return "number"
	case []any:
		return "list"
	case string:
		if isDateLike(val) {
			return "datetime"
		}
		return "text"
	default:
		return "text"
	}
}

func isDateLike(s string) bool {
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02T15:04"} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// normalizeValue wandelt einen JSON-Wert in die String-Speicherform (Arrays als JSON).
func normalizeValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// CollectorTaskIDs liefert die IDs aller Tasks mit collect_fields=true.
func (s *Store) CollectorTaskIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM policy_tasks WHERE collect_fields=TRUE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// --- Platzhalter-Ersetzung ---

// FieldMapsForDevice liefert die Anzeige-Werte je Feldname für agent/client/site
// (für die Ersetzung von {{agent.x}}/{{client.x}}/{{site.x}} in Skripten).
func (s *Store) FieldMapsForDevice(ctx context.Context, deviceID string) (agent, client, site map[string]string, err error) {
	var siteID, clientID sql.NullString
	_ = s.db.QueryRowContext(ctx, s.rebind(`
		SELECT d.site_id, s.client_id FROM devices d LEFT JOIN sites s ON s.id=d.site_id WHERE d.id=?`),
		deviceID).Scan(&siteID, &clientID)

	if agent, err = s.fieldMap(ctx, "device", deviceID); err != nil {
		return
	}
	if client, err = s.fieldMap(ctx, "client", clientID.String); err != nil {
		return
	}
	site, err = s.fieldMap(ctx, "site", siteID.String)
	return
}

func (s *Store) fieldMap(ctx context.Context, mdl, entityID string) (map[string]string, error) {
	out := map[string]string{}
	if entityID == "" {
		return out, nil
	}
	vals, err := s.CustomFieldValues(ctx, mdl, entityID)
	if err != nil {
		return nil, err
	}
	for _, cv := range vals {
		// Rohwert (Listen als JSON-Array) – die Filter im Template kümmern sich um
		// die Darstellung.
		out[cv.Field.Name] = cv.Value
	}
	return out, nil
}

// fieldTmplRe erfasst {{ scope.name | filter | filter(arg) }} (scope = agent|client|site).
var fieldTmplRe = regexp.MustCompile(`\{\{\s*(agent|client|site)\.(.*?)\s*\}\}`)

// SubstituteFields ersetzt {{ scope.name }} in Skripten durch Custom-Field-Werte und
// unterstützt Twig-artige Filter, z.B. {{ agent.domains | first }} oder
// {{ agent.domains | nth(1) }}, {{ agent.tags | join(" ") }}, {{ agent.x | default("-") }}.
func SubstituteFields(script string, agent, client, site map[string]string) string {
	maps := map[string]map[string]string{"agent": agent, "client": client, "site": site}
	return fieldTmplRe.ReplaceAllStringFunc(script, func(m string) string {
		sub := fieldTmplRe.FindStringSubmatch(m)
		scope, body := sub[1], sub[2]
		name, filterStr := body, ""
		if i := strings.Index(body, "|"); i >= 0 {
			name, filterStr = body[:i], body[i+1:]
		}
		raw := maps[scope][strings.TrimSpace(name)] // unbekanntes Feld -> ""
		return applyFieldFilters(raw, filterStr)
	})
}

// applyFieldFilters rendert einen (ggf. Listen-)Rohwert nach den angegebenen Filtern.
// Ohne Filter werden Listen komma-getrennt ausgegeben (abwärtskompatibel).
func applyFieldFilters(raw, filterStr string) string {
	var list []string
	isList := strings.HasPrefix(raw, "[") && json.Unmarshal([]byte(raw), &list) == nil
	cur := raw
	if isList {
		cur = strings.Join(list, ",")
	}
	for _, part := range strings.Split(filterStr, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fname, arg := part, ""
		if i := strings.Index(part, "("); i >= 0 && strings.HasSuffix(part, ")") {
			fname = strings.TrimSpace(part[:i])
			arg = strings.TrimSpace(part[i+1 : len(part)-1])
			// umschließende Anführungszeichen entfernen (Inhalt bleibt erhalten).
			if len(arg) >= 2 && (arg[0] == '"' || arg[0] == '\'') && arg[len(arg)-1] == arg[0] {
				arg = arg[1 : len(arg)-1]
			}
		}
		switch fname {
		case "first":
			if isList {
				cur = firstOr(list, "")
			}
			isList = false
		case "last":
			if isList && len(list) > 0 {
				cur = list[len(list)-1]
			} else if isList {
				cur = ""
			}
			isList = false
		case "nth":
			i, _ := strconv.Atoi(arg)
			if isList && i >= 0 && i < len(list) {
				cur = list[i]
			} else if isList {
				cur = ""
			}
			isList = false
		case "count":
			if isList {
				cur = strconv.Itoa(len(list))
			} else if cur == "" {
				cur = "0"
			} else {
				cur = "1"
			}
			isList = false
		case "join":
			if isList {
				cur = strings.Join(list, arg)
			}
			isList = false
		case "upper":
			cur = strings.ToUpper(cur)
		case "lower":
			cur = strings.ToLower(cur)
		case "trim":
			cur = strings.TrimSpace(cur)
		case "default":
			if cur == "" {
				cur = arg
			}
		}
	}
	return cur
}

func firstOr(list []string, def string) string {
	if len(list) > 0 {
		return list[0]
	}
	return def
}
