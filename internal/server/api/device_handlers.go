package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/boonkerz/roster/internal/server/model"
)

// computeStatus leitet online/offline aus last_seen und der Offline-Schwelle ab.
func (s *Server) computeStatus(d *model.Device) string {
	if !d.Managed {
		return "unmanaged"
	}
	if d.LastSeen == nil {
		return "unknown"
	}
	if time.Since(*d.LastSeen) <= s.cfg.OfflineAfter {
		return "online"
	}
	return "offline"
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	sites, unrestricted := s.allowedSites(r.Context())
	var devices []model.Device
	var err error
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		devices, err = s.store.SearchDevices(r.Context(), q)
		if err == nil && !unrestricted {
			devices = filterBySites(devices, sites) // Suche nachträglich auf den Scope einschränken
		}
	} else {
		filter := sites
		if unrestricted {
			filter = nil
		}
		devices, err = s.store.ListDevices(r.Context(), filter)
	}
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	for i := range devices {
		devices[i].Status = s.computeStatus(&devices[i])
	}
	s.writeJSON(w, http.StatusOK, devices)
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	device, err := s.store.GetDevice(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	device.Status = s.computeStatus(device)
	s.writeJSON(w, http.StatusOK, device)
}

func (s *Server) handleDeviceHistory(w http.ResponseWriter, r *http.Request) {
	snaps, err := s.store.InventoryHistory(r.Context(), chi.URLParam(r, "id"), 50)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, snaps)
}

// handleDashboard liefert die aggregierte Übersicht über alle Geräte.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	sites, unrestricted := s.allowedSites(r.Context())
	filter := sites
	if unrestricted {
		filter = nil
	}
	devices, err := s.store.ListDevices(r.Context(), filter)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	var sum model.DashboardSummary
	for i := range devices {
		d := &devices[i]
		if d.Revoked {
			continue
		}
		sum.DevicesTotal++
		switch s.computeStatus(d) {
		case "online":
			sum.DevicesOnline++
		case "offline":
			sum.DevicesOffline++
		default:
			sum.DevicesUnknown++
		}
		if d.ChecksFailing > 0 {
			sum.DevicesWithFailingChecks++
			sum.FailingChecks += d.ChecksFailing
		}
		if d.TasksFailing > 0 {
			sum.DevicesWithFailingTasks++
			sum.FailingTasks += d.TasksFailing
		}
		if d.UpdatesCount != nil && *d.UpdatesCount > 0 {
			sum.DevicesWithPendingPatches++
			sum.PendingPatches += *d.UpdatesCount
		}
		if d.VulnCount > 0 {
			sum.DevicesWithVulns++
			sum.Vulnerabilities += d.VulnCount
		}
	}
	if events, err := s.store.RecentCheckEvents(r.Context(), 20); err == nil {
		sum.RecentEvents = events
	}
	s.writeJSON(w, http.StatusOK, sum)
}

type deviceNotesRequest struct {
	Notes string `json:"notes"`
}

// handleSetDeviceNotes speichert die Freitext-Notizen eines Geräts.
func (s *Server) handleSetDeviceNotes(w http.ResponseWriter, r *http.Request) {
	var req deviceNotesRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetDeviceNotes(r.Context(), chi.URLParam(r, "id"), req.Notes); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleDeviceEvents liefert den Check-Statuswechsel-Verlauf eines Geräts.
func (s *Server) handleDeviceEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.CheckEventsFor(r.Context(), chi.URLParam(r, "id"), 100)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, events)
}

// handleDeviceTaskRuns liefert die letzten Task-Läufe eines Geräts (Historie).
func (s *Server) handleDeviceTaskRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.TaskResultsFor(r.Context(), chi.URLParam(r, "id"), 100)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, runs)
}

// handleDeviceSoftwareEvents liefert die letzten Software-Änderungen eines Geräts.
func (s *Server) handleDeviceSoftwareEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.SoftwareEventsFor(r.Context(), chi.URLParam(r, "id"), 100)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, events)
}

type scanDirRequest struct {
	Path string `json:"path"`
}

// handleScanDir reiht einen On-Demand-Verzeichnis-Scan (TreeSize) für ein Gerät
// ein. Das Ergebnis (JSON) liefert der Agent als Befehls-Ergebnis; der Client
// pollt es über GET /commands/{id} ab.
func (s *Server) handleScanDir(w http.ResponseWriter, r *http.Request) {
	var req scanDirRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		s.writeErr(w, http.StatusBadRequest, "pfad fehlt")
		return
	}
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "dir_usage", "Speicher-Scan: "+req.Path,
		map[string]any{"path": req.Path})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id, "status": "eingereiht"})
}

// handleListServices reiht das Auflisten der Systemdienste ein (Ergebnis per Polling).
func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "list_services", "Dienste auflisten", nil)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// filterBySites behält nur Geräte, deren Standort im erlaubten Set liegt (Daten-Scope).
func filterBySites(devices []model.Device, sites map[string]bool) []model.Device {
	out := devices[:0]
	for _, d := range devices {
		if d.SiteID != nil && sites[*d.SiteID] {
			out = append(out, d)
		}
	}
	return out
}

// handleListResolutions reiht das Auflisten der verfügbaren Bildschirmauflösungen ein.
func (s *Server) handleListResolutions(w http.ResponseWriter, r *http.Request) {
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "list_resolutions", "Auflösungen auflisten", nil)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleSetResolution setzt die Bildschirmauflösung des Primärdisplays.
func (s *Server) handleSetResolution(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Width < 640 || req.Height < 480 {
		s.writeErr(w, http.StatusBadRequest, "ungültige Auflösung")
		return
	}
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "set_resolution",
		fmt.Sprintf("Auflösung %dx%d", req.Width, req.Height),
		map[string]any{"width": req.Width, "height": req.Height})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// guestToolsScript lädt die VirtIO-/SPICE-Gasttools (Anzeigetreiber für VMs) herunter
// und installiert sie still. Nach der Installation bietet Windows mehrere Auflösungen an.
const guestToolsScript = `$ErrorActionPreference='Stop'
[Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12
$url='https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win-guest-tools.exe'
$out="$env:TEMP\virtio-win-guest-tools.exe"
Write-Output "Lade $url"
Invoke-WebRequest -Uri $url -OutFile $out -UseBasicParsing
Write-Output "Installiere (still)..."
$p=Start-Process -FilePath $out -ArgumentList '/install','/quiet','/norestart' -Wait -PassThru
Write-Output ("Fertig, ExitCode " + $p.ExitCode + " – ggf. Neustart erforderlich.")`

// handleInstallGuestTools installiert die VM-Gasttools (VirtIO-GPU/QXL) per PowerShell,
// damit die Auflösung nicht mehr ausgegraut ist (nur Windows-Gäste).
func (s *Server) handleInstallGuestTools(w http.ResponseWriter, r *http.Request) {
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "run_script", "Gast-Grafiktreiber installieren",
		map[string]any{"shell": "powershell", "script": guestToolsScript, "platforms": []string{"windows"}})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleRunCheck stößt eine sofortige Neuauswertung eines einzelnen Checks an
// (unabhängig vom Zeitplan). Ergebnis kommt beim nächsten Checkin.
func (s *Server) handleRunCheck(w http.ResponseWriter, r *http.Request) {
	checkID := chi.URLParam(r, "checkID")
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "run_check", "Check neu ausführen",
		map[string]any{"check_id": checkID})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleRunTask stößt einen sofortigen Neustart eines einzelnen Tasks an.
func (s *Server) handleRunTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "run_task", "Task neu starten",
		map[string]any{"task_id": taskID})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleMetrics reiht eine Live-Auslastungs-Momentaufnahme ein (Ergebnis per Polling).
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "metrics", "Auslastung", nil)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleListProcesses reiht das Auflisten der Prozesse ein (Ergebnis per Polling).
func (s *Server) handleListProcesses(w http.ResponseWriter, r *http.Request) {
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "list_processes", "Prozesse auflisten", nil)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

type serviceControlRequest struct {
	Name   string `json:"name"`
	Action string `json:"action"` // start | stop | restart
}

// handleServiceControl reiht eine Dienst-Aktion (start/stop/restart) ein.
func (s *Server) handleServiceControl(w http.ResponseWriter, r *http.Request) {
	var req serviceControlRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.Name == "" || (req.Action != "start" && req.Action != "stop" && req.Action != "restart") {
		s.writeErr(w, http.StatusBadRequest, "name und action (start|stop|restart) erforderlich")
		return
	}
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "service_control",
		req.Action+" "+req.Name, map[string]any{"name": req.Name, "action": req.Action})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

type processKillRequest struct {
	PID int `json:"pid"`
}

// handleProcessKill reiht das Beenden eines Prozesses ein.
func (s *Server) handleProcessKill(w http.ResponseWriter, r *http.Request) {
	var req processKillRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if req.PID <= 0 {
		s.writeErr(w, http.StatusBadRequest, "gültige pid erforderlich")
		return
	}
	id, err := s.queueCommand(r.Context(), chi.URLParam(r, "id"), "process_kill",
		fmt.Sprintf("Prozess %d beenden", req.PID), map[string]any{"pid": req.PID})
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]string{"command_id": id})
}

// handleWake weckt ein (offlines) Gerät per Wake-on-LAN: ein online Nachbar im
// selben Standort sendet das Magic Packet an die MAC des Zielgeräts.
func (s *Server) handleWake(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	dev, err := s.store.GetDevice(r.Context(), targetID)
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	var mac string
	for _, i := range dev.Interfaces {
		if i.MAC != "" {
			mac = i.MAC
			break
		}
	}
	if mac == "" {
		s.writeErr(w, http.StatusBadRequest, "keine MAC-Adresse für dieses Gerät bekannt")
		return
	}

	// 1) Best effort: der Server broadcastet das Magic-Packet direkt (erreicht das
	//    lokale Segment des Servers – reicht in flachen Netzen ohne Nachbar-Agent).
	via := []string{}
	if err := sendWOL(mac); err == nil {
		via = append(via, "Server-Broadcast")
	}

	// 2) Zusätzlich, für andere Subnetze: ein Online-Nachbar im selben Standort schickt
	//    das Packet in seinem Segment. Fehlt einer, ist das kein Fehler.
	commandID := ""
	if dev.SiteID != nil && *dev.SiteID != "" {
		if waker, err := s.store.OnlineNeighborInSite(r.Context(), *dev.SiteID, targetID, time.Now().Add(-s.cfg.OfflineAfter)); err == nil {
			if id, qerr := s.queueCommand(r.Context(), waker, "wake_lan", "WoL an "+mac, map[string]any{"mac": mac}); qerr == nil {
				commandID = id
				via = append(via, "Nachbar-Agent")
			}
		}
	}

	if len(via) == 0 {
		s.writeErr(w, http.StatusConflict, "WoL fehlgeschlagen: kein Broadcast möglich und kein Nachbar im Standort online")
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"command_id": commandID, "mac": mac, "via": via})
}

// handleGetCommand liefert einen einzelnen Befehl inkl. Ergebnis (für Polling).
func (s *Server) handleGetCommand(w http.ResponseWriter, r *http.Request) {
	cmd, err := s.store.CommandByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, cmd)
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDevice(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "gelöscht"})
}

func (s *Server) handleRevokeDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RevokeDevice(r.Context(), chi.URLParam(r, "id")); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "widerrufen"})
}

type setGroupsRequest struct {
	GroupIDs []string `json:"group_ids"`
}

func (s *Server) handleSetDeviceGroups(w http.ResponseWriter, r *http.Request) {
	var req setGroupsRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetDeviceGroups(r.Context(), chi.URLParam(r, "id"), req.GroupIDs); err != nil {
		s.mapStoreErr(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "aktualisiert"})
}
