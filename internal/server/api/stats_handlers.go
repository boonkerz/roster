package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

// Statistik-Quellen für die konfigurierbaren Dashboard-Diagramme.
const (
	statKindSummary = "summary"
	statKindCheck   = "check"
	statKindField   = "field"
)

// summaryKeys sind die Kennzahlen, die je Standort als eigene Reihe gezählt werden.
var summaryKeys = []string{"devices_total", "devices_online", "devices_offline", "devices_unknown",
	"failing_checks", "failing_tasks", "pending_patches", "vulnerabilities"}

// computeStatSamples zählt den Ist-Zustand in Statistik-Zeilen (ohne Zeitstempel):
// Kennzahlen und der Gerätestatus je Standort, der Status jedes Checks und die
// Verteilung jedes Feldwerts – Listenfelder zählen jeden Eintrag einzeln.
func (s *Server) computeStatSamples(devices []model.Device, checks []store.CheckStatus, fields []model.CustomField, vals []model.EntityFieldValue) []store.StatSample {
	type cell struct{ site, kind, key, label string }
	counts := map[cell]float64{}
	order := []cell{}
	add := func(site, kind, key, label string, v float64) {
		c := cell{site, kind, key, label}
		if _, ok := counts[c]; !ok {
			order = append(order, c)
		}
		counts[c] += v
	}
	siteOf := map[string]string{}
	for i := range devices {
		d := &devices[i]
		if d.Revoked {
			continue
		}
		site := ""
		if d.SiteID != nil {
			site = *d.SiteID
		}
		siteOf[d.ID] = site
		add(site, statKindSummary, "devices_total", "", 1)
		status := s.computeStatus(d)
		switch status {
		case "online":
			add(site, statKindSummary, "devices_online", "", 1)
		case "offline":
			add(site, statKindSummary, "devices_offline", "", 1)
		default:
			status = "unknown"
			add(site, statKindSummary, "devices_unknown", "", 1)
		}
		add(site, statKindSummary, "device_status", status, 1)
		add(site, statKindSummary, "failing_checks", "", float64(d.ChecksFailing))
		add(site, statKindSummary, "failing_tasks", "", float64(d.TasksFailing))
		if d.UpdatesCount != nil {
			add(site, statKindSummary, "pending_patches", "", float64(*d.UpdatesCount))
		}
		add(site, statKindSummary, "vulnerabilities", "", float64(d.VulnCount))
	}
	// Alle Kennzahlen je Standort auch mit 0 ausgeben, damit Verläufe keine Lücken haben.
	for _, site := range uniqueSites(siteOf) {
		for _, k := range summaryKeys {
			add(site, statKindSummary, k, "", 0)
		}
	}
	for _, c := range checks {
		site, ok := siteOf[c.DeviceID]
		if !ok {
			continue
		}
		st := c.Status
		if st != "passing" && st != "warning" && st != "failing" {
			st = "unknown"
		}
		add(site, statKindCheck, c.CheckID, st, 1)
	}
	ftype := map[string]string{}
	for _, f := range fields {
		ftype[f.ID] = f.Type
	}
	for _, v := range vals {
		site, ok := siteOf[v.EntityID]
		if !ok || strings.TrimSpace(v.Value) == "" {
			continue
		}
		for _, label := range fieldLabels(ftype[v.Field.ID], v.Value) {
			add(site, statKindField, v.Field.ID, label, 1)
		}
	}
	out := make([]store.StatSample, 0, len(order))
	for _, c := range order {
		out = append(out, store.StatSample{SiteID: c.site, Kind: c.kind, Key: c.key, Label: c.label, Value: counts[c]})
	}
	return out
}

func uniqueSites(siteOf map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range siteOf {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// fieldLabels macht aus einem gespeicherten Feldwert die zu zählenden Labels:
// Listen/Mehrfachauswahlen (JSON-Array) je Eintrag, alles andere der Wert selbst.
func fieldLabels(typ, value string) []string {
	if typ == "list" || typ == "multiselect" || strings.HasPrefix(strings.TrimSpace(value), "[") {
		var arr []any
		if err := json.Unmarshal([]byte(value), &arr); err == nil {
			out := make([]string, 0, len(arr))
			for _, x := range arr {
				if str, ok := x.(string); ok {
					if str = strings.TrimSpace(str); str != "" {
						out = append(out, str)
					}
				} else if b, err := json.Marshal(x); err == nil {
					out = append(out, string(b))
				}
			}
			return out
		}
	}
	return []string{strings.TrimSpace(value)}
}

// currentStats lädt den Ist-Zustand für die angegebenen Standorte (allowed nil = alle).
func (s *Server) currentStats(ctx context.Context, allowed map[string]bool) ([]store.StatSample, error) {
	devices, err := s.store.ListDevices(ctx, allowed)
	if err != nil {
		return nil, err
	}
	checks, err := s.store.AllCheckStatuses(ctx)
	if err != nil {
		return nil, err
	}
	fields, err := s.store.CustomFields(ctx, "device")
	if err != nil {
		return nil, err
	}
	vals, err := s.store.AllCustomFieldValues(ctx, "device", allowed)
	if err != nil {
		return nil, err
	}
	return s.computeStatSamples(devices, checks, fields, vals), nil
}

// SnapshotStats schreibt den Ist-Zustand als Statistik-Snapshot der aktuellen Stunde.
func (s *Server) SnapshotStats(ctx context.Context) error {
	samples, err := s.currentStats(ctx, nil)
	if err != nil {
		return err
	}
	ts := time.Now().Truncate(time.Hour).UnixMilli()
	return s.store.ReplaceStatSamples(ctx, ts, samples)
}

type statSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type statSources struct {
	Summary []string     `json:"summary"`
	Checks  []statSource `json:"checks"`
	Fields  []statSource `json:"fields"`
}

// handleStatSources liefert, was sich in einem Dashboard-Diagramm darstellen lässt:
// Kennzahlen, Checks (mit Richtlinie) und benutzerdefinierte Gerätefelder.
func (s *Server) handleStatSources(w http.ResponseWriter, r *http.Request) {
	out := statSources{Summary: append([]string{"device_status"}, summaryKeys...), Checks: []statSource{}, Fields: []statSource{}}
	if policies, err := s.store.ListPolicies(r.Context()); err == nil {
		for _, p := range policies {
			for _, c := range p.Checks {
				out.Checks = append(out.Checks, statSource{ID: c.ID, Name: c.Name + " (" + p.Name + ")", Type: c.Type})
			}
		}
	}
	if fields, err := s.store.CustomFields(r.Context(), "device"); err == nil {
		for _, f := range fields {
			out.Fields = append(out.Fields, statSource{ID: f.ID, Name: f.Name, Type: f.Type})
		}
	}
	sort.Slice(out.Checks, func(i, j int) bool { return out.Checks[i].Name < out.Checks[j].Name })
	sort.Slice(out.Fields, func(i, j int) bool { return out.Fields[i].Name < out.Fields[j].Name })
	s.writeJSON(w, http.StatusOK, out)
}

type statValue struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type statsResponse struct {
	Current []statValue       `json:"current"`
	History []store.StatPoint `json:"history,omitempty"`
}

// handleStats liefert für eine Quelle (source=summary|check|field, key=…) die aktuelle
// Verteilung und – mit since=<Unix-ms> – die Historie, beides im Daten-Scope des Nutzers.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	kind, key := r.URL.Query().Get("source"), r.URL.Query().Get("key")
	if (kind != statKindSummary && kind != statKindCheck && kind != statKindField) || key == "" || len(key) > 128 {
		http.Error(w, "source und key erforderlich", http.StatusBadRequest)
		return
	}
	sites, unrestricted := s.allowedSites(r.Context())
	allowed := sites
	if unrestricted {
		allowed = nil
	}
	samples, err := s.currentStats(r.Context(), allowed)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	resp := statsResponse{Current: []statValue{}}
	byLabel := map[string]float64{}
	var labels []string
	for _, p := range samples {
		if p.Kind != kind || p.Key != key {
			continue
		}
		if _, ok := byLabel[p.Label]; !ok {
			labels = append(labels, p.Label)
		}
		byLabel[p.Label] += p.Value
	}
	sort.Strings(labels)
	for _, l := range labels {
		resp.Current = append(resp.Current, statValue{Label: l, Value: byLabel[l]})
	}
	if since, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64); err == nil && since > 0 {
		hist, err := s.store.StatHistory(r.Context(), kind, key, since, allowed)
		if err != nil {
			s.mapStoreErr(w, err)
			return
		}
		resp.History = hist
	}
	s.writeJSON(w, http.StatusOK, resp)
}
