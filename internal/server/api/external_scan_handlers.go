package api

import (
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/thomaspeterson/pc-inventory/internal/server/store"
)

// externalScanTimeout begrenzt jeden einzelnen Verbindungsversuch.
const externalScanTimeout = 3 * time.Second

// handleExternalScan testet die öffentliche IP des Geräts von außen (der Server läuft
// außerhalb des Kunden-LANs) auf den vom Agent gemeldeten öffentlichen TCP-Ports und
// zeigt so, was NAT/Firewall tatsächlich durchlässt. Nur TCP – UDP lässt sich per
// Connect nicht zuverlässig prüfen.
func (s *Server) handleExternalScan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	dev, err := s.store.GetDevice(r.Context(), id)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	if dev.PublicIP == "" {
		s.writeErr(w, http.StatusBadRequest, "keine öffentliche IP bekannt (Gerät hat noch nicht eingecheckt)")
		return
	}
	// Distinkte öffentliche TCP-Ports sammeln.
	portSet := map[int]bool{}
	for _, p := range dev.ListenPorts {
		if p.Public && (p.Proto == "tcp" || p.Proto == "tcp6") {
			portSet[p.Port] = true
		}
	}
	if len(portSet) == 0 {
		s.writeErr(w, http.StatusBadRequest, "keine öffentlichen TCP-Ports zum Testen")
		return
	}
	ports := make([]int, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	sort.Ints(ports)

	results := scanTCP(dev.PublicIP, ports)
	if err := s.store.SaveExternalPortResults(r.Context(), id, results); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	reachable := 0
	for _, res := range results {
		if res.Reachable {
			reachable++
		}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"public_ip":  dev.PublicIP,
		"tested":     len(results),
		"reachable":  reachable,
		"checked_at": time.Now().UTC(),
	})
}

// scanTCP versucht parallel, sich zu ip:port zu verbinden (begrenzte Nebenläufigkeit).
func scanTCP(ip string, ports []int) []store.ExternalPortResult {
	out := make([]store.ExternalPortResult, len(ports))
	sem := make(chan struct{}, 20)
	var wg sync.WaitGroup
	for i, port := range ports {
		wg.Add(1)
		go func(i, port int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			addr := net.JoinHostPort(ip, strconv.Itoa(port))
			conn, err := net.DialTimeout("tcp", addr, externalScanTimeout)
			ok := err == nil
			if conn != nil {
				conn.Close()
			}
			out[i] = store.ExternalPortResult{Port: port, Reachable: ok}
		}(i, port)
	}
	wg.Wait()
	return out
}
