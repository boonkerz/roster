package api

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

func TestComputeStatSamples(t *testing.T) {
	s := &Server{cfg: config.Config{OfflineAfter: 10 * time.Minute}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	now := time.Now()
	old := now.Add(-time.Hour)
	siteA, siteB := "a", "b"
	upd := 3
	devices := []model.Device{
		{ID: "d1", Managed: true, LastSeen: &now, SiteID: &siteA, ChecksFailing: 2, VulnCount: 1},
		{ID: "d2", Managed: true, LastSeen: &old, SiteID: &siteA, UpdatesCount: &upd},
		{ID: "d3", Managed: true, LastSeen: &now, SiteID: &siteB},
		{ID: "d4", Managed: true, LastSeen: &now, SiteID: &siteB, Revoked: true}, // zählt nicht
	}
	checks := []store.CheckStatus{
		{DeviceID: "d1", CheckID: "c1", Status: "failing"},
		{DeviceID: "d2", CheckID: "c1", Status: "passing"},
		{DeviceID: "d3", CheckID: "c1", Status: "komisch"},
		{DeviceID: "d4", CheckID: "c1", Status: "failing"}, // widerrufenes Gerät
	}
	fields := []model.CustomField{{ID: "f1", Type: "text"}, {ID: "f2", Type: "list"}}
	vals := []model.EntityFieldValue{
		{EntityID: "d1", CustomFieldValue: model.CustomFieldValue{Field: model.CustomField{ID: "f1"}, Value: "nginx:1.27"}},
		{EntityID: "d2", CustomFieldValue: model.CustomFieldValue{Field: model.CustomField{ID: "f1"}, Value: "nginx:1.27"}},
		{EntityID: "d3", CustomFieldValue: model.CustomFieldValue{Field: model.CustomField{ID: "f1"}, Value: "nginx:1.25"}},
		{EntityID: "d3", CustomFieldValue: model.CustomFieldValue{Field: model.CustomField{ID: "f2"}, Value: `["web","prod"]`}},
		{EntityID: "d1", CustomFieldValue: model.CustomFieldValue{Field: model.CustomField{ID: "f2"}, Value: `["web"]`}},
		{EntityID: "d1", CustomFieldValue: model.CustomFieldValue{Field: model.CustomField{ID: "f1"}, Value: "   "}}, // leer
	}
	got := map[string]float64{}
	for _, p := range s.computeStatSamples(devices, checks, fields, vals) {
		got[p.SiteID+"|"+p.Kind+"|"+p.Key+"|"+p.Label] += p.Value
	}
	want := map[string]float64{
		"a|summary|devices_total|": 2, "a|summary|devices_online|": 1, "a|summary|devices_offline|": 1, "a|summary|devices_unknown|": 0,
		"a|summary|device_status|online": 1, "a|summary|device_status|offline": 1,
		"a|summary|failing_checks|": 2, "a|summary|pending_patches|": 3, "a|summary|vulnerabilities|": 1, "a|summary|failing_tasks|": 0,
		"b|summary|devices_total|": 1, "b|summary|devices_online|": 1, "b|summary|device_status|online": 1,
		"b|summary|pending_patches|": 0,
		"a|check|c1|failing": 1, "a|check|c1|passing": 1, "b|check|c1|unknown": 1,
		"a|field|f1|nginx:1.27": 2, "b|field|f1|nginx:1.25": 1,
		"b|field|f2|web": 1, "b|field|f2|prod": 1, "a|field|f2|web": 1,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: erwartete %v, bekam %v", k, v, got[k])
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok && got[k] != 0 {
			t.Errorf("unerwartete Zeile %s=%v", k, got[k])
		}
	}
}

func TestFieldLabels(t *testing.T) {
	cases := []struct {
		typ, val string
		want     []string
	}{
		{"text", " 1.2.3 ", []string{"1.2.3"}},
		{"list", `["a"," b ",""]`, []string{"a", "b"}},
		{"text", `["x","y"]`, []string{"x", "y"}}, // JSON-Array auch ohne Listentyp
		{"list", `kein json`, []string{"kein json"}},
		{"checkbox", "true", []string{"true"}},
	}
	for _, c := range cases {
		got := fieldLabels(c.typ, c.val)
		if len(got) != len(c.want) {
			t.Fatalf("%q: erwartete %v, bekam %v", c.val, c.want, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%q: erwartete %v, bekam %v", c.val, c.want, got)
			}
		}
	}
}
