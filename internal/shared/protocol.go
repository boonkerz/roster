// Package shared enthält die DTOs des Agent<->Server-Protokolls.
// Diese Typen werden sowohl vom Server als auch vom Agent importiert,
// damit das Wire-Format an genau einer Stelle definiert ist.
package shared

import "time"

// EnrollRequest sendet der Agent beim ersten Start, um sich gegen ein
// kurzlebiges Enrollment-Token ein dauerhaftes Agent-Token zu holen.
type EnrollRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	OSVersion       string `json:"os_version"`
	AgentVersion    string `json:"agent_version"`
}

// EnrollResponse liefert die eindeutige Geräte-ID und das pro Agent
// widerrufbare Bearer-Token zurück.
type EnrollResponse struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
}

// CheckinRequest ist der periodische Heartbeat inkl. Inventar und Policy-Ergebnissen.
type CheckinRequest struct {
	Inventory      Inventory       `json:"inventory"`
	CheckResults   []CheckResult   `json:"check_results,omitempty"`
	TaskResults    []TaskResult    `json:"task_results,omitempty"`
	CommandResults []CommandResult `json:"command_results,omitempty"`
	Sample         *MetricsSample  `json:"sample,omitempty"` // leichte Momentaufnahme für die Historie
}

// MetricsSample ist eine leichte Auslastungs-Momentaufnahme je Checkin (für die
// Verlaufscharts). Prozentwerte 0..100.
type MetricsSample struct {
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	Disk float64 `json:"disk"` // am stärksten belegter Datenträger
}

// CommandResult ist das Ergebnis eines Ad-hoc-Befehls.
type CommandResult struct {
	CommandID string    `json:"command_id"`
	ExitCode  int       `json:"exit_code"`
	Output    string    `json:"output,omitempty"`
	RanAt     time.Time `json:"ran_at"`
}

// CommandProgress meldet einen Zwischenstand eines noch laufenden Befehls.
type CommandProgress struct {
	CommandID string `json:"command_id"`
	Output    string `json:"output"`
}

// CheckinResponse enthält die für den Agent ausstehenden Befehle
// (Befehlsausführung ist eine spätere Phase, das Feld existiert aber bereits).
type CheckinResponse struct {
	NextCheckinSec int       `json:"next_checkin_seconds"`
	Commands       []Command `json:"commands"`
	// LatestAgentVersion ist die vom Server bereitgestellte Agent-Version. Weicht sie
	// von der eigenen ab, kann sich der Agent selbst aktualisieren (sofern aktiviert).
	LatestAgentVersion string `json:"latest_agent_version"`
	// Policy ist die für dieses Gerät wirksame Policy (Checks + Tasks), nil wenn keine.
	Policy *PolicyBundle `json:"policy,omitempty"`
}

// PolicyBundle ist die für ein Gerät aufgelöste Policy (vererbt über Client/Site/Gerät).
type PolicyBundle struct {
	Checks []CheckSpec `json:"checks"`
	Tasks  []TaskSpec  `json:"tasks"`
}

// CheckSpec beschreibt einen auszuführenden Check.
type CheckSpec struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"` // disk | memory | cpu | updates | script
	Config    map[string]any `json:"config,omitempty"`
	Shell     string         `json:"shell,omitempty"`     // bei type=script: powershell|shell
	Script    string         `json:"script,omitempty"`    // bei type=script: Skriptinhalt
	Frequency string         `json:"frequency,omitempty"` // "" = jeden Checkin, sonst Preset
	Platforms []string       `json:"platforms,omitempty"` // windows|linux|darwin; leer = keine Extra-Einschränkung
}

// TaskSpec beschreibt einen geplanten Skript-Task.
type TaskSpec struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Shell           string   `json:"shell"` // powershell|shell
	Script          string   `json:"script"`
	IntervalMinutes int      `json:"interval_minutes"`
	ScheduleType    string   `json:"schedule_type,omitempty"` // "interval" | "daily" (Legacy)
	DailyTime       string   `json:"daily_time,omitempty"`    // "HH:MM" bei daily/weekly
	Weekdays        string   `json:"weekdays,omitempty"`      // "" = alle, sonst z.B. "1,3,5" (0=So)
	Frequency       string   `json:"frequency,omitempty"`     // Preset; überschreibt ScheduleType
	Platforms       []string `json:"platforms,omitempty"`     // windows|linux|darwin; leer = keine Extra-Einschränkung
}

// CheckResult ist das vom Agent gemeldete Ergebnis eines Checks.
type CheckResult struct {
	CheckID string  `json:"check_id"`
	Status  string  `json:"status"` // passing | failing | warning | unknown
	Output  string  `json:"output,omitempty"`
	Value   float64 `json:"value,omitempty"`
}

// TaskResult ist das Ergebnis eines Task-Laufs.
type TaskResult struct {
	TaskID   string    `json:"task_id"`
	ExitCode int       `json:"exit_code"`
	Output   string    `json:"output,omitempty"`
	RanAt    time.Time `json:"ran_at"`
}

// Inventory ist der vollständige Hardware-/Netzwerk-Snapshot eines Geräts.
type Inventory struct {
	Hostname     string      `json:"hostname"`
	OS           string      `json:"os"`
	OSVersion    string      `json:"os_version"`
	Vendor       string      `json:"vendor"`
	Model        string      `json:"model"`
	Serial       string      `json:"serial"`
	CPUModel     string      `json:"cpu_model"`
	CPUCores     int         `json:"cpu_cores"`
	MemoryBytes  uint64      `json:"memory_bytes"`
	UptimeSec    uint64      `json:"uptime_seconds"`
	AgentVersion string      `json:"agent_version"`
	CPUSockets   int         `json:"cpu_sockets,omitempty"`
	CPUThreads   int         `json:"cpu_threads,omitempty"`
	Disks        []Disk      `json:"disks"`
	Interfaces   []Interface `json:"interfaces"`

	GPUs          []string       `json:"gpus,omitempty"`
	PhysicalDisks []PhysicalDisk `json:"physical_disks,omitempty"`
	PublicIP      string         `json:"public_ip,omitempty"`

	Software      []SoftwarePackage `json:"software,omitempty"`
	Printers      []Printer         `json:"printers,omitempty"`
	LoggedInUsers []string          `json:"logged_in_users,omitempty"`
	// OSUpdates ist nil, solange noch kein Update-Check lief (Status "unbekannt").
	OSUpdates *OSUpdateInfo `json:"os_updates,omitempty"`

	CollectedAt time.Time `json:"collected_at"`
}

// OSUpdateInfo beschreibt verfügbare Betriebssystem-Updates/Patches.
type OSUpdateInfo struct {
	Count     int          `json:"count"`
	CheckedAt time.Time    `json:"checked_at"`
	Items     []UpdateItem `json:"items,omitempty"`
}

// UpdateItem ist ein einzelner verfügbarer Patch/Update.
type UpdateItem struct {
	Name     string `json:"name"`
	Severity string `json:"severity,omitempty"` // Critical | Important | Other
	URL      string `json:"url,omitempty"`
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
	Default bool   `json:"default,omitempty"`
}

// Disk beschreibt ein Laufwerk/Volume inkl. Belegung.
type Disk struct {
	Name        string  `json:"name"`
	SizeBytes   uint64  `json:"size_bytes"`
	FreeBytes   uint64  `json:"free_bytes,omitempty"`
	UsedPercent float64 `json:"used_percent,omitempty"`
	FSType      string  `json:"fs_type"`
}

// PhysicalDisk ist ein physisches Laufwerk (Modell + Größe).
type PhysicalDisk struct {
	Model     string `json:"model"`
	SizeBytes uint64 `json:"size_bytes"`
}

// Interface beschreibt eine Netzwerkschnittstelle inkl. MAC und IPs.
type Interface struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}

// Command ist ein an den Agent gerichteter Befehl (spätere Phase).
type Command struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}

// PrinterInfo sind per SNMP ausgelesene Druckerdaten.
type PrinterInfo struct {
	IP          string          `json:"ip"`
	Description string          `json:"description"`
	Model       string          `json:"model"`
	Serial      string          `json:"serial"`
	Firmware    string          `json:"firmware,omitempty"`
	PageCount   int             `json:"page_count"`
	Status      string          `json:"status"`
	Supplies    []PrinterSupply `json:"supplies,omitempty"`
}

// PrinterSupply ist ein Verbrauchsmaterial (Toner/Trommel) mit Füllstand.
type PrinterSupply struct {
	Name  string `json:"name"`
	Level int    `json:"level"` // -2 = unbekannt, -3 = „vorhanden"
	Max   int    `json:"max"`
}

// NetworkHost ist ein beim Netzwerk-Scan gefundener Host.
type NetworkHost struct {
	IP       string `json:"ip"`
	MAC      string `json:"mac,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Ports    []int  `json:"ports,omitempty"`
}

// WaitResponse ist die Antwort des Wake-Long-Polls (/agent/wait). Sie weist den
// Agent an, sich für eine Echtzeit-Session zu melden. Type ist leer ("" = idle,
// Timeout ohne Auftrag), "checkin", "open_terminal" oder "open_vnc".
type WaitResponse struct {
	Type    string `json:"type"`
	Session string `json:"session,omitempty"` // Token der Session
	Shell   string `json:"shell,omitempty"`   // cmd | powershell | shell (oder leer)
	RunAs   string `json:"runas,omitempty"`   // system | user
	// Nur bei "open_vnc": Einmalpasswort für den VNC-Server (RFB-Auth) und ob der
	// angemeldete Nutzer die Verbindung am Gerät bestätigen muss (Zustimmung).
	Password string `json:"password,omitempty"`
	Consent  bool   `json:"consent,omitempty"`
	// Monitor-Auswahl: 0 = alle (virtueller Desktop), 1..N = einzelner Monitor
	// (Default 1 = primär).
	Monitor int `json:"monitor,omitempty"`
}

// TermControl ist ein Steuer-Frame der Terminal-Daten-WS (als Text/JSON gesendet);
// rohe Terminal-I/O läuft dagegen als Binär-Frames.
type TermControl struct {
	Type string `json:"type"`           // resize | exit
	Cols int    `json:"cols,omitempty"` // bei resize
	Rows int    `json:"rows,omitempty"` // bei resize
	Code int    `json:"code,omitempty"` // bei exit: Exit-Code der Shell
}
