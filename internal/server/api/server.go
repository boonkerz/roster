// Package api implementiert die HTTP-Schnittstelle (Agent- und Web-API).
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/thomaspeterson/pc-inventory/internal/server/config"
	"github.com/thomaspeterson/pc-inventory/internal/server/store"
)

// Server bündelt Abhängigkeiten der HTTP-Handler.
type Server struct {
	store   *store.Store
	cfg     config.Config
	log     *slog.Logger
	webFS   fs.FS    // gebautes React-Frontend (kann nil sein)
	version string   // Version der eingebetteten Agent-Binaries (für Auto-Update)
	term    *termHub // flüchtiger Zustand für Remote-Terminal-Sessions
	files   *fileHub // flüchtige Datei-Übertragungen
	router  http.Handler
}

// New erstellt den Server und registriert alle Routen.
func New(st *store.Store, cfg config.Config, log *slog.Logger, webFS fs.FS, version string) *Server {
	s := &Server{store: st, cfg: cfg, log: log, webFS: webFS, version: version, term: newTermHub(), files: newFileHub()}
	s.router = s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

func (s *Server) routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

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
				r.Post("/auth/me/password", s.handleChangePassword)
				r.Post("/auth/2fa/setup", s.handle2FASetup)
				r.Post("/auth/2fa/enable", s.handle2FAEnable)
				r.Post("/auth/2fa/disable", s.handle2FADisable)
				r.Post("/auth/2fa/recovery", s.handle2FARecovery)

				// Restliche Funktionen erst nach abgeschlossener 2FA (bei Pflicht).
				r.Group(func(r chi.Router) {
					r.Use(s.requireEnrolled)
					r.Get("/agents", s.handleAgentList)

					r.Get("/dashboard", s.handleDashboard)
					r.Get("/devices", s.handleListDevices)
					r.Get("/devices/{id}", s.handleGetDevice)
					r.Get("/devices/{id}/history", s.handleDeviceHistory)
					r.Get("/devices/{id}/events", s.handleDeviceEvents)
					r.Get("/devices/{id}/task-runs", s.handleDeviceTaskRuns)
					r.Get("/devices/{id}/software-events", s.handleDeviceSoftwareEvents)
					r.Post("/devices/{id}/scan-dir", s.handleScanDir)
					r.Post("/devices/{id}/services", s.handleListServices)
					r.Post("/devices/{id}/processes", s.handleListProcesses)
					r.Post("/devices/{id}/metrics", s.handleMetrics)
					r.Get("/commands/{id}", s.handleGetCommand)
					r.Put("/devices/{id}/groups", s.handleSetDeviceGroups)

					r.Get("/groups", s.handleListGroups)
					r.Get("/clients", s.handleClientTree)
					r.Get("/scripts", s.handleListScripts)
					r.Get("/policies", s.handleListPolicies)
					r.Get("/maintenance", s.handleListMaintenance)
					r.Get("/settings/alerts", s.handleGetAlerts)
					r.Get("/settings/alert-providers", s.handleListAlertProviders)
					r.Get("/custom-fields", s.handleListCustomFields)
					r.Get("/custom-field-values", s.handleGetCustomFieldValues)

					// Techniker + Admins dürfen Geräte bedienen (keine Verwaltung).
					r.Group(func(r chi.Router) {
						r.Use(s.requireTech)
						r.Put("/devices/{id}/notes", s.handleSetDeviceNotes)
						r.Post("/devices/{id}/run", s.handleRunScript)
						r.Post("/devices/{id}/service-control", s.handleServiceControl)
						r.Post("/devices/{id}/process-kill", s.handleProcessKill)
						r.Post("/devices/{id}/wake", s.handleWake)
						r.Post("/bulk/run-script", s.handleBulkRunScript)
						r.Post("/bulk/scan-updates", s.handleBulkScanUpdates)
						r.Post("/devices/{id}/scan-updates", s.handleScanUpdates)
						r.Post("/devices/{id}/install-updates", s.handleInstallUpdates)
						r.Put("/devices/{id}/patches/approve", s.handleApprovePatch)
						r.Post("/devices/{id}/reboot", s.handleReboot)
						r.Post("/devices/{id}/av-status", s.handleAVStatus)
						r.Post("/devices/{id}/bitlocker", s.handleBitLockerStatus)
						r.Get("/devices/{id}/bitlocker/{cmd}", s.handleBitLockerResult)
						r.Post("/devices/{id}/smart", s.handleSmartStatus)
						r.Post("/devices/{id}/event-log", s.handleEventLog)
						r.Post("/devices/{id}/browse", s.handleBrowse)
						r.Post("/devices/{id}/read-file", s.handleReadFile)
						r.Get("/devices/{id}/file/{cmd}", s.handleServeFile)
						r.Post("/devices/{id}/write-file", s.handleWriteFile)
					})

					// Nur Admins dürfen schreiben/verwalten.
					r.Group(func(r chi.Router) {
						r.Use(s.requireAdmin)
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

						r.Post("/scripts", s.handleCreateScript)
						r.Put("/scripts/{id}", s.handleUpdateScript)
						r.Delete("/scripts/{id}", s.handleDeleteScript)
						r.Post("/policies", s.handleCreatePolicy)
						r.Put("/policies/{id}", s.handleUpdatePolicy)
						r.Delete("/policies/{id}", s.handleDeletePolicy)
						r.Post("/policies/{id}/checks", s.handleAddCheck)
						r.Post("/policies/{id}/tasks", s.handleAddTask)
						r.Post("/policies/{id}/assignments", s.handleAddAssignment)
						r.Delete("/checks/{id}", s.handleDeleteCheck)
						r.Delete("/tasks/{id}", s.handleDeleteTask)
						r.Delete("/assignments/{id}", s.handleDeleteAssignment)
						r.Post("/maintenance", s.handleCreateMaintenance)
						r.Delete("/maintenance/{id}", s.handleDeleteMaintenance)
						r.Put("/settings/alerts", s.handleSetAlertsEnabled)
						r.Post("/custom-fields", s.handleCreateCustomField)
						r.Put("/custom-fields/{id}", s.handleUpdateCustomField)
						r.Delete("/custom-fields/{id}", s.handleDeleteCustomField)
						r.Put("/custom-field-values", s.handleSetCustomFieldValues)
						r.Post("/settings/alert-channels", s.handleCreateAlertChannel)
						r.Put("/settings/alert-channels/{id}", s.handleUpdateAlertChannel)
						r.Delete("/settings/alert-channels/{id}", s.handleDeleteAlertChannel)
						r.Post("/settings/alert-channels/{id}/test", s.handleTestAlertChannel)
						r.Get("/enrollment-tokens", s.handleListTokens)
						r.Post("/enrollment-tokens", s.handleCreateToken)
						r.Delete("/enrollment-tokens/{id}", s.handleDeleteToken)
						r.Get("/users", s.handleListUsers)
						r.Post("/users", s.handleCreateUser)
						r.Post("/users/{id}/reset-2fa", s.handleAdminReset2FA)
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
	http.ServeFileFS(w, r, s.webFS, p)
}
