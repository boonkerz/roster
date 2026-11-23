package api

import (
	"context"
	"time"
)

// RunBlockMaintenanceLoop pflegt die ip_blocks-Tabelle: abgelaufene Sperren als
// „expired“ nachtragen und beendete Sperren nach zwei Tagen löschen – stündlich, in
// Häppchen. Ohne das wuchs die Tabelle auf Millionen „wirksamer“ Zeilen, und jede
// Sicherheitsübersicht las sie komplett.
func (s *Server) RunBlockMaintenanceLoop(ctx context.Context) {
	run := func() {
		mctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		started := time.Now()
		expired, err := s.store.ExpireBlocks(mctx, time.Now())
		if err != nil {
			s.log.Warn("sperren: ablauf nachtragen", "err", err, "bisher", expired)
		}
		deleted, err := s.store.PruneEndedBlocks(mctx, time.Now().AddDate(0, 0, -2))
		if err != nil {
			s.log.Warn("sperren: aufräumen", "err", err, "bisher", deleted)
		}
		if expired > 0 || deleted > 0 {
			s.log.Info("sperren gepflegt", "abgelaufen", expired, "gelöscht", deleted, "dauer", time.Since(started).Round(time.Second))
		}
	}
	select {
	case <-time.After(time.Minute):
	case <-ctx.Done():
		return
	}
	run()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			run()
		case <-ctx.Done():
			return
		}
	}
}

// RunWarmLoop berechnet Sicherheitsübersicht und Dashboard-Kennzahlen für den
// uneingeschränkten Sichtbarkeitsbereich im Hintergrund vor, damit die Oberfläche nie auf
// die Berechnung wartet (Cache-Fristen: Übersicht 60 s, Kennzahlen 30 s).
func (s *Server) RunWarmLoop(ctx context.Context) {
	warm := func() {
		wctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if sum, err := s.store.SecuritySummary(wctx, nil); err == nil {
			s.secSumMu.Lock()
			if s.secSumCache == nil {
				s.secSumCache = map[string]secSumEntry{}
			}
			s.secSumCache["*"] = secSumEntry{sum: sum, at: time.Now()}
			s.secSumMu.Unlock()
		}
		if samples, err := s.computeCurrentStats(wctx, nil); err == nil {
			s.statsMu.Lock()
			if s.statsCache == nil {
				s.statsCache = map[string]statsEntry{}
			}
			s.statsCache["*"] = statsEntry{samples: samples, at: time.Now()}
			s.statsMu.Unlock()
		}
	}
	select {
	case <-time.After(20 * time.Second):
	case <-ctx.Done():
		return
	}
	warm()
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			warm()
		case <-ctx.Done():
			return
		}
	}
}
