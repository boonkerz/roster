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
	// PatchHistory sind die zuletzt gelesenen Patch-Verlaufseinträge (idempotent; der
	// Server verwirft Duplikate). Kommt nur mit, wenn der Agent den Verlauf neu gelesen hat.
	PatchHistory []PatchEvent `json:"patch_history,omitempty"`
	// FIMEvents sind seit dem letzten Checkin erkannte Datei-Änderungen (Datei-Integrität).
	FIMEvents []FIMEvent `json:"fim_events,omitempty"`
	// AuthEvents sind neu gelesene Anmelde-/Kontoereignisse (Anmeldungs-Überwachung).
	AuthEvents []AuthEvent `json:"auth_events,omitempty"`
	// Compliance ist ein frischer Prüfbericht (nur mitgeschickt, wenn neu geprüft wurde).
	Compliance *ComplianceReport `json:"compliance,omitempty"`
	// LogSources ist der Lesestatus der konfigurierten Log-Quellen (gefundene Dateien/
	// Container, Ereignisse, letzter Treffer, Fehler) – damit am Gerät sichtbar ist, ob
	// eine Quelle wirklich gelesen wird.
	LogSources []LogSourceStatus `json:"log_sources,omitempty"`
}

// LogSourceStatus ist der Lesestatus einer Log-Quelle auf einem Gerät.
type LogSourceStatus struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Target    string    `json:"target"`
	Targets   int       `json:"targets"`              // gefundene Dateien bzw. laufende Container
	Events    int       `json:"events"`               // Ereignisse seit Agent-Start
	LastEvent time.Time `json:"last_event,omitempty"` // Null-Zeit = noch keins
	LastRun   time.Time `json:"last_run"`
	Error     string    `json:"error,omitempty"`
}

// ComplianceReport ist das Ergebnis einer Compliance-Prüfung.
type ComplianceReport struct {
	ScannedAt time.Time          `json:"scanned_at"`
	Profile   string             `json:"profile"`
	Results   []ComplianceResult `json:"results"`
}

// ComplianceResult ist das Ergebnis einer Regel.
type ComplianceResult struct {
	RuleID   string `json:"rule_id"`
	Status   string `json:"status"` // pass | fail | na | error
	Evidence string `json:"evidence,omitempty"`
}

// AuthEvent ist ein Anmelde- oder Kontoereignis (sshd, sudo, su, PAM, Windows-
// Sicherheitsprotokoll, macOS log).
type AuthEvent struct {
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"` // login_failed | login_ok | invalid_user | lockout | sudo | su | user_added | user_removed | user_modified | group_changed | password_changed | probe (verdächtige Anfrage, z. B. Web-Scanner)
	User     string    `json:"user,omitempty"`
	SourceIP string    `json:"source_ip,omitempty"`
	Service  string    `json:"service,omitempty"` // sshd | rdp | console | network | sudo | su | pam | system | Name einer Log-Quelle
	Detail   string    `json:"detail,omitempty"`
	RecordID string    `json:"record_id,omitempty"` // Journal-Cursor / Datei-Offset / Windows RecordId – macht den Import idempotent
}

// FIMEvent ist eine von der Datei-Integritätsüberwachung erkannte Änderung.
type FIMEvent struct {
	Time     time.Time `json:"time"`
	RuleID   string    `json:"rule_id,omitempty"`
	Path     string    `json:"path"`
	Change   string    `json:"change"` // created | modified | deleted | attrs | baseline | limit | error
	OldHash  string    `json:"old_hash,omitempty"`
	NewHash  string    `json:"new_hash,omitempty"`
	OldMode  string    `json:"old_mode,omitempty"`
	NewMode  string    `json:"new_mode,omitempty"`
	OldOwner string    `json:"old_owner,omitempty"`
	NewOwner string    `json:"new_owner,omitempty"`
	Size     int64     `json:"size,omitempty"`
	Alert    bool      `json:"alert"` // Regel will Alarm
}

// PatchEvent ist ein Eintrag des Patch-Verlaufs (installiert/aktualisiert/entfernt),
// gelesen aus den Logs des Paketmanagers bzw. der Windows-Update-Historie.
type PatchEvent struct {
	Time       time.Time `json:"time"`
	Action     string    `json:"action"` // install | upgrade | remove
	Name       string    `json:"name"`
	OldVersion string    `json:"old_version,omitempty"`
	NewVersion string    `json:"new_version,omitempty"`
	Source     string    `json:"source,omitempty"` // apt | dnf | yum | pacman | wua | softwareupdate
	Detail     string    `json:"detail,omitempty"` // z. B. Commandline (apt), KB, Ergebnis
}

// MetricsSample ist eine leichte Auslastungs-Momentaufnahme je Checkin (für die
// Verlaufscharts). Prozentwerte 0..100.
type MetricsSample struct {
	CPU  float64  `json:"cpu"`
	Mem  float64  `json:"mem"`
	Disk float64  `json:"disk"`           // am stärksten belegter Datenträger
	Temp *float64 `json:"temp,omitempty"` // Leittemperatur in °C (siehe HeadlineTemperature), nil = keine Sensoren
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
	// Timezone ist der zentral eingestellte IANA-Name ("Europe/Berlin"), in dem der Agent
	// seine Zeitpläne rechnet (daily/weekly, Wochen- und Monatsgrenzen). Leer = lokale
	// Zeit des Geräts, wie vor Einführung der Einstellung. Ältere Agenten ignorieren es.
	Timezone string `json:"timezone,omitempty"`
	// Security bündelt die Sicherheitsüberwachung (Datei-Integrität, Anmeldungen,
	// Compliance) aller zugewiesenen Richtlinien; nil = nichts aktiv.
	Security *SecuritySpec `json:"security,omitempty"`
}

// SecuritySpec ist der Bereich „Sicherheitsüberwachung“ der wirksamen Richtlinie.
type SecuritySpec struct {
	FIM         *FIMSpec        `json:"fim,omitempty"`
	AuthMonitor bool            `json:"auth_monitor,omitempty"`
	Compliance  *ComplianceSpec `json:"compliance,omitempty"`
	// LogSources sind zusätzliche Protokolle (Dateien, Docker-Container), aus denen der
	// Agent Anmelde- und Angriffsereignisse liest (nginx, SFTP-/FTP-Container …).
	LogSources []LogSource `json:"log_sources,omitempty"`
}

// LogSource ist ein zusätzliches Protokoll für die Angriffserkennung.
type LogSource struct {
	ID      string `json:"id"`
	Name    string `json:"name"`              // Dienstname in den Ereignissen (z. B. nginx-proxy, sftp)
	Type    string `json:"type"`              // file | docker
	Target  string `json:"target"`            // Dateipfad (Glob erlaubt) bzw. Container-Name/-Muster
	Format  string `json:"format"`            // nginx_access | nginx_error | sshd | ftp | mail | regex
	Pattern string `json:"pattern,omitempty"` // regex: Muster mit (?P<ip>…) und optional (?P<user>…)
	Kind    string `json:"kind,omitempty"`    // regex: login_failed | probe (Ereignisart je Treffer)
}

// FIMSpec konfiguriert die Datei-Integritätsüberwachung.
type FIMSpec struct {
	IntervalMin int       `json:"interval_min"` // Scan-Abstand in Minuten (Standard 60)
	Rules       []FIMRule `json:"rules"`
}

// FIMRule ist ein überwachter Pfad (Datei oder Verzeichnis, Glob erlaubt).
type FIMRule struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Recursive bool     `json:"recursive"`
	Exclude   []string `json:"exclude,omitempty"`   // Glob-Muster (Pfad oder Dateiname)
	Platforms []string `json:"platforms,omitempty"` // windows|linux|darwin; leer = alle
	Alert     bool     `json:"alert"`               // Änderungen alarmieren
}

// ComplianceSpec konfiguriert die Compliance-Prüfung (Baseline-Regeln).
type ComplianceSpec struct {
	Enabled   bool     `json:"enabled"`
	Profile   string   `json:"profile"` // server | workstation
	Disabled  []string `json:"disabled,omitempty"`
	IntervalH int      `json:"interval_h"` // Prüfabstand in Stunden (Standard 24)
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
	ListenPorts   []ListenPort      `json:"listen_ports,omitempty"`
	// OSUpdates ist nil, solange noch kein Update-Check lief (Status "unbekannt").
	OSUpdates *OSUpdateInfo `json:"os_updates,omitempty"`

	// Docker (nur wo die docker-CLI vorhanden ist): Container + lokale Images.
	Containers []DockerContainer `json:"docker_containers,omitempty"`
	Images     []DockerImage     `json:"docker_images,omitempty"`

	// Temperatursensoren (CPU, Chipsatz, NVMe, GPU …), soweit das System sie liefert.
	// VMs und viele Windows-Rechner haben keine auslesbaren Sensoren.
	Temperatures []Temperature `json:"temperatures,omitempty"`

	// Lüfter (derzeit Linux/hwmon). Unbelegte Anschlüsse (dauerhaft 0 U/min) fehlen.
	Fans []Fan `json:"fans,omitempty"`

	// Proxmox VE (nur auf PVE-Hosts, erkannt über pvesh): VMs/Container samt
	// Backup-Status. nil = kein Proxmox-Host.
	Proxmox *ProxmoxInfo `json:"proxmox,omitempty"`

	// Paketquellen (apt/dnf/pacman/apk/zypper/winget/brew), Status automatischer Updates
	// und Kernel-/OS-Details für Patch-Übersicht und Lebenszyklus (EOL).
	Repos []PackageRepo `json:"repos,omitempty"`
	// AutoUpdates: "" unbekannt/kein Mechanismus | enabled | security | download | notify | disabled.
	AutoUpdates     string `json:"auto_updates,omitempty"`
	KernelRunning   string `json:"kernel_running,omitempty"`   // uname -r
	KernelInstalled string `json:"kernel_installed,omitempty"` // neuester installierter Kernel (Linux)
	OSBuild         string `json:"os_build,omitempty"`         // Windows: „23H2 (22631.4169)“, macOS: Build
	OSCodename      string `json:"os_codename,omitempty"`      // Linux VERSION_CODENAME, Windows DisplayVersion
	OSEdition       string `json:"os_edition,omitempty"`       // Windows: Pro/Enterprise/Server …

	CollectedAt time.Time `json:"collected_at"`
}

// PackageRepo ist eine konfigurierte Paketquelle eines Geräts.
type PackageRepo struct {
	ID         string `json:"id"`   // apt: Datei:Zeile bzw. Suite, dnf: repo id, pacman: Sektion
	Name       string `json:"name"` // Anzeigename (dnf name=, sonst wie ID)
	URL        string `json:"url"`
	Type       string `json:"type"` // apt | dnf | pacman | apk | zypper | winget | brew
	Enabled    bool   `json:"enabled"`
	GPGCheck   bool   `json:"gpg_check"`
	Suite      string `json:"suite,omitempty"` // apt: Suite + Komponenten
	ThirdParty bool   `json:"third_party"`     // nicht von der Distribution
}

// DockerContainer ist ein (laufender oder gestoppter) Docker-Container.
type DockerContainer struct {
	ContainerID string `json:"container_id"`      // kurze ID
	Name        string `json:"name"`              // Container-Name(n)
	Image       string `json:"image"`             // Image-Referenz
	State       string `json:"state"`             // running | exited | paused | created | restarting | dead
	Status      string `json:"status"`            // z. B. "Up 3 hours" / "Exited (0) 2 days ago"
	Ports       string `json:"ports,omitempty"`   // Port-Mappings (Rohstring)
	Compose     string `json:"compose,omitempty"` // com.docker.compose.project (falls gesetzt)
	Created     string `json:"created,omitempty"` // Erstellzeitpunkt (Rohstring)
	Health      string `json:"health,omitempty"`  // healthy | unhealthy | starting | "" (kein Healthcheck)
}

// Temperature ist ein Temperatursensor (Momentaufnahme beim Checkin).
type Temperature struct {
	Sensor   string  `json:"sensor"`             // Schlüssel des Treibers, z. B. coretemp_package_id_0
	Label    string  `json:"label"`              // lesbar, z. B. "CPU Package 0"
	Class    string  `json:"class"`              // cpu | gpu | disk | board | other
	Celsius  float64 `json:"celsius"`            // aktueller Wert
	High     float64 `json:"high,omitempty"`     // Warnschwelle laut Chip (0 = unbekannt)
	Critical float64 `json:"critical,omitempty"` // kritische Schwelle laut Chip (0 = unbekannt)
}

// WarnAt liefert die Warnschwelle in °C (0 = keine bekannt). Viele Treiber melden
// als „max" denselben Wert wie „crit" (z. B. Intel coretemp: beide = TjMax); dann
// gilt 10 °C darunter als Warnung.
func (t Temperature) WarnAt() float64 {
	switch {
	case t.High > 0 && (t.Critical == 0 || t.High < t.Critical):
		return t.High
	case t.Critical > 10:
		return t.Critical - 10
	}
	return 0
}

// Status bewertet den Sensor anhand der Chip-Schwellen: ok | warn | critical |
// unknown (keine Schwellen bekannt).
func (t Temperature) Status() string {
	switch {
	case t.Critical > 0 && t.Celsius >= t.Critical:
		return "critical"
	case t.WarnAt() > 0 && t.Celsius >= t.WarnAt():
		return "warn"
	case t.WarnAt() == 0 && t.Critical == 0:
		return "unknown"
	}
	return "ok"
}

// Fan ist ein Lüfter (Momentaufnahme beim Checkin).
type Fan struct {
	Sensor  string `json:"sensor"`            // eindeutiger Schlüssel, z. B. nct6798_fan2
	Label   string `json:"label"`             // lesbar, z. B. "Mainboard Lüfter 2" oder Chip-Label ("CPU Fan")
	RPM     int    `json:"rpm"`               // aktuelle Drehzahl
	Min     int    `json:"min,omitempty"`     // Mindestdrehzahl laut Chip (0 = nicht gesetzt)
	Max     int    `json:"max,omitempty"`     // Höchstdrehzahl laut Chip (0 = unbekannt)
	Percent *int   `json:"percent,omitempty"` // PWM-Ansteuerung 0–100 %, falls lesbar
	Alarm   bool   `json:"alarm,omitempty"`   // Chip meldet Lüfteralarm
}

// Problem nennt, was an einem Lüfter nicht stimmt ("" = in Ordnung).
func (f Fan) Problem() string {
	switch {
	case f.Alarm:
		return "Alarm"
	case f.RPM == 0:
		return "steht"
	case f.Min > 0 && f.RPM < f.Min:
		return "zu langsam"
	}
	return ""
}

// ProxmoxInfo beschreibt einen Proxmox-VE-Host (bzw. dessen Cluster).
type ProxmoxInfo struct {
	Version  string           `json:"version"`            // pve-manager-Version, z. B. "8.2.4"
	Guests   []ProxmoxGuest   `json:"guests"`             // clusterweit (pvesh /cluster/resources)
	Storages []ProxmoxStorage `json:"storages,omitempty"` // Speicher, die Backups aufnehmen
}

// ProxmoxStorage ist ein Speicher mit Backup-Inhalt – Futter für die Zielauswahl im
// Backup-Formular, damit der Speichername dort kein Freitext sein muss.
type ProxmoxStorage struct {
	Name   string `json:"name"`
	Node   string `json:"node"`
	Shared bool   `json:"shared,omitempty"`
}

// ProxmoxGuest ist eine VM (qemu) oder ein Container (lxc) samt Backup-Status.
type ProxmoxGuest struct {
	Node     string  `json:"node"`
	VMID     int     `json:"vmid"`
	Type     string  `json:"type"`   // qemu | lxc
	Name     string  `json:"name"`   // Anzeigename
	Status   string  `json:"status"` // running | stopped | paused …
	Template bool    `json:"template,omitempty"`
	CPUs     float64 `json:"cpus,omitempty"`    // zugewiesene vCPUs
	CPU      float64 `json:"cpu,omitempty"`     // Auslastung 0..1 (nur laufend)
	Mem      uint64  `json:"mem,omitempty"`     // belegter RAM (Bytes)
	MaxMem   uint64  `json:"maxmem,omitempty"`  // zugewiesener RAM (Bytes)
	MaxDisk  uint64  `json:"maxdisk,omitempty"` // Plattengröße (Bytes)
	Uptime   uint64  `json:"uptime,omitempty"`  // Sekunden

	// Backup-Status: letztes vorhandenes Backup auf einem Backup-Speicher ...
	BackupAt      *time.Time `json:"backup_at,omitempty"`
	BackupSize    uint64     `json:"backup_size,omitempty"`
	BackupStorage string     `json:"backup_storage,omitempty"`
	BackupCount   int        `json:"backup_count,omitempty"`
	// ... Ergebnis des letzten vzdump-Laufs, der diesen Gast enthielt ...
	BackupTaskStatus string     `json:"backup_task_status,omitempty"` // ok | failed | "" (kein Lauf bekannt)
	BackupTaskAt     *time.Time `json:"backup_task_at,omitempty"`
	BackupTaskMsg    string     `json:"backup_task_msg,omitempty"`
	// ... und ob er überhaupt in einem Backup-Job steckt (nil = unbekannt).
	BackupJob *bool `json:"backup_job,omitempty"`
}

// DockerImage ist ein lokal vorhandenes Docker-Image.
type DockerImage struct {
	Repository string `json:"repository"`        // Repository (oder "<none>")
	Tag        string `json:"tag"`               // Tag (oder "<none>")
	ImageID    string `json:"image_id"`          // kurze Image-ID
	Size       string `json:"size,omitempty"`    // menschlich (z. B. "123MB")
	Created    string `json:"created,omitempty"` // Alter (z. B. "2 weeks ago")
}

// ListenPort ist ein lauschender Socket des Geräts (Angriffsfläche). Public=true,
// wenn er nicht nur an Loopback gebunden ist (also grundsätzlich vom Netz aus
// erreichbar – ob wirklich „von außen", hängt zusätzlich von NAT/Firewall ab).
type ListenPort struct {
	Proto   string `json:"proto"`   // tcp | tcp6 | udp | udp6
	Address string `json:"address"` // Bind-Adresse (z. B. 0.0.0.0, ::, 127.0.0.1)
	Port    int    `json:"port"`
	Process string `json:"process,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Public  bool   `json:"public"` // nicht nur an Loopback gebunden
}

// OSUpdateInfo beschreibt verfügbare Betriebssystem-Updates/Patches.
type OSUpdateInfo struct {
	Count int `json:"count"`
	// SecurityCount zählt die Einträge mit Security=true (apt -security-Suite,
	// dnf-Security-Advisory, WUA-Kategorie „Security Updates“ …).
	SecurityCount int          `json:"security_count,omitempty"`
	CheckedAt     time.Time    `json:"checked_at"`
	Items         []UpdateItem `json:"items,omitempty"`
}

// UpdateItem ist ein einzelner verfügbarer Patch/Update.
type UpdateItem struct {
	Name     string `json:"name"`
	Severity string `json:"severity,omitempty"` // Critical | Important | Other
	URL      string `json:"url,omitempty"`
	// Version ist die verfügbare (neue) Version, CurrentVersion die installierte –
	// soweit der Paketmanager sie nennt.
	Version        string `json:"version,omitempty"`
	CurrentVersion string `json:"current_version,omitempty"`
	// Source nennt die Herkunft: apt-Suite(n), dnf-Repo/Advisory, WUA-Kategorien/KB.
	Source string `json:"source,omitempty"`
	// Security markiert Sicherheits-Updates (Quelle siehe OSUpdateInfo.SecurityCount).
	Security bool `json:"security,omitempty"`
	// Kind unterscheidet unter Windows: "" (OS/Software) | driver | definition.
	Kind string `json:"kind,omitempty"`
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
