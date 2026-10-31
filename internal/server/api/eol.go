package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/eol"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

const eolCacheTTL = 20 * time.Hour

// eolSettings liest Vorwarnzeit und Alarm-Schalter (Standard 90 Tage, Alarme an).
func (s *Server) eolSettings(ctx context.Context) (warnDays int, alerts bool) {
	warnDays, alerts = 90, true
	if v, _ := s.store.GetSetting(ctx, store.SettingEOLWarnDays); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			warnDays = n
		}
	}
	if v, _ := s.store.GetSetting(ctx, store.SettingEOLAlerts); v == "0" {
		alerts = false
	}
	return warnDays, alerts
}

// eolCycles liefert die Zyklen eines Produkts aus dem Cache (max. 20 h alt) oder frisch
// von endoflife.date; schlägt der Abruf fehl, bleibt ein alter Cache in Gebrauch.
func (s *Server) eolCycles(ctx context.Context, client *http.Client, product string, force bool) ([]eol.Cycle, error) {
	data, fetched, err := s.store.EOLCache(ctx, product)
	if err != nil {
		return nil, err
	}
	var cached []eol.Cycle
	if data != "" {
		_ = json.Unmarshal([]byte(data), &cached)
	}
	if len(cached) > 0 && !force && time.Since(fetched) < eolCacheTTL {
		return cached, nil
	}
	fresh, err := eol.Fetch(ctx, client, product)
	if err != nil {
		if len(cached) > 0 {
			s.log.Warn("eol: abruf fehlgeschlagen, nutze cache", "product", product, "err", err)
			return cached, nil
		}
		return nil, err
	}
	if b, err := json.Marshal(fresh); err == nil {
		_ = s.store.SetEOLCache(ctx, product, string(b))
	}
	return fresh, nil
}

// RefreshEOL bewertet alle Geräte neu und meldet Statuswechsel nach soon/eol.
// Liefert die Zahl bewerteter Geräte und Produkte, die nicht geladen werden konnten.
func (s *Server) RefreshEOL(ctx context.Context, force bool) (rated int, failed []string) {
	client := &http.Client{Timeout: 20 * time.Second}
	devices, err := s.store.ListDevices(ctx, nil)
	if err != nil {
		s.log.Warn("eol: geräte laden fehlgeschlagen", "err", err)
		return 0, nil
	}
	warnDays, alerts := s.eolSettings(ctx)
	cycles := map[string][]eol.Cycle{}
	failedSet := map[string]bool{}
	now := time.Now()
	for i := range devices {
		d := &devices[i]
		if d.Revoked || !d.Managed {
			continue
		}
		targets := eol.Resolve(eol.Device{OS: d.OS, OSVersion: d.OSVersion, OSBuild: d.OSBuild, OSCodename: d.OSCodename,
			OSEdition: d.OSEdition, ProxmoxVersion: d.ProxmoxVersion})
		seen := map[string]bool{}
		for _, t := range targets {
			seen[t.Kind] = true
			cs, ok := cycles[t.Product]
			if !ok && !failedSet[t.Product] {
				cs, err = s.eolCycles(ctx, client, t.Product, force)
				if err != nil {
					s.log.Warn("eol: produkt nicht ladbar", "product", t.Product, "err", err)
					failedSet[t.Product] = true
					failed = append(failed, t.Product)
					continue
				}
				cycles[t.Product] = cs
			}
			if failedSet[t.Product] {
				continue
			}
			c, found := eol.Match(cs, t.Candidates)
			entry := model.DeviceEOL{DeviceID: d.ID, Kind: t.Kind, Product: t.Product, Cycle: t.Candidates[0], Status: eol.StatusUnknown}
			if found {
				entry.Cycle, entry.Label, entry.EOLDate, entry.SupportDate = c.Name, c.Label, c.EOL, c.Support
				entry.Status = eol.Classify(c, now, warnDays)
			}
			prev, err := s.store.UpsertDeviceEOL(ctx, entry)
			if err != nil {
				s.log.Warn("eol: speichern fehlgeschlagen", "device", d.ID, "err", err)
				continue
			}
			rated++
			if alerts && (entry.Status == eol.StatusSoon || entry.Status == eol.StatusEOL) && prev != entry.Status {
				s.notifyEOL(ctx, d, entry)
				_ = s.store.MarkEOLAlerted(ctx, d.ID, t.Kind, entry.Status)
			} else if entry.Status == eol.StatusOK && prev != "" {
				_ = s.store.MarkEOLAlerted(ctx, d.ID, t.Kind, "")
			}
		}
		// Nicht mehr zuordenbare Einträge (z. B. OS gewechselt) entfernen.
		for _, kind := range []string{"os", "proxmox"} {
			if !seen[kind] {
				_ = s.store.DeleteDeviceEOL(ctx, d.ID, kind)
			}
		}
	}
	return rated, failed
}

// notifyEOL meldet ein nahendes/erreichtes Support-Ende über die Alarmkanäle.
func (s *Server) notifyEOL(ctx context.Context, d *model.Device, e model.DeviceEOL) {
	what := d.OS + " " + d.OSVersion
	if e.Kind == "proxmox" {
		what = "Proxmox VE " + d.ProxmoxVersion
	}
	date := "unbekannt"
	if e.EOLDate != nil {
		date = e.EOLDate.Format("02.01.2006")
	}
	sev, subject := "warning", fmt.Sprintf("[Roster] Support-Ende naht: %s (%s)", d.Hostname, what)
	body := fmt.Sprintf("Auf %q läuft %s (%s %s). Das Support-Ende ist am %s – danach gibt es keine Sicherheitsupdates mehr.", d.Hostname, what, e.Product, e.Cycle, date)
	if e.Status == eol.StatusEOL {
		sev, subject = "critical", fmt.Sprintf("[Roster] Support-Ende erreicht: %s (%s)", d.Hostname, what)
		body = fmt.Sprintf("Auf %q läuft %s (%s %s). Der Support ist seit %s beendet – keine Sicherheitsupdates mehr. Upgrade einplanen.", d.Hostname, what, e.Product, e.Cycle, date)
	}
	s.dispatchDeviceAlert(ctx, d, sev, subject, body)
}

// RunEOLLoop bewertet kurz nach dem Start und danach täglich.
func (s *Server) RunEOLLoop(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Minute):
	}
	run := func() {
		rated, failed := s.RefreshEOL(ctx, false)
		s.log.Info("eol-bewertung fertig", "geräte", rated, "fehlgeschlagen", failed)
	}
	run()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// handleEOLFleet liefert die Lebenszyklus-Bewertung aller Geräte (GET /eol).
func (s *Server) handleEOLFleet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.EOLFleet(r.Context(), s.scopeFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleDeviceEOL liefert die Bewertung eines Geräts.
func (s *Server) handleDeviceEOL(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DeviceEOLFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handleEOLRefresh stößt eine sofortige Neubewertung an (POST /eol/refresh, Einstellungen).
func (s *Server) handleEOLRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	rated, failed := s.RefreshEOL(ctx, true)
	if failed == nil {
		failed = []string{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"rated": rated, "failed": failed})
}
