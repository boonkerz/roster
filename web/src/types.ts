// Typen spiegeln die JSON-Antworten der Go-API wider.

export type Role = "admin" | "technician" | "viewer";

export interface User {
  id: string;
  username: string;
  email: string;
  role: Role;
  custom_role_id?: string;
  permissions?: string[];
  auth_source: "local" | "ldap";
  theme: "light" | "dark" | "";
  created_at: string;
  last_login?: string;
  totp_enabled?: boolean;
  require_2fa?: boolean;
}

// Permission-Keys (Spiegel von internal/server/model/permissions.go).
export const PERMISSIONS = [
  "page.dashboard", "page.devices", "page.policies", "page.scripts", "page.settings", "devices.operate",
] as const;
export type Permission = (typeof PERMISSIONS)[number];

export interface CustomRole {
  id: string;
  name: string;
  permissions: string[];
  created_at: string;
  user_count: number;
}

export interface Interface {
  name: string;
  mac: string;
  ipv4: string;
  ipv6: string;
}

export interface ListenPort {
  proto: string;
  address: string;
  port: number;
  process?: string;
  pid?: number;
  public: boolean;
  ext_checked?: boolean;
  ext_reachable?: boolean;
}

export interface DockerContainer {
  container_id: string;
  name: string;
  image: string;
  state: string;   // running | exited | paused | ...
  status: string;  // "Up 3 hours" / "Exited (0) 2 days ago"
  ports?: string;
  compose?: string;
  created?: string;
  health?: string; // healthy | unhealthy | starting | ""
}

export interface DockerImage {
  repository: string;
  tag: string;
  image_id: string;
  size?: string;
  created?: string;
  created_at?: string;
}

// DockerImageScan ist die gescannte Paketliste eines Images inkl. OSV-Trefferzahlen.
export interface DockerImageScan {
  image_id: string;
  image: string;
  os?: string;
  os_version?: string;
  os_name?: string;
  pkg?: string; // apk | dpkg | rpm | none
  package_count: number;
  services?: string[];
  error?: string;
  scanned_at: string;
  vuln_scanned_at?: string;
  vulns: number;
  critical: number;
  high: number;
  fixable: number;
}

// DockerDatabase ist eine Datenbank in einem DB-Container (Bestand des letzten Checkins).
export interface DockerDatabase {
  container_id: string;
  container: string;
  engine: string; // mongo | mysql | postgres
  name?: string;
  collections: number;
  error?: string;
  updated_at: string;
}

// FleetImageRow fasst ein Image über alle Geräte zusammen (Sicherheit → Container).
export interface FleetImageRow {
  image: string;
  os?: string;
  os_version?: string;
  os_name?: string;
  hosts: number;
  devices: string[];
  hostnames: string[];
  packages: number;
  vulns: number;
  critical: number;
  high: number;
  fixable: number;
  fixable_ch: number;
  services?: string[];
  error?: string;
  pending: boolean;
}

export interface FleetDBRow {
  device_id: string;
  hostname: string;
  container: string;
  engine: string;
  error?: string;
  databases: DockerDatabase[];
}

export interface FleetContainers {
  images: FleetImageRow[];
  databases: FleetDBRow[];
}

// ContainerVulnerability ist ein OSV-Treffer in einem Container-Image.
export interface ContainerVulnerability {
  device_id?: string;
  hostname?: string;
  image_id: string;
  image: string;
  package: string;
  version: string;
  vuln_id: string;
  severity: string;
  summary: string;
  fixed: string;
  url: string;
  detected_at: string;
}

export interface Group {
  id: string;
  name: string;
  description: string;
  parent_id?: string;
  device_count?: number;
  rule: string; // JSON-Regel einer Smart Group (leer = statische Gruppe)
}

export interface SoftwarePackage {
  name: string;
  version: string;
  publisher?: string;
}

export interface Printer {
  name: string;
  driver?: string;
  port?: string;
  default: boolean;
}

export interface Site {
  id: string;
  client_id: string;
  name: string;
  device_count: number;
}

export interface Client {
  id: string;
  name: string;
  device_count: number;
  sites?: Site[];
}

export interface ClientTree {
  clients: Client[];
  unassigned_count: number;
}

export interface Script {
  id: string;
  name: string;
  shell: "powershell" | "shell";
  platforms?: string[]; // windows | linux | darwin; leer = keine Einschränkung
  content: string;
  check_only?: boolean;
  created_at: string;
}

// PolicyBackup ist ein Backup-Eintrag einer Richtlinie (serverseitig geplant).
export interface PolicyBackup {
  id: string;
  policy_id: string;
  name: string;
  type: "proxmox" | "script";
  enabled: boolean;
  config: Record<string, unknown>;
  script_id?: string | null;
  weekdays: string;   // "1,3,5" (0 = Sonntag), leer = täglich
  at_time: string;    // "18:00" in der zentral eingestellten Zeitzone
  catch_up_minutes: number;
  wait_device_id?: string | null;
  wait_minutes: number;
  timeout_minutes: number;
  after_device_id?: string | null;
  after_script_id?: string | null;
  after_when: "always" | "success" | "failure";
  last_run_at?: string;
}

// BackupRun ist ein einzelner Backup-Lauf.
export interface BackupRun {
  id: string;
  backup_id: string;
  backup_name?: string;
  device_id: string;
  trigger_type: "schedule" | "manual";
  status: "waiting" | "running" | "ok" | "failed" | "timeout" | "skipped";
  scheduled_at: string;
  wait_until?: string;
  started_at?: string;
  finished_at?: string;
  exit_code: number;
  summary: string;
  output?: string;
}

export interface PolicyCheck {
  id: string;
  policy_id: string;
  name: string;
  type: string;
  config: Record<string, number | string | boolean>;
  script_id?: string;
  remediation_script_id?: string | null;
  remediation_proxmox?: ProxmoxRemediation | null;
  severity?: "warning" | "critical";
  frequency?: string;
}

// ProxmoxHost ist der Zugang zu einer Proxmox-VE-API (Token-Secret nie im Client).
export interface ProxmoxHost {
  id: string;
  name: string;
  base_url: string;
  token_id: string;
  verify_tls: boolean;
}

// ProxmoxStorage ist ein backup-fähiger Speicher des PVE-Clusters – die Zielauswahl
// im Backup-Formular.
export interface ProxmoxStorage {
  name: string;
  node: string;
  shared?: boolean;
}

// ProxmoxGuest ist ein Container/VM eines Proxmox-Hosts. Aus der API-Integration
// kommen nur die Grunddaten; vom Agent auf einem PVE-Host (Gerät) zusätzlich
// Ressourcen und Backup-Status.
export interface ProxmoxGuest {
  node: string;
  vmid: number;
  type: "lxc" | "qemu";
  name: string;
  status: string;
  template?: boolean;
  cpus?: number;
  cpu?: number;
  mem?: number;
  maxmem?: number;
  maxdisk?: number;
  uptime?: number;
  backup_at?: string;
  backup_size?: number;
  backup_storage?: string;
  backup_count?: number;
  backup_task_status?: "ok" | "failed" | "";
  backup_task_at?: string;
  backup_task_msg?: string;
  backup_job?: boolean; // fehlt = unbekannt
}

// Temperature ist ein Temperatursensor eines Geräts (Momentaufnahme vom Checkin).
export interface Temperature {
  sensor: string;
  label: string;
  class: "cpu" | "gpu" | "disk" | "board" | "other";
  celsius: number;
  high?: number;     // Warnschwelle laut Chip
  critical?: number; // kritische Schwelle laut Chip
}

// Fan ist ein Lüfter eines Geräts (derzeit Linux/hwmon).
export interface Fan {
  sensor: string;
  label: string;
  rpm: number;
  min?: number;
  max?: number;
  percent?: number; // PWM-Ansteuerung 0–100 %
  alarm?: boolean;
}

// fanProblem nennt, was an einem Lüfter nicht stimmt (wie shared.Fan.Problem).
export function fanProblem(f: Fan): "" | "Alarm" | "steht" | "zu langsam" {
  if (f.alarm) return "Alarm";
  if (f.rpm === 0) return "steht";
  if (f.min && f.rpm < f.min) return "zu langsam";
  return "";
}

// ProxmoxRemediation zeigt auf einen Gast, der bei Check-Fehler rebootet wird.
export interface ProxmoxRemediation {
  host_id: string;
  node: string;
  type: "lxc" | "qemu";
  vmid: number;
}

export type CustomFieldType = "text" | "number" | "checkbox" | "select" | "multiselect" | "datetime" | "list";

export interface CustomField {
  id: string;
  model: "client" | "site" | "device";
  name: string;
  type: CustomFieldType;
  options: string[];
  default_value: string;
  required: boolean;
  managed?: boolean;          // agent-verwaltet -> im Editor schreibgeschützt
  link?: boolean;             // Listen-Einträge als Links rendern
  selection_field?: string;   // Name des Begleit-Listenfelds für die Auswahl (leer = keine)
}

export interface CustomFieldValue {
  field: CustomField;
  value: string;
}

export interface PolicyTask {
  id: string;
  policy_id: string;
  name: string;
  script_id?: string;
  interval_minutes: number;
  schedule_type: "interval" | "daily";
  daily_time: string;
  weekdays: string;
  frequency?: string;
  collect_fields?: boolean;
}

export interface Command {
  id: string;
  device_id: string;
  type: string;
  label: string;
  status: "pending" | "sent" | "done";
  created_at: string;
  exit_code: number;
  output?: string;
  ran_at?: string;
}

export interface AlertField {
  key: string;
  label: string;
  type: "text" | "password" | "number" | "checkbox";
  required?: boolean;
  help?: string;
}

export interface AlertProvider {
  type: string;
  label: string;
  fields: AlertField[];
}

export interface ChannelScope {
  target_type: "client" | "site" | "device";
  target_id: string;
}

export interface AlertChannel {
  id: string;
  type: string;
  name: string;
  enabled: boolean;
  config: Record<string, string>;
  min_severity: "warning" | "critical";
  assignments: ChannelScope[];
}

export interface AlertsResponse {
  enabled: boolean;
  alert_software: boolean;
  alert_after_remediation: boolean; // Checks mit Auto-Remediation erst nach deren Lauf melden
  channels: AlertChannel[];
}

export interface AlertConfig {
  enabled: boolean;
  smtp_host: string;
  smtp_port: number;
  smtp_user: string;
  smtp_pass?: string;
  smtp_from: string;
  smtp_tls: boolean;
  recipient: string;
  webhook_url: string;
}

export interface Assignment {
  id: string;
  policy_id: string;
  target_type: "client" | "site" | "device";
  target_id: string;
}

export interface Policy {
  id: string;
  name: string;
  description: string;
  checks?: PolicyCheck[];
  tasks?: PolicyTask[];
  backups?: PolicyBackup[];
  notifications?: PolicyNotifications;
  security?: PolicySecurity;
  fim_rules?: PolicyFIMRule[];
  responses?: PolicyResponse[];
  log_sources?: PolicyLogSource[];
  assignments?: Assignment[];
}

// PolicyLogSource ist ein zusätzliches Protokoll (Datei/Docker-Container) für die Angriffserkennung.
export interface PolicyLogSource {
  id: string;
  policy_id: string;
  name: string;
  type: "file" | "docker" | "journal" | "docker_events";
  target: string;
  format: "nginx_access" | "apache_access" | "nginx_error" | "sshd" | "ftp" | "mail" | "haproxy" | "traefik" | "mysql" | "firewall" | "docker_events" | "regex";
  pattern?: string;
  kind?: "login_failed" | "probe";
  enabled: boolean;
}

// Bereich „Sicherheitsüberwachung“ einer Richtlinie.
export interface PolicySecurity {
  fim_interval_min: number;
  auth_monitor: boolean;
  compliance_enabled: boolean;
  compliance_profile: "server" | "workstation";
  compliance_disabled: string[];
  compliance_interval_h: number;
  fim_whodata: boolean;   // Verursacher ermitteln (auditd / SACL+4663)
  state_monitor: boolean; // Systemzustand überwachen
  rootcheck: boolean;     // Rootkit-/Malware-Prüfung
}

export interface PolicyFIMRule {
  id: string;
  policy_id: string;
  path: string;
  recursive: boolean;
  exclude: string[];
  platforms: string[];
  alert: boolean;
}

// Compliance-Regelkatalog und Ergebnisse.
export interface ComplianceRule {
  id: string;
  title: string;
  title_en: string;
  description: string;
  severity: "low" | "medium" | "high";
  category: string;
  platforms: string[];
  profiles: string[];
  remediation: string;
  frameworks: string[];    // z. B. "CIS 5.2.10", "PCI DSS 8.6"
  description_en?: string;
  remediation_en?: string;
}
export interface ComplianceResult { device_id: string; hostname?: string; rule_id: string; status: "pass" | "fail" | "na" | "error"; evidence?: string; checked_at: string; }
export interface ComplianceDevice { device_id: string; hostname: string; os: string; client_name?: string; site_name?: string; score: number; profile: string; passed: number; failed: number; skipped: number; checked_at: string; }
export interface ComplianceRuleStat { rule_id: string; passed: number; failed: number; skipped: number; }

// AuthEvent ist ein Anmelde-/Kontoereignis (Anmeldungs-Überwachung).
export interface AuthEvent {
  id: string;
  device_id: string;
  hostname?: string;
  client_name?: string;
  ts: string;
  kind: string;
  username?: string;
  source_ip?: string;
  country?: string;
  threat?: string[]; // Blocklisten, in denen die Quell-IP steht
  service?: string;
  detail?: string;
}

// SecurityIncident ist ein abgeleiteter Sicherheitsvorfall.
export interface MitreTechnique { id: string; name: string; tactic: string }
// IncidentKindInfo ist ein Eintrag des Katalogs der Vorfallsarten (GET /security/kinds).
export interface IncidentKindInfo { kind: string; default_severity: "info" | "warning" | "critical"; techniques: MitreTechnique[] }

export interface SecurityIncident {
  id: string;
  device_id: string;
  hostname?: string;
  client_name?: string;
  site_name?: string;
  ts: string;
  kind: "brute_force" | "brute_force_success" | "new_source_ip" | "account_change" | "lockout" | "fim_change" | "scan" | "portscan" | "threat_listed" | "persistence" | "audit_cleared" | "suspicious_process" | "state_change" | "rootkit" | "malware" | "container_exec" | "db_change" | string;
  severity: "info" | "warning" | "critical";
  mitre?: MitreTechnique[]; // ATT&CK-Techniken der Art
  threat?: string[]; // Blocklisten, in denen die Quell-IP steht
  source_ip?: string;
  country?: string; // ISO-3166-Code der Quell-IP
  username?: string;
  title: string;
  detail?: string;
  status: "open" | "acknowledged" | "resolved";
  response?: string;
  response_status?: string;
  response_command_id?: string;
  acknowledged_by?: string;
  acknowledged_at?: string;
}

export interface CountryCount { country: string; ips: number; count: number }
export interface SecuritySummary {
  open_incidents: number;
  critical_open: number;
  failed_logins_24h: number;
  devices_monitored: number;
  top_ips: { ip: string; country?: string; count: number }[];
  top_countries: CountryCount[];     // Herkunft der Fehlversuche (24 h)
  blocked_countries: CountryCount[]; // Länder gesperrter IPs (30 Tage)
}

// Log-Quelle (ungespeichert) für den Probelauf auf einem Gerät.
export interface LogSourceSpec { name: string; type: PolicyLogSource["type"]; target: string; format: PolicyLogSource["format"]; pattern?: string; kind?: string }
export interface LogSourceTestResult {
  targets: { target: string; lines: number; matched: number; error?: string }[];
  lines: number;
  matched: number;
  events: { time: string; kind: string; user?: string; source_ip?: string; service?: string; detail?: string }[];
  error?: string;
  hints?: string[];
}

// Auf einem Gerät wirksame Log-Quelle mit dem vom Agent gemeldeten Lesestatus.
export interface DeviceLogSource {
  id: string;
  policy_id?: string;
  policy_name?: string;
  name: string;
  type: PolicyLogSource["type"];
  target: string;
  format?: PolicyLogSource["format"];
  pattern?: string;
  kind?: string;
  reported: boolean;
  targets: number;
  events: number;
  last_event_at?: string;
  last_run_at?: string;
  error?: string;
}
export interface SecurityMonitoring {
  auth_monitor: boolean;
  auth_monitor_since?: string;
  fim_rules: number;
  compliance: boolean;
  log_sources: DeviceLogSource[];
  fim_whodata: boolean;
  state_monitor: boolean;
  rootcheck: boolean;
}

export interface SecuritySettings {
  bf_threshold: number;
  bf_window_min: number;
  new_ip_alert: boolean;
  new_ip_ignore_private: boolean;
  learn_days: number;
  never_block: string[];
  response_cooldown_min: number;
  invalid_user_max: number;
  scan_threshold: number;
  auto_block: boolean;
  auto_block_minutes: number;
  fleet_block: boolean;
  subnet_ips: number;
  block_private: boolean; // auch private/interne Adressen automatisch sperren
  severity: Record<string, string>; // Schweregrad-Override je Vorfallsart ("" = Standard)
  threat_enabled: boolean; // Blocklisten abgleichen
  threat_block: boolean;   // gelistete IPs beim ersten Ereignis flottenweit sperren
  threat_feeds: string[];  // Feed-IDs, leer = alle
  portscan_ports: number;  // verschiedene Zielports je IP im Fenster bis zum Vorfall „Portscan“
}

export interface ThreatFeedStatus { id: string; name: string; url: string; enabled: boolean; entries: number; fetched_at?: string; error?: string }
export interface ThreatStatus { enabled: boolean; block: boolean; active: boolean; feeds: ThreatFeedStatus[] }

// PolicyResponse ist eine Active-Response-Regel (Auslöser → Aktion).
export interface PolicyResponse {
  id: string;
  policy_id: string;
  trigger: string;
  action: "block_ip" | "lock_user" | "run_script";
  minutes: number;
  script_id?: string;
  platforms: string[];
  enabled: boolean;
}

// IPBlock ist eine (angestoßene) IP-Sperre.
export interface IPBlock {
  id: string;
  device_id: string;
  hostname?: string;
  ip: string;
  reason?: string;
  incident_id?: string;
  until?: string;
  status: "pending" | "active" | "expired" | "released" | "failed";
  country?: string;
  detail?: string;
  command_id?: string;
  created_by: string;
  created_at: string;
  threat?: string[]; // Blocklisten, in denen die IP steht
}

// FIMEvent ist eine erkannte Datei-Änderung (Datei-Integrität).
export interface FIMEvent {
  id: string;
  device_id: string;
  hostname?: string;
  client_name?: string;
  ts: string;
  rule_id?: string;
  path: string;
  change: "created" | "modified" | "deleted" | "attrs" | "baseline" | "limit";
  old_hash?: string;
  new_hash?: string;
  old_mode?: string;
  new_mode?: string;
  old_owner?: string;
  new_owner?: string;
  size: number;
  alert: boolean;
  acknowledged: boolean;
  user?: string;    // Who-Data: Benutzer der Änderung
  process?: string; // Who-Data: Prozess der Änderung
}

// Bereich „Benachrichtigungen“ einer Richtlinie.
export interface PolicyNotifications {
  software_alerts: "" | "on" | "off"; // "" = Standard (globale Einstellung)
  software_alert_kinds: string[];      // added | removed | updated
  software_ignore: string[];           // Glob-Muster auf Programmnamen
  security_alert_kinds: string[];
  security_alert_skip_blocked: boolean;
}

// Wirksame Software-Alarm-Einstellung eines Geräts (GET /devices/{id}/software-alerts).
export interface DeviceSoftwareAlerts {
  device: "" | "on" | "off";
  enabled: boolean;
  inherited: boolean; // Wert ohne Geräte-Override
  source: "device" | "policy" | "global";
  policy_id?: string;
  policy_name?: string;
  kinds: string[];
  ignore: string[];
}

export interface FailingCheck {
  id: string;
  name: string;
  type: string;
}

export interface CheckResult {
  check_id: string;
  status: string;
  output?: string;
  value?: number;
  updated_at: string;
  name?: string;
  type?: string;
}

// Verlauf eines Checks am Gerät (ein Punkt je Checkin bzw. je Zeitfenster).
export interface CheckHistoryPoint { ts: number; value: number; min: number; max: number; status: string; n: number }
export interface CheckHistoryResponse { from: number; to: number; bucket_ms: number; points: CheckHistoryPoint[] }

export interface TaskResult {
  id: string;
  task_id: string;
  exit_code: number;
  output?: string;
  ran_at: string;
  name?: string;
}

export interface Disk {
  name: string;
  size_bytes: number;
  free_bytes: number;
  used_percent: number;
  fs_type: string;
}

export interface PhysicalDisk {
  model: string;
  size_bytes: number;
}

export interface UpdateItem {
  name: string;
  severity: string;
  url?: string;
  approved: boolean;
  version?: string;
  current_version?: string;
  source?: string;
  security?: boolean;
  kind?: string; // "" | driver | definition (Windows)
  phased?: boolean; // Ubuntu: per Phasing zurückgestellt
}

// PendingPatch ist eine Zeile der flottenweiten Patch-Übersicht (Gerät × Update).
export interface PendingPatch {
  device_id: string;
  hostname: string;
  os: string;
  site_id?: string;
  site_name?: string;
  client_id?: string;
  client_name?: string;
  name: string;
  severity: string;
  url?: string;
  version?: string;
  current_version?: string;
  source?: string;
  security: boolean;
  kind?: string;
  approved: boolean;
  phased?: boolean;
}

// PackageRepo ist eine Paketquelle (apt/dnf/zypper/pacman/apk/winget/brew).
export interface PackageRepo {
  device_id?: string;
  hostname?: string;
  client_name?: string;
  id: string;
  name: string;
  url: string;
  type: string;
  enabled: boolean;
  gpg_check: boolean;
  suite?: string;
  third_party: boolean;
}

export interface PackageVersionCount { name: string; version: string; devices: number; }
export interface PackageDevice { device_id: string; hostname: string; os: string; client_name?: string; site_name?: string; version: string; }

// PatchEvent ist ein Eintrag des Patch-Verlaufs (install | upgrade | remove).
export interface PatchEvent {
  id: string;
  device_id: string;
  hostname?: string;
  client_name?: string;
  site_name?: string;
  ts: string;
  action: "install" | "upgrade" | "remove";
  name: string;
  old_version?: string;
  new_version?: string;
  source?: string;
  detail?: string;
}

export interface PatchSummary {
  devices_total: number;
  devices_checked: number;
  devices_pending: number;
  devices_security: number;
  pending: number;
  security: number;
  approved: number;
  compliant_pct: number;
}

export interface Device {
  id: string;
  hostname: string;
  os: string;
  os_version: string;
  vendor: string;
  model: string;
  serial: string;
  cpu_model: string;
  cpu_cores: number;
  cpu_sockets?: number;
  cpu_threads?: number;
  memory_bytes: number;
  agent_version: string;
  public_ip?: string;
  disks?: Disk[];
  physical_disks?: PhysicalDisk[];
  gpus?: string[];
  first_seen: string;
  last_seen?: string;
  enrolled_at: string;
  revoked: boolean;
  status: "online" | "offline" | "unknown" | "unmanaged";
  managed?: boolean;
  site_id?: string | null;
  site_name?: string;
  client_id?: string;
  client_name?: string;
  checks_total?: number;
  checks_failing?: number;
  failing_checks?: FailingCheck[]; // gerade fehlschlagende Checks (für Filter „Check fehlerhaft“)
  tasks_total?: number;
  tasks_failing?: number;
  vuln_count?: number;
  security_updates_count?: number | null;
  auto_updates?: "" | "enabled" | "security" | "download" | "notify" | "disabled";
  kernel_running?: string;
  kernel_installed?: string;
  os_build?: string;
  os_codename?: string;
  os_edition?: string;
  repos?: PackageRepo[];
  eol_status?: "ok" | "soon" | "eol" | "unknown";
  eol_date?: string;
  fim_open?: number;
  incidents_open?: number;
  compliance_score?: number | null;
  compliance_checked_at?: string;
  assigned_checks?: number;
  assigned_tasks?: number;
  check_results?: CheckResult[];
  task_results?: TaskResult[];
  commands?: Command[];
  logged_in_users?: string[];
  updates_count?: number | null;
  updates_checked_at?: string;
  available_updates?: UpdateItem[];
  interfaces?: Interface[];
  listen_ports?: ListenPort[];
  docker_containers?: DockerContainer[];
  docker_images?: DockerImage[];
  docker_image_scans?: DockerImageScan[];
  docker_dbs?: DockerDatabase[];
  temperatures?: Temperature[];
  fans?: Fan[];
  proxmox_version?: string; // gesetzt = Agent läuft auf einem Proxmox-VE-Host
  proxmox_guests?: ProxmoxGuest[];
  proxmox_storages?: ProxmoxStorage[];
  software_alerts?: "" | "on" | "off"; // Override: "" = wie Richtlinie/Standard
  groups?: Group[];
  software?: SoftwarePackage[];
  printers?: Printer[];
  notes?: string;
}

export interface CheckEvent {
  id: string;
  device_id: string;
  hostname?: string;
  check_id: string;
  check_name: string;
  old_status: string;
  new_status: string;
  output?: string;
  notified: boolean;
  notified_at?: string;
  created_at: string;
}

export interface ReportSchedule {
  id: string;
  title: string;
  frequency: "daily" | "weekly" | "monthly";
  channel_id: string;
  channel_name?: string;
  last_run?: string;
  created_at: string;
}

export interface AuditEntry {
  id: string;
  ts: string;
  user_id?: string;
  username: string;
  action: string;
  method: string;
  path: string;
  status: number;
  ip?: string;
}

export interface MaintenanceWindow {
  id: string;
  target_type: "client" | "site" | "device";
  target_id: string;
  target_name?: string;
  note?: string;
  starts_at: string;
  ends_at: string;
  created_at: string;
}

export interface DashboardSummary {
  devices_total: number;
  devices_online: number;
  devices_offline: number;
  devices_unknown: number;
  devices_with_failing_checks: number;
  failing_checks: number;
  devices_with_failing_tasks: number;
  failing_tasks: number;
  devices_with_pending_patches: number;
  pending_patches: number;
  devices_with_security_patches: number;
  security_patches: number;
  devices_eol?: number;
  devices_eol_soon?: number;
  open_incidents?: number;
  failed_logins_24h?: number;
  compliance_avg?: number;
  devices_low_compliance?: number;
  devices_compliance_checked?: number;
  devices_with_vulns: number;
  vulnerabilities: number;
  recent_events: CheckEvent[];
}

export interface SoftwareEvent {
  id: string;
  device_id: string;
  change: "added" | "removed" | "updated";
  name: string;
  version?: string;
  old_version?: string;
  created_at: string;
}

export interface PrinterSupply { name: string; level: number; max: number; }
export interface PrinterInfo {
  ip: string;
  description: string;
  model: string;
  serial: string;
  firmware?: string;
  page_count: number;
  status: string;
  supplies?: PrinterSupply[];
}

export interface NetworkAsset {
  id: string;
  site_id: string;
  ip: string;
  mac: string;
  hostname: string;
  ports: string;
  note: string;
  managed: boolean;
  first_seen: string;
  last_seen: string;
}

export interface Vulnerability {
  device_id?: string;
  hostname?: string;
  package: string;
  version: string;
  vuln_id: string;
  severity: string;
  summary: string;
  fixed: string;
  url: string;
  detected_at: string;
}

export interface DeployPackage {
  id: string;
  name: string;
  winget: string;
  choco: string;
  apt: string;
  dnf: string;
  brew: string;
  pacman: string;
  apk: string;
}

export interface EnrollmentToken {
  id: string;
  label: string;
  token?: string;
  expires_at?: string;
  max_uses: number;
  used_count: number;
  created_by: string;
  created_at: string;
}

// GeneralSettings sind die globalen Einstellungen (bislang nur die Zeitzone).
export interface GeneralSettings {
  timezone: string;     // IANA-Name, leer = Systemzeit des jeweiligen Geräts
  server_now?: string;  // aktuelle Zeit in der wirksamen Zone (nur lesend)
  effective?: string;   // tatsächlich verwendete Zone
  eol_warn_days?: number; // Vorwarnzeit Support-Ende (Tage)
  eol_alerts?: boolean;   // Alarme bei nahendem/erreichtem Support-Ende
}

// DeviceEOL ist die Lebenszyklus-Bewertung eines Geräts (OS bzw. Proxmox VE).
export interface DeviceEOL {
  device_id: string;
  hostname?: string;
  os?: string;
  os_version?: string;
  client_name?: string;
  site_name?: string;
  kind: "os" | "proxmox";
  product: string;
  cycle: string;
  label?: string;
  eol_date?: string;
  support_date?: string;
  status: "ok" | "soon" | "eol" | "unknown";
  checked_at: string;
}

// --- Dashboard-Diagramme (konfigurierbar, im Benutzerkonto gespeichert) ---
export type StatSourceKind = "summary" | "check" | "field" | "events";
export interface StatSource { id: string; name: string; type?: string }
export interface StatSources { summary: string[]; checks: StatSource[]; fields: StatSource[] }
export interface StatPoint { ts: number; label: string; value: number }
export interface StatsResponse { current: { label: string; value: number }[]; history?: StatPoint[] }
export type DashChart = "bar" | "donut" | "line" | "list";
export type DashRange = "24h" | "7d" | "30d" | "90d" | "365d";
export interface DashWidget {
  id: string;
  title: string;          // leer = Name der Quelle
  source: StatSourceKind;
  key: string;            // Kennzahl, Check-ID oder Feld-ID
  chart: DashChart;
  range: DashRange;       // nur für Verlauf
  width?: DashWidth;      // Spalten von 4 (Standard 2 = halbe Zeile)
}
export type DashWidth = 1 | 2 | 3 | 4;

// --- Abläufe (Flow-Designer) ---
export type FlowNodeType = "find_files" | "replace" | "compose" | "command" | "http" | "check" | "rollback" | "wait" | "read_value" | "yaml";
export interface FlowNodeDef {
  id: string;
  type: FlowNodeType;
  label?: string;
  config?: Record<string, string | number | boolean>;
  x?: number;
  y?: number;
}
export interface FlowEdgeDef { from: string; port: "ok" | "fail"; to: string }
export interface FlowGraph { nodes: FlowNodeDef[]; edges: FlowEdgeDef[] }
export interface FlowParam { name: string; label?: string; default?: string; required?: boolean }
export interface Flow {
  id: string;
  name: string;
  description: string;
  graph: FlowGraph;
  params: FlowParam[];
  created_by?: string;
  updated_at: string;
}
export interface FlowTemplate { key: string; name: string; description: string; graph: FlowGraph; params: FlowParam[] }
export interface FlowNodeResult {
  id: string;
  type: string;
  label?: string;
  status: "ok" | "fail" | "dry" | "empty" | "skipped";
  output?: string;
  diff?: string;
  files?: string[];
  duration_ms: number;
}
export interface FlowResult {
  status: "ok" | "fail" | "rolled_back" | "dry" | "nothing";
  nodes: FlowNodeResult[];
  rolled_back?: string[];
  error?: string;
}
export interface FlowRunDevice {
  device_id: string;
  hostname: string;
  stage: "preview" | "test" | "rollout";
  command_id: string;
  status: "queued" | "ok" | "fail" | "rolled_back" | "dry" | "nothing";
  result?: FlowResult;
  updated_at: string;
}
export interface FlowRun {
  id: string;
  flow_id: string;
  flow_name: string;
  params: Record<string, string>;
  test_device_id: string;
  started_by?: string;
  created_at: string;
  devices: FlowRunDevice[];
}
export interface FlowRunSummary { id: string; flow_name: string; started_by?: string; created_at: string; devices: number; ok: number; failed: number; pending: number }
