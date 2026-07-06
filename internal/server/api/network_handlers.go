package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/netscan"
	"github.com/thomaspeterson/pc-inventory/internal/snmp"

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

// handleSNMPPrinter liest einen Drucker per SNMP aus – entweder lokal (Server im
// Segment) oder über einen Agenten (dann Rückgabe einer command_id zum Pollen).
func (s *Server) handleSNMPPrinter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceID  string `json:"device_id"`
		IP        string `json:"ip"`
		Community string `json:"community"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.IP == "" {
		s.writeErr(w, http.StatusBadRequest, "ip erforderlich")
		return
	}
	if req.DeviceID == "server" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		info, err := snmp.Query(ctx, req.IP, req.Community)
		if err != nil {
			s.writeErr(w, http.StatusBadGateway, "SNMP fehlgeschlagen: "+err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, info)
		return
	}
	if req.DeviceID == "" {
		s.writeErr(w, http.StatusBadRequest, "device_id erforderlich")
		return
	}
	id, err := s.queueCommand(r.Context(), req.DeviceID, "snmp_query", "SNMP-Abfrage "+req.IP,
		map[string]any{"ip": req.IP, "community": req.Community})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleDeviceSNMP fragt einen (nicht verwalteten) Drucker aus der Geräteliste per
// SNMP ab – über einen online-Agenten im selben Standort.
func (s *Server) handleDeviceSNMP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	var ip string
	for _, i := range d.Interfaces {
		if v := strings.TrimSpace(strings.Split(i.IPv4, ",")[0]); v != "" {
			ip = v
			break
		}
	}
	if ip == "" {
		s.writeErr(w, http.StatusBadRequest, "keine IP-Adresse für dieses Gerät bekannt")
		return
	}
	if d.SiteID == nil || *d.SiteID == "" {
		s.writeErr(w, http.StatusBadRequest, "Gerät hat keinen Standort – SNMP braucht einen Agenten im selben Netz")
		return
	}
	var req struct {
		Community string `json:"community"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	agentID, err := s.store.OnlineNeighborInSite(r.Context(), *d.SiteID, id, time.Now().Add(-s.cfg.OfflineAfter))
	if err != nil {
		s.writeErr(w, http.StatusBadRequest, "kein online-Agent im Standort für die Abfrage")
		return
	}
	cmdID, err := s.queueCommand(r.Context(), agentID, "snmp_query", "SNMP-Abfrage "+ip,
		map[string]any{"ip": ip, "community": req.Community})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": cmdID})
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
