package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/netscan"

	"github.com/thomaspeterson/pc-inventory/internal/server/model"
	"github.com/thomaspeterson/pc-inventory/internal/shared"
)

// handleStartNetworkScan lässt einen Agenten (im Zielsegment) ein CIDR scannen; die
// Funde werden bei Rückkehr des Ergebnisses in die gewählte Site importiert.
func (s *Server) handleStartNetworkScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID string `json:"device_id"`
		CIDR     string `json:"cidr"`
		SiteID   string `json:"site_id"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.CIDR == "" || req.SiteID == "" {
		s.writeErr(w, http.StatusBadRequest, "cidr und site_id erforderlich")
		return
	}
	// "server" = der Inventory-Server scannt selbst (wenn er im Segment hängt).
	if req.DeviceID == "server" {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		hosts, err := netscan.Scan(ctx, req.CIDR)
		if err != nil {
			s.writeErr(w, http.StatusBadRequest, "Scan fehlgeschlagen: "+err.Error())
			return
		}
		n, err := s.store.UpsertNetworkAssets(r.Context(), req.SiteID, hosts)
		if err != nil {
			s.mapStoreErr(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]int{"imported": n})
		return
	}
	if req.DeviceID == "" {
		s.writeErr(w, http.StatusBadRequest, "device_id erforderlich")
		return
	}
	id, err := s.queueCommand(r.Context(), req.DeviceID, "network_scan", "Netzwerk-Scan "+req.CIDR,
		map[string]any{"cidr": req.CIDR, "site_id": req.SiteID})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

func (s *Server) handleListSiteAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.store.NetworkAssetsForSite(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if assets == nil {
		assets = []model.NetworkAsset{}
	}
	s.writeJSON(w, http.StatusOK, assets)
}

func (s *Server) handleSetAssetNote(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetNetworkAssetNote(r.Context(), chi.URLParam(r, "id"), req.Note); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"ok": "1"})
}

// handleAdoptAsset übernimmt ein Asset als nicht verwaltetes Gerät.
func (s *Server) handleAdoptAsset(w http.ResponseWriter, r *http.Request) {
	id, err := s.store.AdoptNetworkAsset(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"device_id": id})
}

// handleAdoptAllAssets übernimmt alle noch nicht verwalteten Assets einer Site.
func (s *Server) handleAdoptAllAssets(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.AdoptAllForSite(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]int{"adopted": n})
}

func (s *Server) handleDeleteAsset(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteNetworkAsset(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// importNetworkScans importiert die Ergebnisse abgeschlossener Netzwerk-Scans in die
// jeweils angeforderte Site.
func (s *Server) importNetworkScans(ctx context.Context, results []shared.CommandResult) {
	for _, res := range results {
		typ, payload, err := s.store.CommandMeta(ctx, res.CommandID)
		if err != nil || typ != "network_scan" || res.ExitCode != 0 {
			continue
		}
		siteID, _ := payload["site_id"].(string)
		if siteID == "" {
			continue
		}
		var hosts []shared.NetworkHost
		if json.Unmarshal([]byte(res.Output), &hosts) != nil {
			continue
		}
		if _, err := s.store.UpsertNetworkAssets(ctx, siteID, hosts); err != nil {
			s.log.Error("netzwerk-scan import", "err", err)
		}
	}
}
