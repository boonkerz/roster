package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/alert"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/report"
)

// buildHealthReport erzeugt den Health-Bericht über alle Geräte (mit Status).
func (s *Server) buildHealthReport(r *http.Request) (report.Report, error) {
	devices, err := s.store.ListDevices(r.Context(), nil)
	if err != nil {
		return report.Report{}, err
	}
	for i := range devices {
		devices[i].Status = s.computeStatus(&devices[i])
	}
	return report.Build("Health-Bericht", devices), nil
}

// handleHealthReport liefert den Health-Bericht als HTML (zum Ansehen/Drucken).
func (s *Server) handleHealthReport(w http.ResponseWriter, r *http.Request) {
	rep, err := s.buildHealthReport(r)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(rep.HTML()))
}

func (s *Server) handleListReportSchedules(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListReportSchedules(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, items)
}

type reportScheduleRequest struct {
	Title     string `json:"title"`
	Frequency string `json:"frequency"`
	ChannelID string `json:"channel_id"`
}

func (s *Server) handleCreateReportSchedule(w http.ResponseWriter, r *http.Request) {
	var req reportScheduleRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.ChannelID == "" {
		s.writeErr(w, http.StatusBadRequest, "kanal erforderlich")
		return
	}
	if req.Frequency != "daily" && req.Frequency != "weekly" && req.Frequency != "monthly" {
		req.Frequency = "daily"
	}
	if req.Title == "" {
		req.Title = "Health-Bericht"
	}
	rs := &model.ReportSchedule{Title: req.Title, Frequency: req.Frequency, ChannelID: req.ChannelID}
	if err := s.store.CreateReportSchedule(r.Context(), rs); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, rs)
}

func (s *Server) handleDeleteReportSchedule(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteReportSchedule(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RunReportLoop versendet fällige geplante Berichte (einmal beim Start, dann stündlich).
func (s *Server) RunReportLoop(ctx context.Context) {
	tick := func() {
		now := time.Now()
		due, err := s.store.DueReportSchedules(ctx, now)
		if err != nil || len(due) == 0 {
			return
		}
		devices, err := s.store.ListDevices(ctx, nil)
		if err != nil {
			return
		}
		for i := range devices {
			devices[i].Status = s.computeStatus(&devices[i])
		}
		for _, rs := range due {
			ch, err := s.store.AlertChannel(ctx, rs.ChannelID)
			if err != nil || !ch.Enabled {
				s.log.Warn("bericht-kanal nicht verfügbar", "schedule", rs.ID)
				continue
			}
			rep := report.Build(rs.Title, devices)
			alert.Dispatch(s.log, ch, alert.Notification{
				Subject: "[Roster] " + rs.Title,
				Body:    rep.Text(),
			})
			_ = s.store.MarkReportRun(ctx, rs.ID, now)
			s.log.Info("bericht versendet", "schedule", rs.ID, "kanal", ch.Name)
		}
	}
	tick()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
