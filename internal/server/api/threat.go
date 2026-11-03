package api

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/threat"
)

// threatCacheTTL: so lange gilt ein gespeicherter Feed als frisch.
const threatCacheTTL = 20 * time.Hour

// RefreshThreat lädt die aktivierten Blocklisten (aus dem Cache, wenn frisch; sonst aus dem
// Netz – bei Fehlern bleibt der alte Stand) und installiert den Index. Mit
// force=false und network=false (Start) wird nur der Cache gelesen.
func (s *Server) RefreshThreat(ctx context.Context, force bool) (loaded map[string]int, failed []string) {
	return s.refreshThreat(ctx, force, true)
}

func (s *Server) refreshThreat(ctx context.Context, force, network bool) (map[string]int, []string) {
	cfg := s.securitySettings(ctx)
	loaded := map[string]int{}
	if !cfg.ThreatEnabled {
		threat.Install(nil)
		return loaded, nil
	}
	enabled := map[string]bool{}
	for _, id := range cfg.ThreatFeeds {
		enabled[id] = true
	}
	client := &http.Client{Timeout: 60 * time.Second}
	ix := threat.NewIndex()
	var failed []string
	for _, f := range threat.Feeds {
		if len(enabled) > 0 && !enabled[f.ID] {
			continue
		}
		row, _ := s.store.ThreatFeed(ctx, f.ID)
		body := ""
		if row != nil {
			body = row.Data
		}
		stale := row == nil || row.Data == "" || time.Since(row.FetchedAt) > threatCacheTTL
		if network && (force || stale) {
			if b, err := threat.Fetch(ctx, client, f); err != nil {
				s.log.Warn("bedrohungsdaten: feed nicht ladbar", "feed", f.ID, "err", err)
				_ = s.store.SetThreatFeedError(ctx, f.ID, err.Error())
				failed = append(failed, f.ID)
			} else {
				ips, nets := threat.ParseFeed(b)
				_ = s.store.SetThreatFeed(ctx, f.ID, string(b), len(ips)+len(nets))
				body = string(b)
			}
		}
		if body == "" {
			continue
		}
		ips, nets := threat.ParseFeed([]byte(body))
		ix.Add(f.Name, ips, nets)
		loaded[f.ID] = len(ips) + len(nets)
	}
	threat.Install(ix)
	return loaded, failed
}

// RunThreatLoop lädt die Feeds beim Start aus dem Cache, nach zwei Minuten aus dem Netz
// und danach täglich.
func (s *Server) RunThreatLoop(ctx context.Context) {
	if loaded, _ := s.refreshThreat(ctx, false, false); len(loaded) > 0 {
		s.log.Info("bedrohungsdaten aus cache", "feeds", len(loaded))
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}
	run := func() {
		loaded, failed := s.RefreshThreat(ctx, false)
		s.log.Info("bedrohungsdaten aktualisiert", "feeds", len(loaded), "fehlgeschlagen", failed)
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

// handleThreatStatus liefert die Feeds mit Stand, Einträgen und Fehlern.
func (s *Server) handleThreatStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.securitySettings(r.Context())
	rows, err := s.store.ThreatFeeds(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	enabled := map[string]bool{}
	for _, id := range cfg.ThreatFeeds {
		enabled[id] = true
	}
	out := make([]model.ThreatFeedStatus, 0, len(threat.Feeds))
	for _, f := range threat.Feeds {
		st := model.ThreatFeedStatus{ID: f.ID, Name: f.Name, URL: f.URL, Enabled: cfg.ThreatEnabled && (len(enabled) == 0 || enabled[f.ID])}
		if row, ok := rows[f.ID]; ok {
			st.Entries, st.Error = row.Entries, row.Error
			if !row.FetchedAt.IsZero() {
				t := row.FetchedAt
				st.FetchedAt = &t
			}
		}
		out = append(out, st)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"enabled": cfg.ThreatEnabled, "block": cfg.ThreatBlock, "active": threat.Active(), "feeds": out})
}

// handleThreatLookup prüft eine IP gegen den Index (?ip=).
func (s *Server) handleThreatLookup(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))
	if net.ParseIP(ip) == nil {
		s.writeErr(w, http.StatusBadRequest, "ungültige IP")
		return
	}
	feeds := threat.Lookup(ip)
	if feeds == nil {
		feeds = []string{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ip": ip, "feeds": feeds})
}

// handleThreatRefresh lädt alle Feeds sofort neu (Admin).
func (s *Server) handleThreatRefresh(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	loaded, failed := s.RefreshThreat(ctx, true)
	if failed == nil {
		failed = []string{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"loaded": loaded, "failed": failed})
}
