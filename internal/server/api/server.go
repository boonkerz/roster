// Package api implementiert die HTTP-Schnittstelle (Agent- und Web-API).
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/boonkerz/roster/internal/server/config"
	"github.com/boonkerz/roster/internal/server/model"
	"github.com/boonkerz/roster/internal/server/store"
)

// Server bündelt Abhängigkeiten der HTTP-Handler.
type Server struct {
	store    *store.Store
	cfg      config.Config
	log      *slog.Logger
	webFS    fs.FS           // gebautes React-Frontend (kann nil sein)
	version  string          // Version der eingebetteten Agent-Binaries (für Auto-Update)
	term     *termHub        // flüchtiger Zustand für Remote-Terminal-Sessions
	files    *fileHub        // flüchtige Datei-Übertragungen
	filePool *filePool       // direkte Datei-Sitzungen zu Agents (Web-Dateibrowser)
	limiter  *attemptLimiter // Fehlversuche bei Anmeldung/Enrollment
	pwSem    chan struct{}   // gleichzeitige argon2-Prüfungen
	fileCaps *fileCapHub     // Datei-Capabilities des nativen Viewers (Token→Gerät)
	router   http.Handler

	secSumMu    sync.Mutex // schützt secSumCache (siehe securitySummary)
	secSumCache map[string]secSumEntry
	statsMu     sync.Mutex // schützt statsCache (siehe currentStats)
	statsCache  map[string]statsEntry

	responses     chan responseJob // Aufträge für den seriellen Reaktions-Worker (Sperren usw.)
	imageVulnKick chan struct{}    // stößt den OSV-Abgleich der Container-Images an

	tzMu   sync.RWMutex   // schützt den Zeitzonen-Cache (siehe timezone.go)
	tzName string         // zuletzt gelesener IANA-Name aus app_settings
	tzLoc  *time.Location // dazu aufgelöste Zone
}

// New erstellt den Server und registriert alle Routen.
func New(st *store.Store, cfg config.Config, log *slog.Logger, webFS fs.FS, version string) *Server {
	s := &Server{store: st, cfg: cfg, log: log, webFS: webFS, version: version, term: newTermHub(), files: newFileHub(spoolDirFor(cfg.DatabaseURL)), filePool: newFilePool(), limiter: newAttemptLimiter(10, 15*time.Minute, 15*time.Minute), pwSem: make(chan struct{}, 4), fileCaps: newFileCapHub(), responses: make(chan responseJob, 4096), imageVulnKick: make(chan struct{}, 1)}
	s.router = s.routes()
	go s.responseWorker()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.securityHeaders)
	r.Use(s.logSlowRequests)

	r.Route("/api/v1", func(r chi.Router) {
		// --- Realtime: Wake-Poll + Terminal-WebSockets (KEIN Request-Timeout) ---
		r.Group(func(r chi.Router) {
			r.Group(func(r chi.Router) {
				r.Use(s.requireAgent)
				r.Get("/agent/wait", s.handleAgentWait)
				r.Get("/agent/terminal", s.handleAgentTerminal)
			})
			r.Group(func(r chi.Router) {
				r.Use(s.requireUser)
				r.Use(s.requireAdmin)
				r.Get("/devices/{id}/terminal", s.handleDeviceTerminal)
				r.Post("/devices/{id}/remote/start", s.handleRemoteStart)
				r.Get("/devices/{id}/remote/ws", s.handleDeviceVNC)
			})
			// Nativer Viewer (roster-viewer): keine Cookie-Session, Auth über das
			// pro-Sitzung erzeugte Viewer-Token (nur ein Admin kann es via
			// /remote/start erzeugen). Sonst identisch zur Browser-WS.
			r.Get("/devices/{id}/remote/viewer-ws", s.handleViewerVNC)
			// Dateimanager im nativen Viewer: dieselbe Command-Queue wie die Web-UI,
			// aber Auth über die Viewer-Datei-Capability (Token→Gerät, gerätegebunden).
			r.Post("/devices/{id}/viewer-files/browse", s.handleViewerBrowse)
			r.Post("/devices/{id}/viewer-files/read", s.handleViewerReadFile)
			r.Post("/devices/{id}/viewer-files/write", s.handleViewerWriteFile)
			r.Post("/devices/{id}/viewer-files/mkdir", s.handleViewerMkdir)
			r.Post("/devices/{id}/viewer-files/delete", s.handleViewerDelete)
			r.Get("/devices/{id}/viewer-files/command/{cmd}", s.handleViewerCommand)
			r.Get("/devices/{id}/viewer-files/blob/{cmd}", s.handleViewerBlob)
			// Direkter Datei-Kanal (Viewer ↔ Agent durch den Tunnel, ab Agent 0.29.0).
			r.Post("/devices/{id}/viewer-files/session", s.handleViewerFileSession)
			r.Get("/devices/{id}/viewer-files/ws", s.handleViewerFileWS)
		})

		// --- Übrige API mit 30s-Request-Timeout ---
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(30 * time.Second))

			// --- Agent-API (Bearer-Token) ---
			r.Post("/agent/enroll", s.handleEnroll)
			r.Group(func(r chi.Router) {
				r.Use(s.requireAgent)
				r.Post("/agent/checkin", s.handleCheckin)
				r.Post("/agent/command-progress", s.handleCommandProgress)
				r.Post("/agent/file-upload", s.handleAgentFileUpload)
				r.Get("/agent/file-download", s.handleAgentFileDownload)
			})

			// --- Agent-Download (öffentlich, für Install-Skripte) ---
			r.Get("/agents/{platform}", s.handleAgentDownload)
			// --- Nativer Fernsteuerungs-Viewer (öffentlich; enthält keine Geheimnisse) ---
			r.Get("/viewer/{platform}", s.handleViewerDownload)
			// --- Taskleisten-App (öffentlich; enthält keine Geheimnisse) ---
			r.Get("/tray/{platform}", s.handleTrayDownload)

			// --- Auth ---
			r.Post("/auth/login", s.handleLogin)
			r.Post("/auth/login/totp", s.handleLoginTOTP)
			r.Post("/auth/logout", s.handleLogout)

			// --- Web-API (Session-Cookie) ---
			r.Group(func(r chi.Router) {
				r.Use(s.requireUser)
				r.Use(s.audit) // ändernde Aktionen protokollieren
				// Immer erlaubt – auch während der 2FA-Einrichtung:
				r.Get("/auth/me", s.handleMe)
				r.Put("/auth/me/theme", s.handleSetTheme)
				r.Get("/auth/me/prefs", s.handleGetUserPrefs)
				r.Put("/auth/me/prefs/{key}", s.handleSetUserPref)
				r.Delete("/auth/me/prefs/{key}", s.handleDeleteUserPref)
				r.Post("/auth/me/password", s.handleChangePassword)
				r.Post("/auth/2fa/setup", s.handle2FASetup)
				r.Post("/auth/2fa/enable", s.handle2FAEnable)
				r.Post("/auth/2fa/disable", s.handle2FADisable)
				r.Post("/auth/2fa/recovery", s.handle2FARecovery)

				// Restliche Funktionen erst nach abgeschlossener 2FA (bei Pflicht).
				r.Group(func(r chi.Router) {
					r.Use(s.requireEnrolled)
					r.Use(s.scopeDevice)                 // Daten-Scope für /devices/{id}/… erzwingen
					r.Get("/agents", s.handleAgentList)  // harmlose Plattform-Liste
					r.Get("/viewer", s.handleViewerList) // Viewer-Pakete für die Download-Seite
					r.Get("/tray", s.handleTrayList)     // Taskleisten-App-Pakete für die Download-Seite

					// --- API-Tokens (Bearer) für native Clients / Mobile-App ---
					// Jeder voll authentifizierte Nutzer verwaltet seine eigenen Tokens.
					r.Get("/auth/api-tokens", s.handleListAPITokens)
					r.Post("/auth/api-tokens", s.handleCreateAPIToken)
					r.Delete("/auth/api-tokens/{id}", s.handleRevokeAPIToken)

					// --- Übersicht (page.dashboard) ---
					r.With(s.requirePerm(model.PermDashboard)).Get("/dashboard", s.handleDashboard)
					r.With(s.requirePerm(model.PermDashboard)).Get("/dashboard/stats/sources", s.handleStatSources)
					r.With(s.requirePerm(model.PermDashboard)).Get("/dashboard/stats", s.handleStats)

					// --- Geräte lesen (page.devices) ---
					r.Group(func(r chi.Router) {
						r.Use(s.requirePerm(model.PermDevices))
						r.Get("/devices", s.handleListDevices)
						r.Get("/devices/{id}", s.handleGetDevice)
						r.Get("/devices/{id}/history", s.handleDeviceHistory)
						r.Get("/devices/{id}/events", s.handleDeviceEvents)
						r.Get("/devices/{id}/remote-consent", s.handleGetDeviceConsent)
						r.Get("/devices/{id}/software-alerts", s.handleGetDeviceSoftwareAlerts)
						r.Get("/devices/{id}/metrics-history", s.handleMetricsHistory)
						r.Get("/devices/{id}/checks/{checkID}/history", s.handleCheckHistory)
						r.Get("/devices/{id}/vulnerabilities", s.handleDeviceVulns)
						r.Get("/vulnerabilities", s.handleAllVulns)
						r.Get("/devices/{id}/container-vulnerabilities", s.handleDeviceContainerVulns)
						r.Get("/container-vulnerabilities", s.handleAllContainerVulns)
						r.Get("/patches/pending", s.handlePendingPatches)
						r.Get("/patches/summary", s.handlePatchSummary)
						r.Get("/patches/history", s.handlePatchHistory)
						r.Get("/devices/{id}/patch-history", s.handleDevicePatchHistory)
						r.Get("/packages", s.handlePackagesFleet)
						r.Get("/packages/devices", s.handlePackageDevices)
						r.Get("/repos", s.handleReposFleet)
						r.Get("/eol", s.handleEOLFleet)
						r.Get("/devices/{id}/eol", s.handleDeviceEOL)
						r.Get("/devices/{id}/fim", s.handleDeviceFIM)
						r.Get("/security/fim", s.handleFIMFleet)
						r.Get("/security/incidents", s.handleIncidents)
						r.Get("/security/auth-events", s.handleAuthEvents)
						r.Get("/security/summary", s.handleSecuritySummary)
						r.Get("/security/kinds", s.handleIncidentKinds)
						r.Get("/security/threat", s.handleThreatStatus)
						r.Get("/security/containers", s.handleFleetContainers)
						r.Get("/security/threat/lookup", s.handleThreatLookup)
						r.Get("/devices/{id}/incidents", s.handleDeviceIncidents)
						r.Get("/devices/{id}/auth-events", s.handleDeviceAuthEvents)
						r.Get("/compliance/rules", s.handleComplianceRules)
						r.Get("/compliance", s.handleComplianceFleet)
						r.Get("/compliance/by-rule", s.handleComplianceByRule)
						r.Get("/compliance/rules/{id}/devices", s.handleComplianceRuleDevices)
						r.Get("/devices/{id}/compliance", s.handleDeviceCompliance)
						r.Get("/security/blocks", s.handleIPBlocks)
						r.Get("/devices/{id}/blocks", s.handleDeviceIPBlocks)
						r.Get("/devices/{id}/security-monitoring", s.handleDeviceSecurityMonitoring)
						r.Get("/sites/{id}/assets", s.handleListSiteAssets)
						r.Get("/devices/{id}/task-runs", s.handleDeviceTaskRuns)
						r.Get("/devices/{id}/backup-runs", s.handleDeviceBackupRuns)
						r.Get("/backup-runs/{id}", s.handleGetBackupRun)
						r.Get("/devices/{id}/software-events", s.handleDeviceSoftwareEvents)
						r.Post("/devices/{id}/scan-dir", s.handleScanDir)
						r.Post("/devices/{id}/services", s.handleListServices)
						r.Post("/devices/{id}/processes", s.handleListProcesses)
						r.Post("/devices/{id}/resolutions", s.handleListResolutions)
						r.Post("/devices/{id}/resolution", s.handleSetResolution)
						r.Post("/devices/{id}/install-guest-tools", s.handleInstallGuestTools)
						r.Post("/devices/{id}/metrics", s.handleMetrics)
						r.Get("/commands/{id}", s.handleGetCommand)
						r.Put("/devices/{id}/groups", s.handleSetDeviceGroups)
						r.Get("/groups", s.handleListGroups)
						r.Get("/clients", s.handleClientTree)
						r.Get("/custom-fields", s.handleListCustomFields)
						r.Get("/custom-field-values", s.handleGetCustomFieldValues)
					})

					// --- Richtlinien lesen (page.policies) ---
					r.Group(func(r chi.Router) {
						r.Use(s.requirePerm(model.PermPolicies))
						r.Get("/policies", s.handleListPolicies)
						r.Get("/maintenance", s.handleListMaintenance)
					})

					// Skript-Liste: für die Skripte-Seite ODER den Richtlinien-Editor.
					r.With(s.requirePermAny(model.PermScripts, model.PermPolicies)).Get("/scripts", s.handleListScripts)

					// Proxmox-Hosts/Gäste lesen: für die Einstellungen ODER die
					// Proxmox-Remediation-Auswahl im Richtlinien-Editor.
					r.Group(func(r chi.Router) {
						r.Use(s.requirePermAny(model.PermSettings, model.PermPolicies))
						r.Get("/proxmox/hosts", s.handleListProxmoxHosts)
						r.Get("/proxmox/hosts/{id}/guests", s.handleListProxmoxGuests)
					})

					// Allgemeine Einstellungen lesen: für die Einstellungen-Seite ODER den
					// Richtlinien-Editor (der beschriftet die Zeitfelder mit der Zone).
					r.With(s.requirePermAny(model.PermSettings, model.PermPolicies)).
						Get("/settings/general", s.handleGetGeneralSettings)
					r.With(s.requirePermAny(model.PermSettings, model.PermPolicies)).
						Get("/settings/security", s.handleGetSecuritySettings)

					// --- Einstellungen lesen (page.settings) ---
					r.Group(func(r chi.Router) {
						r.Use(s.requirePerm(model.PermSettings))
						r.Get("/settings/alerts", s.handleGetAlerts)
						r.Get("/settings/alert-providers", s.handleListAlertProviders)
					})

					// Geräte bedienen (devices.operate) – keine Verwaltung.
					r.Group(func(r chi.Router) {
						r.Use(s.requirePerm(model.PermDevicesOperate))
						r.Put("/devices/{id}/notes", s.handleSetDeviceNotes)
						r.Put("/devices/{id}/software-alerts", s.handleSetDeviceSoftwareAlerts)
						r.Post("/devices/{id}/run", s.handleRunScript)
						r.Post("/devices/{id}/checks/{checkID}/run", s.handleRunCheck)
						r.Post("/devices/{id}/tasks/{taskID}/run", s.handleRunTask)
						r.Post("/backups/{id}/run", s.handleRunBackup)
						r.Post("/backups/{id}/prune", s.handlePruneBackup)
						r.Post("/devices/{id}/external-scan", s.handleExternalScan)
						r.Post("/devices/{id}/service-control", s.handleServiceControl)
						r.Post("/devices/{id}/process-kill", s.handleProcessKill)
						r.Post("/devices/{id}/wake", s.handleWake)
						r.Post("/devices/{id}/docker-control", s.handleDockerControl)
						r.Post("/devices/{id}/proxmox-control", s.handleProxmoxControl)
						r.Post("/bulk/run-script", s.handleBulkRunScript)
						r.Post("/bulk/scan-updates", s.handleBulkScanUpdates)
						r.Post("/bulk/install-updates", s.handleBulkInstallUpdates)
						r.Post("/bulk/reboot", s.handleBulkReboot)
						r.Get("/software-packages", s.handleListPackages)
						r.Post("/bulk/install-package", s.handleBulkInstallPackage)
						r.Post("/devices/{id}/scan-updates", s.handleScanUpdates)
						r.Post("/devices/{id}/scan-cve", s.handleScanCVE)
						r.Post("/devices/{id}/scan-images", s.handleScanImages)
						r.Post("/network-scan", s.handleStartNetworkScan)
						r.Post("/snmp-printer", s.handleSNMPPrinter)
						r.Post("/devices/{id}/snmp", s.handleDeviceSNMP)
						r.Post("/network-assets/{id}/adopt", s.handleAdoptAsset)
						r.Post("/sites/{id}/assets/adopt-all", s.handleAdoptAllAssets)
						r.Put("/network-assets/{id}/note", s.handleSetAssetNote)
						r.Delete("/network-assets/{id}", s.handleDeleteAsset)
						r.Post("/devices/{id}/install-updates", s.handleInstallUpdates)
						r.Put("/devices/{id}/patches/approve", s.handleApprovePatch)
						r.Put("/patches/approve", s.handleApprovePatches)
						r.Put("/security/fim/ack", s.handleAckFIM)
						r.Put("/security/incidents/{id}", s.handleSetIncidentStatus)
						r.Post("/devices/{id}/fim/rescan", s.handleFIMRescan)
						r.Post("/devices/{id}/fim/reset", s.handleFIMReset)
						r.Post("/devices/{id}/rootcheck", s.handleRootcheckNow)
						r.Post("/devices/{id}/compliance-scan", s.handleComplianceScan)
						r.Post("/devices/{id}/block-ip", s.handleBlockIP)
						r.Post("/devices/{id}/unblock-ip", s.handleUnblockIP)
						r.Post("/security/unblock-ip", s.handleUnblockIPFleet)
						r.Post("/devices/{id}/lock-user", s.handleLockUser)
						r.Post("/devices/{id}/unlock-user", s.handleUnlockUser)
						r.Post("/devices/{id}/reboot", s.handleReboot)
						r.Post("/devices/{id}/av-status", s.handleAVStatus)
						r.Post("/devices/{id}/bitlocker", s.handleBitLockerStatus)
						r.Get("/devices/{id}/bitlocker/{cmd}", s.handleBitLockerResult)
						r.Post("/devices/{id}/smart", s.handleSmartStatus)
						r.Post("/devices/{id}/event-log", s.handleEventLog)
						r.Post("/devices/{id}/test-log-source", s.handleTestLogSource)
						r.Post("/devices/{id}/browse", s.handleBrowse)
						r.Post("/devices/{id}/read-file", s.handleReadFile)
						r.Get("/devices/{id}/file/{cmd}", s.handleServeFile)
						r.Post("/devices/{id}/write-file", s.handleWriteFile)
						// Direkt über eine Datei-Sitzung (Agent ab 0.29.0, sonst 409 legacy).
						r.Get("/devices/{id}/fs/list", s.handleFSList)
						r.Get("/devices/{id}/fs/stat", s.handleFSStat)
						r.Get("/devices/{id}/fs/download", s.handleFSDownload)
						r.Post("/devices/{id}/fs/upload", s.handleFSUpload)
					})

					// --- Abläufe (Flow-Designer) lesen ---
					r.Group(func(r chi.Router) {
						r.Use(s.requirePermAny(model.PermScripts, model.PermDevicesOperate))
						r.Get("/flows", s.handleListFlows)
						r.Get("/flows/templates", s.handleFlowTemplates)
						r.Get("/flows/{id}", s.handleGetFlow)
						r.Get("/flows/{id}/runs", s.handleListFlowRuns)
						r.Get("/flows/{id}/export", s.handleExportFlow)
						r.Get("/flow-runs/{id}", s.handleGetFlowRun)
					})
					// --- Abläufe ausführen (devices.operate) ---
					r.Group(func(r chi.Router) {
						r.Use(s.requirePerm(model.PermDevicesOperate))
						r.Post("/flows/{id}/runs", s.handleStartFlowRun)
						r.Post("/flow-runs/{id}/test", s.handleFlowRunTest)
						r.Post("/flow-runs/{id}/rollout", s.handleFlowRunRollout)
					})

					// --- Skripte verwalten (page.scripts) ---
					r.Group(func(r chi.Router) {
						r.Use(s.requirePerm(model.PermScripts))
						r.Post("/flows", s.handleSaveFlow)
						r.Put("/flows/{id}", s.handleSaveFlow)
						r.Delete("/flows/{id}", s.handleDeleteFlow)
						r.Post("/flows/{id}/copy", s.handleCopyFlow)
						r.Post("/flows/import", s.handleImportFlow)
						r.Post("/flows/yaml-preview", s.handleYAMLPreview)
						r.Delete("/flow-runs/{id}", s.handleDeleteFlowRun)
						r.Post("/scripts", s.handleCreateScript)
						r.Put("/scripts/{id}", s.handleUpdateScript)
						r.Delete("/scripts/{id}", s.handleDeleteScript)
					})

					// --- Richtlinien verwalten (page.policies) ---
					r.Group(func(r chi.Router) {
						r.Use(s.requirePerm(model.PermPolicies))
						r.Post("/policies", s.handleCreatePolicy)
						r.Put("/policies/{id}", s.handleUpdatePolicy)
						r.Delete("/policies/{id}", s.handleDeletePolicy)
						r.Post("/policies/{id}/checks", s.handleAddCheck)
						r.Put("/checks/{id}", s.handleUpdateCheck)
						r.Post("/policies/{id}/tasks", s.handleAddTask)
						r.Post("/policies/{id}/backups", s.handleAddBackup)
						r.Put("/backups/{id}", s.handleUpdateBackup)
						r.Delete("/backups/{id}", s.handleDeleteBackup)
						r.Put("/policies/{id}/notifications", s.handleSetPolicyNotifications)
						r.Put("/policies/{id}/security", s.handleSetPolicySecurity)
						r.Post("/policies/{id}/fim", s.handleAddFIMRule)
						r.Put("/fim/{id}", s.handleUpdateFIMRule)
						r.Delete("/fim/{id}", s.handleDeleteFIMRule)
						r.Post("/policies/{id}/responses", s.handleAddResponse)
						r.Put("/responses/{id}", s.handleUpdateResponse)
						r.Delete("/responses/{id}", s.handleDeleteResponse)
						r.Post("/policies/{id}/log-sources", s.handleAddLogSource)
						r.Put("/log-sources/{id}", s.handleUpdateLogSource)
						r.Delete("/log-sources/{id}", s.handleDeleteLogSource)
						r.Post("/policies/{id}/assignments", s.handleAddAssignment)
						r.Delete("/checks/{id}", s.handleDeleteCheck)
						r.Delete("/tasks/{id}", s.handleDeleteTask)
						r.Delete("/assignments/{id}", s.handleDeleteAssignment)
						r.Post("/maintenance", s.handleCreateMaintenance)
						r.Delete("/maintenance/{id}", s.handleDeleteMaintenance)
					})

					// --- Einstellungen/Verwaltung (page.settings) ---
					r.Group(func(r chi.Router) {
						r.Use(s.requirePerm(model.PermSettings))
						r.Put("/settings/general", s.handleSetGeneralSettings)
						r.Post("/eol/refresh", s.handleEOLRefresh)
						r.Post("/security/threat/refresh", s.handleThreatRefresh)
						r.Put("/settings/security", s.handleSetSecuritySettings)
						r.Get("/audit", s.handleListAudit)
						r.Get("/reports/health", s.handleHealthReport)
						r.Get("/report-schedules", s.handleListReportSchedules)
						r.Post("/report-schedules", s.handleCreateReportSchedule)
						r.Delete("/report-schedules/{id}", s.handleDeleteReportSchedule)
						r.Delete("/devices/{id}", s.handleDeleteDevice)
						r.Post("/devices/{id}/revoke", s.handleRevokeDevice)
						r.Put("/devices/{id}/site", s.handleSetDeviceSite)
						r.Post("/groups", s.handleCreateGroup)
						r.Put("/groups/{id}", s.handleUpdateGroup)
						r.Delete("/groups/{id}", s.handleDeleteGroup)
						r.Post("/clients", s.handleCreateClient)
						r.Put("/clients/{id}", s.handleRenameClient)
						r.Delete("/clients/{id}", s.handleDeleteClient)
						r.Post("/sites", s.handleCreateSite)
						r.Put("/sites/{id}", s.handleRenameSite)
						r.Delete("/sites/{id}", s.handleDeleteSite)
						r.Put("/settings/alerts", s.handleSetAlertsEnabled)
						r.Post("/custom-fields", s.handleCreateCustomField)
						r.Put("/custom-fields/{id}", s.handleUpdateCustomField)
						r.Delete("/custom-fields/{id}", s.handleDeleteCustomField)
						r.Put("/custom-field-values", s.handleSetCustomFieldValues)
						r.Post("/settings/alert-channels", s.handleCreateAlertChannel)
						r.Put("/settings/alert-channels/{id}", s.handleUpdateAlertChannel)
						r.Delete("/settings/alert-channels/{id}", s.handleDeleteAlertChannel)
						r.Post("/settings/alert-channels/{id}/test", s.handleTestAlertChannel)
						r.Post("/proxmox/hosts", s.handleCreateProxmoxHost)
						r.Delete("/proxmox/hosts/{id}", s.handleDeleteProxmoxHost)
						r.Post("/proxmox/hosts/{id}/reboot", s.handleRebootProxmoxGuest)
						r.Put("/remote-consent", s.handleSetRemoteConsent)
						r.Post("/software-packages", s.handleCreatePackage)
						r.Put("/software-packages/{id}", s.handleUpdatePackage)
						r.Delete("/software-packages/{id}", s.handleDeletePackage)
					})

					// --- Nur echte Admins: Benutzer, Rollen, Enrollment-Tokens ---
					r.Group(func(r chi.Router) {
						r.Use(s.requireAdmin)
						r.Get("/enrollment-tokens", s.handleListTokens)
						r.Post("/enrollment-tokens", s.handleCreateToken)
						r.Delete("/enrollment-tokens/{id}", s.handleDeleteToken)
						r.Get("/users", s.handleListUsers)
						r.Post("/users", s.handleCreateUser)
						r.Put("/users/{id}", s.handleUpdateUser)
						r.Delete("/users/{id}", s.handleDeleteUser)
						r.Get("/users/{id}/scope", s.handleGetUserScope)
						r.Put("/users/{id}/scope", s.handleSetUserScope)
						r.Post("/users/{id}/reset-2fa", s.handleAdminReset2FA)
						r.Get("/roles", s.handleListRoles)
						r.Post("/roles", s.handleCreateRole)
						r.Put("/roles/{id}", s.handleUpdateRole)
						r.Delete("/roles/{id}", s.handleDeleteRole)
					})
				})
			})
		})
	})

	// Kurze öffentliche Install-URL für den One-Liner (irm .../i/w/<token> | iex).
	r.Get("/i/{plat}/{token}", s.handleInstallScript)

	// Frontend (SPA) ausliefern, falls eingebettet.
	if s.webFS != nil {
		r.NotFound(s.serveSPA)
	}
	return r
}

// --- HTTP-Helfer ---

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (s *Server) writeErr(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON liest und validiert eine JSON-Anfrage.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		s.writeErr(w, http.StatusBadRequest, "ungültiger request body: "+err.Error())
		return false
	}
	return true
}

// mapStoreErr übersetzt Store-Fehler in HTTP-Status.
func (s *Server) mapStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeErr(w, http.StatusNotFound, "nicht gefunden")
	default:
		s.log.Error("store-fehler", "err", err)
		s.writeErr(w, http.StatusInternalServerError, "interner fehler")
	}
}

// serveSPA liefert statische Dateien aus webFS und fällt für Client-Routen auf index.html zurück.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	if _, err := fs.Stat(s.webFS, p); err != nil {
		p = "index.html" // SPA-Fallback
	}
	// Cache-Regeln: index.html darf nie veraltet aus dem Cache kommen (sie verweist
	// auf die gehashten Assets des jeweiligen Builds), die Assets selbst sind durch
	// den Hash im Namen unveränderlich und dürfen lange gecacht werden.
	switch {
	case p == "index.html":
		w.Header().Set("Cache-Control", "no-cache")
	case strings.HasPrefix(p, "assets/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFileFS(w, r, s.webFS, p)
}
