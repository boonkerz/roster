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
		var expired, deleted int64
		// Runden zu 25.000 Zeilen (~500 MB WAL), dazwischen WAL kürzen: ohne das wuchs das
		// Log beim ersten Lauf auf dem Live-System um ~400 MB je 20.000 Zeilen und hätte
		// bei 2,3 Millionen Altlasten die Platte gefüllt.
		const round = 25000
		for {
			n, done, err := s.store.ExpireBlocks(mctx, time.Now(), round)
			expired += n
			if err != nil {
				s.log.Warn("sperren: ablauf nachtragen", "err", err, "bisher", expired)
				break
			}
			s.checkpoint(mctx)
			if done {
				break
			}
		}
		for mctx.Err() == nil {
			n, done, err := s.store.PruneEndedBlocks(mctx, time.Now().AddDate(0, 0, -2), round)
			deleted += n
			if err != nil {
				s.log.Warn("sperren: aufräumen", "err", err, "bisher", deleted)
				break
			}
			s.checkpoint(mctx)
			if done {
				break
			}
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
	ck := time.NewTicker(10 * time.Minute)
	defer ck.Stop()
	for {
		select {
		case <-t.C:
			run()
		case <-ck.C:
			s.checkpoint(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// checkpoint schreibt das SQLite-WAL zurück und kürzt es. Der automatische Checkpoint kann
// das Log unter Dauerlast nie zurücksetzen (immer ein Leser aktiv) – live wuchs es auf
// 2,8 GB, und jede Seitensuche musste durch das ganze Log.
func (s *Server) checkpoint(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	frames, busy, err := s.store.CheckpointWAL(cctx)
	switch {
	case err != nil:
		s.log.Warn("wal-checkpoint", "err", err)
	case frames > 25000 || busy: // > 100 MB Log oder nicht vollständig
		s.log.Info("wal-checkpoint", "frames", frames, "busy", busy)
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
