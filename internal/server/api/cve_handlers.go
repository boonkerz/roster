package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/cve"
	"github.com/thomaspeterson/pc-inventory/internal/server/model"
)

// handleScanCVE gleicht die installierte Software eines Geräts gegen OSV.dev ab und
// speichert die gefundenen Schwachstellen.
func (s *Server) handleScanCVE(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	sw := make([]cve.SW, 0, len(d.Software))
	for _, p := range d.Software {
		sw = append(sw, cve.SW{Name: p.Name, Version: p.Version})
	}
	// Unabhängig vom Request-Kontext (Scan kann länger dauern als der Client wartet).
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second}
	vulns, err := cve.Scan(ctx, client, sw, cve.Ecosystem(d.OS, d.OSVersion))
	if err != nil {
		s.writeErr(w, http.StatusBadGateway, "OSV.dev nicht erreichbar")
		return
	}
	mv := make([]model.Vulnerability, 0, len(vulns))
	for _, v := range vulns {
		mv = append(mv, model.Vulnerability{
			Package: v.Package, Version: v.Version, VulnID: v.ID,
			Severity: v.Severity, Summary: v.Summary, Fixed: v.Fixed, URL: v.URL,
		})
	}
	if err := s.store.ReplaceVulnerabilities(context.Background(), id, mv); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"count": len(mv)})
}

func (s *Server) handleDeviceVulns(w http.ResponseWriter, r *http.Request) {
	vulns, err := s.store.VulnerabilitiesFor(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if vulns == nil {
		vulns = []model.Vulnerability{}
	}
	s.writeJSON(w, http.StatusOK, vulns)
}

func (s *Server) handleAllVulns(w http.ResponseWriter, r *http.Request) {
	vulns, err := s.store.AllVulnerabilities(r.Context())
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if vulns == nil {
		vulns = []model.Vulnerability{}
	}
	s.writeJSON(w, http.StatusOK, vulns)
}
