// Package model definiert die Domänentypen des Servers.
package model

import "time"

// Role legt die Berechtigungsstufe eines Benutzers fest.
type Role string

const (
	RoleAdmin  Role = "admin"      // volle Verwaltung
	RoleTech   Role = "technician" // Geräte bedienen, aber nicht verwalten
	RoleViewer Role = "viewer"     // nur lesen
)

// CanOperate meldet, ob die Rolle Geräte bedienen darf (Techniker oder Admin).
func (r Role) CanOperate() bool { return r == RoleAdmin || r == RoleTech }

// AuthSource gibt an, woher ein Benutzer authentifiziert wird.
type AuthSource string

const (
	AuthLocal AuthSource = "local"
	AuthLDAP  AuthSource = "ldap"
)

// User ist ein Web-/Admin-Benutzer (lokal oder via LDAP).
type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"` // bei LDAP leer
	Role         Role       `json:"role"`
	CustomRoleID string     `json:"custom_role_id,omitempty"` // optionale Custom-Rolle (Rechte-Set)
	AuthSource   AuthSource `json:"auth_source"`
	Theme        string     `json:"theme"` // "light" | "dark" | "" (nicht gesetzt)
	CreatedAt    time.Time  `json:"created_at"`
	LastLogin    *time.Time `json:"last_login,omitempty"`
	// Zwei-Faktor (TOTP). TOTPSecret nie ins JSON.
	TOTPSecret  string `json:"-"`
	TOTPEnabled bool   `json:"totp_enabled"`
	// Require2FA ist berechnet (nicht persistiert): muss der Nutzer 2FA einrichten?
	Require2FA bool `json:"require_2fa"`
	// Permissions ist berechnet (nicht persistiert): effektive Rechte des Benutzers.
	Permissions []string `json:"permissions,omitempty"`
}

// CustomRole ist ein wiederverwendbares Rechte-Set (Seiten/Funktionen).
type CustomRole struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
	UserCount   int       `json:"user_count"` // Anzahl zugeordneter Benutzer (nur lesend)
}

// Device ist ein inventarisiertes Gerät = genau ein Agent.
type Device struct {
	ID           string     `json:"id"`
	Hostname     string     `json:"hostname"`
	OS           string     `json:"os"`
	OSVersion    string     `json:"os_version"`
	Vendor       string     `json:"vendor"`
	Model        string     `json:"model"`
	Serial       string     `json:"serial"`
	CPUModel     string     `json:"cpu_model"`
	CPUCores     int        `json:"cpu_cores"`
	CPUSockets   int        `json:"cpu_sockets"`
	CPUThreads   int        `json:"cpu_threads"`
	MemoryBytes  uint64     `json:"memory_bytes"`
	AgentVersion string     `json:"agent_version"`
	PublicIP     string     `json:"public_ip,omitempty"`
	FirstSeen    time.Time  `json:"first_seen"`
	LastSeen     *time.Time `json:"last_seen,omitempty"`
	EnrolledAt   time.Time  `json:"enrolled_at"`
	Revoked      bool       `json:"revoked"`
	Managed      bool       `json:"managed"` // false = ohne Agent (z. B. aus Netzwerk-Scan)
	Status       string     `json:"status"` // berechnet: online/offline/unmanaged (nicht persistiert)

	// Freitext-Notizen zum Gerät (Doku).
	Notes string `json:"notes"`

	// Organisations-Zuordnung (Client -> Site). SiteID nil = nicht zugeordnet.
	SiteID     *string `json:"site_id,omitempty"`
	SiteName   string  `json:"site_name,omitempty"`
	ClientID   string  `json:"client_id,omitempty"`
	ClientName string  `json:"client_name,omitempty"`

	LoggedInUsers []string          `json:"logged_in_users,omitempty"`
	Interfaces    []Interface       `json:"interfaces,omitempty"`
	Groups        []Group           `json:"groups,omitempty"`
	Software      []SoftwarePackage `json:"software,omitempty"`
	Printers      []Printer         `json:"printers,omitempty"`

	// OS-Updates: UpdatesCount nil = noch nicht geprüft ("unbekannt").
	UpdatesCount     *int         `json:"updates_count,omitempty"`
	UpdatesCheckedAt *time.Time   `json:"updates_checked_at,omitempty"`
	AvailableUpdates []UpdateItem `json:"available_updates,omitempty"`

	// Policy-Check-Zusammenfassung (für Health-Badge in der Liste).
	ChecksTotal   int `json:"checks_total"`
	ChecksFailing int `json:"checks_failing"`

	// Task-Zusammenfassung (für Task-Badge in der Liste): Gesamt = Tasks mit
	// mindestens einem Lauf, Failing = letzter Lauf mit exit_code != 0.
	TasksTotal   int `json:"tasks_total"`
	TasksFailing int `json:"tasks_failing"`

	// Anzahl erkannter Schwachstellen (CVE/OSV) – für Badge in der Liste + Dashboard.
	VulnCount int `json:"vuln_count"`

	// Anzahl wirksamer Checks/Tasks laut zugewiesener Policy (für die Statusmeldung).
	AssignedChecks int `json:"assigned_checks"`
	AssignedTasks  int `json:"assigned_tasks"`

	CheckResults []CheckResult `json:"check_results,omitempty"`
	TaskResults  []TaskResult  `json:"task_results,omitempty"`
	Commands     []Command     `json:"commands,omitempty"`
	ListenPorts  []ListenPort  `json:"listen_ports,omitempty"`

	Disks         []Disk         `json:"disks,omitempty"`
	PhysicalDisks []PhysicalDisk `json:"physical_disks,omitempty"`
	GPUs          []string       `json:"gpus,omitempty"`
}

// Disk ist ein Volume inkl. Belegung (für die Anzeige).
type Disk struct {
	Name        string  `json:"name"`
	SizeBytes   uint64  `json:"size_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
	FSType      string  `json:"fs_type"`
}

// PhysicalDisk ist ein physisches Laufwerk (Modell + Größe).
type PhysicalDisk struct {
	Model     string `json:"model"`
	SizeBytes uint64 `json:"size_bytes"`
}

// UpdateItem ist ein verfügbarer Patch/Update (für den Patches-Tab).
type UpdateItem struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
	URL      string `json:"url,omitempty"`
	Approved bool   `json:"approved"`
}

// SoftwarePackage ist ein installiertes Programm/Paket.
type SoftwarePackage struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Publisher string `json:"publisher,omitempty"`
}

// Printer ist ein installierter Drucker.
type Printer struct {
	Name    string `json:"name"`
	Driver  string `json:"driver,omitempty"`
	Port    string `json:"port,omitempty"`
	Default bool   `json:"default"`
}

// Interface ist eine im Inventar gemeldete Netzwerkschnittstelle.
// Schnittstellen werden bei jedem Checkin komplett ersetzt und nie einzeln
// referenziert, daher ohne eigenen Primärschlüssel (DB-dialekt-portabel).
type Interface struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

// ListenPort ist ein lauschender Socket des Geräts (Angriffsfläche). Public=true,
// wenn nicht nur an Loopback gebunden.
type ListenPort struct {
	Proto   string `json:"proto"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Public  bool   `json:"public"`
	// Außen-Check (nur TCP): ExtChecked=true, wenn der Server die öffentliche IP auf
	// diesem Port getestet hat; ExtReachable = tatsächlich von außen erreichbar.
	ExtChecked   bool `json:"ext_checked,omitempty"`
	ExtReachable bool `json:"ext_reachable,omitempty"`
}

// Client ist die oberste Organisationsebene (Firma/Kunde). Enthält Sites.
type Client struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DeviceCount int    `json:"device_count"`
	Sites       []Site `json:"sites,omitempty"`
}

// Site ist ein Standort innerhalb eines Clients. Geräte gehören zu genau einer Site.
type Site struct {
	ID          string `json:"id"`
	ClientID    string `json:"client_id"`
	Name        string `json:"name"`
	DeviceCount int    `json:"device_count"`
}

// Group bündelt Geräte (n:m). Im UI als „Tags" geführt.
type Group struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ParentID    *string `json:"parent_id,omitempty"`
	DeviceCount int     `json:"device_count,omitempty"`
	// Rule ist die JSON-Regel einer Smart Group (leer = statische Gruppe).
	Rule string `json:"rule"`
}

// Script ist ein wiederverwendbares Skript (für Script-Checks und Tasks).
type Script struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Shell     string    `json:"shell"`      // powershell | shell
	Platforms []string  `json:"platforms"`  // windows|linux|darwin; leer = keine Extra-Einschränkung
	Content   string    `json:"content"`
	CheckOnly bool      `json:"check_only"` // nur als Check verwendbar (nicht in Ausführen/Sammelaktion)
	CreatedAt time.Time `json:"created_at"`
}

// Policy bündelt Checks und Tasks und wird Clients/Sites/Geräten zugewiesen.
type Policy struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Checks      []PolicyCheck `json:"checks,omitempty"`
	Tasks       []PolicyTask  `json:"tasks,omitempty"`
	Assignments []Assignment  `json:"assignments,omitempty"`
}

// PolicyCheck ist ein Check innerhalb einer Policy.
type PolicyCheck struct {
	ID        string         `json:"id"`
	PolicyID  string         `json:"policy_id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"` // disk | memory | cpu | updates | script
	Config    map[string]any `json:"config"`
	ScriptID  *string        `json:"script_id,omitempty"`
	Severity  string         `json:"severity"`  // warning | critical (Standard critical)
	Frequency string         `json:"frequency"` // "" = jeden Checkin, sonst Preset
	// RemediationScriptID: bei „failing" automatisch ausgeführtes Skript (Self-Healing).
	RemediationScriptID *string `json:"remediation_script_id,omitempty"`
}

// PolicyTask ist ein geplanter Skript-Task innerhalb einer Policy.
type PolicyTask struct {
	ID              string  `json:"id"`
	PolicyID        string  `json:"policy_id"`
	Name            string  `json:"name"`
	ScriptID        *string `json:"script_id,omitempty"`
	IntervalMinutes int     `json:"interval_minutes"`
	ScheduleType    string  `json:"schedule_type"` // interval | daily (Legacy)
	DailyTime       string  `json:"daily_time"`
	Weekdays        string  `json:"weekdays"`
	Frequency       string  `json:"frequency"` // Preset; überschreibt ScheduleType
	// CollectFields: JSON-Ausgabe des Tasks in benutzerdefinierte Felder übernehmen.
	CollectFields bool `json:"collect_fields"`
}

// CustomField ist eine benutzerdefinierte Feld-Definition für eine Entität.
type CustomField struct {
	ID       string   `json:"id"`
	Model    string   `json:"model"` // client | site | device
	Name     string   `json:"name"`
	Type     string   `json:"type"` // text|number|checkbox|select|multiselect|datetime|list
	Options  []string `json:"options"`
	Default  string   `json:"default_value"`
	Required bool     `json:"required"`
}

// CustomFieldValue verbindet eine Feld-Definition mit dem Wert einer Entität.
type CustomFieldValue struct {
	Field CustomField `json:"field"`
	Value string      `json:"value"` // list/multiselect: JSON-Array, sonst String
}

// AlertConfig steuert Benachrichtigungen bei fehlschlagenden Checks. Nach der
// Modularisierung wird nur noch Enabled (Master-Schalter) genutzt; die smtp_*-
// Felder bleiben für Abwärtskompatibilität der Tabelle erhalten.
type AlertConfig struct {
	Enabled       bool   `json:"enabled"`
	AlertSoftware bool   `json:"alert_software"` // bei Software-Änderungen benachrichtigen
	SMTPHost      string `json:"smtp_host,omitempty"`
	SMTPPort      int    `json:"smtp_port,omitempty"`
	SMTPUser      string `json:"smtp_user,omitempty"`
	SMTPPass      string `json:"smtp_pass,omitempty"`
	SMTPFrom      string `json:"smtp_from,omitempty"`
	SMTPTLS       bool   `json:"smtp_tls,omitempty"`
	Recipient     string `json:"recipient,omitempty"`
	WebhookURL    string `json:"webhook_url,omitempty"`
}

// AlertChannel ist eine konfigurierte Instanz eines Benachrichtigungs-Providers.
type AlertChannel struct {
	ID      string            `json:"id"`
	Type    string            `json:"type"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"`
	// MinSeverity: "warning" = bei jedem Fehler, "critical" = nur kritische Checks.
	MinSeverity string `json:"min_severity"`
	// Assignments schränkt den Geltungsbereich ein (leer = global/alle Geräte).
	Assignments []ChannelScope `json:"assignments"`
}

// ChannelScope bindet einen Kanal an einen Client/eine Site/ein Gerät.
type ChannelScope struct {
	TargetType string `json:"target_type"` // client | site | device
	TargetID   string `json:"target_id"`
}

// Command ist ein Ad-hoc-Befehl an ein Gerät (z.B. Skript ausführen).
type Command struct {
	ID        string     `json:"id"`
	DeviceID  string     `json:"device_id"`
	Type      string     `json:"type"`
	Label     string     `json:"label"`
	Status    string     `json:"status"` // pending | sent | done
	CreatedAt time.Time  `json:"created_at"`
	ExitCode  int        `json:"exit_code"`
	Output    string     `json:"output,omitempty"`
	RanAt     *time.Time `json:"ran_at,omitempty"`
}

// Assignment verknüpft eine Policy mit einem Ziel (client|site|device).
type Assignment struct {
	ID         string `json:"id"`
	PolicyID   string `json:"policy_id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
}

// CheckResult ist der aktuelle Status eines Checks auf einem Gerät.
type CheckResult struct {
	CheckID   string    `json:"check_id"`
	Status    string    `json:"status"`
	Output    string    `json:"output,omitempty"`
	Value     float64   `json:"value,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`

	// für die Anzeige angereichert:
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// CheckEvent protokolliert einen Statuswechsel eines Checks (z.B. passing→warning,
// failing→passing) inkl. ob/wann darüber benachrichtigt wurde.
type CheckEvent struct {
	ID         string     `json:"id"`
	DeviceID   string     `json:"device_id"`
	Hostname   string     `json:"hostname,omitempty"` // nur in der globalen Übersicht gefüllt
	CheckID    string     `json:"check_id"`
	CheckName  string     `json:"check_name"`
	OldStatus  string     `json:"old_status"`
	NewStatus  string     `json:"new_status"`
	Output     string     `json:"output,omitempty"`
	Notified   bool       `json:"notified"`
	NotifiedAt *time.Time `json:"notified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ReportSchedule versendet regelmäßig einen Health-Bericht über einen Alarm-Kanal.
type ReportSchedule struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Frequency   string     `json:"frequency"` // daily | weekly | monthly
	ChannelID   string     `json:"channel_id"`
	ChannelName string     `json:"channel_name,omitempty"`
	LastRun     *time.Time `json:"last_run,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// MaintenanceWindow unterdrückt Alarme für ein Ziel (Client/Site/Device) im Zeitraum.
type MaintenanceWindow struct {
	ID         string    `json:"id"`
	TargetType string    `json:"target_type"` // client | site | device
	TargetID   string    `json:"target_id"`
	TargetName string    `json:"target_name,omitempty"`
	Note       string    `json:"note,omitempty"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// AuditEntry protokolliert eine ändernde Aktion eines Benutzers.
type AuditEntry struct {
	ID       string    `json:"id"`
	TS       time.Time `json:"ts"`
	UserID   string    `json:"user_id,omitempty"`
	Username string    `json:"username"`
	Action   string    `json:"action"`
	Method   string    `json:"method"`
	Path     string    `json:"path"`
	Status   int       `json:"status"`
	IP       string    `json:"ip,omitempty"`
}

// DashboardSummary ist die aggregierte Übersicht über alle (nicht widerrufenen) Geräte.
type DashboardSummary struct {
	DevicesTotal              int          `json:"devices_total"`
	DevicesOnline             int          `json:"devices_online"`
	DevicesOffline            int          `json:"devices_offline"`
	DevicesUnknown            int          `json:"devices_unknown"`
	DevicesWithFailingChecks  int          `json:"devices_with_failing_checks"`
	FailingChecks             int          `json:"failing_checks"`
	DevicesWithFailingTasks   int          `json:"devices_with_failing_tasks"`
	FailingTasks              int          `json:"failing_tasks"`
	DevicesWithPendingPatches int          `json:"devices_with_pending_patches"`
	PendingPatches            int          `json:"pending_patches"`
	DevicesWithVulns          int          `json:"devices_with_vulns"`
	Vulnerabilities           int          `json:"vulnerabilities"`
	RecentEvents              []CheckEvent `json:"recent_events"`
}

// SoftwareEvent protokolliert eine Software-Änderung (installiert/entfernt/aktualisiert).
type SoftwareEvent struct {
	ID         string    `json:"id"`
	DeviceID   string    `json:"device_id"`
	Change     string    `json:"change"` // added | removed | updated
	Name       string    `json:"name"`
	Version    string    `json:"version,omitempty"`
	OldVersion string    `json:"old_version,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// TaskResult ist ein Task-Lauf-Ergebnis auf einem Gerät.
type TaskResult struct {
	ID       string    `json:"id"`
	TaskID   string    `json:"task_id"`
	ExitCode int       `json:"exit_code"`
	Output   string    `json:"output,omitempty"`
	RanAt    time.Time `json:"ran_at"`
	Name     string    `json:"name,omitempty"`
}

// EnrollmentToken wird von Admins erzeugt und per GPO verteilt.
// NetworkAsset ist ein beim Netzwerk-Scan gefundener Host, einer Site zugeordnet.
type NetworkAsset struct {
	ID        string    `json:"id"`
	SiteID    string    `json:"site_id"`
	SiteName  string    `json:"site_name,omitempty"`
	IP        string    `json:"ip"`
	MAC       string    `json:"mac"`
	Hostname  string    `json:"hostname"`
	Ports     string    `json:"ports"`
	Note      string    `json:"note"`
	Managed   bool      `json:"managed"` // entspricht einem verwalteten Gerät
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

// Vulnerability ist eine für ein Gerät erkannte Schwachstelle (CVE/OSV).
type Vulnerability struct {
	DeviceID   string    `json:"device_id,omitempty"`
	Hostname   string    `json:"hostname,omitempty"`
	Package    string    `json:"package"`
	Version    string    `json:"version"`
	VulnID     string    `json:"vuln_id"`
	Severity   string    `json:"severity"` // CRITICAL | HIGH | MEDIUM | LOW | ""
	Summary    string    `json:"summary"`
	Fixed      string    `json:"fixed"`
	URL        string    `json:"url"`
	DetectedAt time.Time `json:"detected_at"`
}

// DeployPackage ist ein verteilbares Software-Paket mit Kennung je Paketmanager.
type DeployPackage struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Winget string `json:"winget"`
	Choco  string `json:"choco"`
	Apt    string `json:"apt"`
	Dnf    string `json:"dnf"`
	Brew   string `json:"brew"`
}

type EnrollmentToken struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Token     string     `json:"token,omitempty"` // nur bei Erstellung im Klartext zurückgegeben
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	MaxUses   int        `json:"max_uses"` // 0 = unbegrenzt
	UsedCount int        `json:"used_count"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	// SiteID bindet das Token an einen Standort; damit enrollte Geräte landen dort.
	SiteID *string `json:"site_id,omitempty"`
}
