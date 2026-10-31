package api

import (
	"net/http"

	"github.com/boonkerz/roster/internal/server/store"
)

// scopeFilter liefert den Daten-Scope als Store-Filter: nil = unbeschränkt.
func (s *Server) scopeFilter(r *http.Request) map[string]bool {
	sites, unrestricted := s.allowedSites(r.Context())
	if unrestricted {
		return nil
	}
	return sites
}

// handlePendingPatches liefert die flottenweite Liste ausstehender Updates
// (GET /patches/pending?security=1&approved=1&q=&client=&site=).
func (s *Server) handlePendingPatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.store.PendingPatches(r.Context(), store.PatchFilter{
		Allowed:      s.scopeFilter(r),
		ClientID:     q.Get("client"),
		SiteID:       q.Get("site"),
		Query:        q.Get("q"),
		SecurityOnly: q.Get("security") == "1",
		ApprovedOnly: q.Get("approved") == "1",
	})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, rows)
}

// handlePatchSummary liefert die Kopfzahlen der Patch-Seite.
func (s *Server) handlePatchSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.store.PatchSummary(r.Context(), s.scopeFilter(r))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, sum)
}

type approvePatchesRequest struct {
	Items []struct {
		DeviceID string `json:"device_id"`
		Name     string `json:"name"`
	} `json:"items"`
	Approved bool `json:"approved"`
}

// handleApprovePatches genehmigt mehrere Patches auf mehreren Geräten in einem Zug
// (PUT /patches/approve). Geräte außerhalb des Daten-Scopes werden übersprungen.
func (s *Server) handleApprovePatches(w http.ResponseWriter, r *http.Request) {
	var req approvePatchesRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Items) == 0 {
		s.writeErr(w, http.StatusBadRequest, "items fehlt")
		return
	}
	allowed := s.scopeFilter(r)
	ids := make([]string, 0, len(req.Items))
	for _, it := range req.Items {
		ids = append(ids, it.DeviceID)
	}
	sites, err := s.store.DeviceSiteIDs(r.Context(), ids)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	changed, skipped := 0, 0
	for _, it := range req.Items {
		site, exists := sites[it.DeviceID]
		if it.Name == "" || !exists || (allowed != nil && !allowed[site]) {
			skipped++
			continue
		}
		if err := s.store.ApprovePatch(r.Context(), it.DeviceID, it.Name, req.Approved); err != nil {
			s.mapStoreErr(w, err)
			return
		}
		changed++
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"changed": changed, "skipped": skipped})
}
