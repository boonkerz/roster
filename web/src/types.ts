// Typen spiegeln die JSON-Antworten der Go-API wider.

export type Role = "admin" | "technician" | "viewer";

export interface User {
  id: string;
  username: string;
  email: string;
  role: Role;
  auth_source: "local" | "ldap";
  theme: "light" | "dark" | "";
  created_at: string;
  last_login?: string;
  totp_enabled?: boolean;
  require_2fa?: boolean;
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

export interface PolicyCheck {
  id: string;
  policy_id: string;
  name: string;
  type: string;
  config: Record<string, number | string>;
  script_id?: string;
  remediation_script_id?: string | null;
  severity?: "warning" | "critical";
  frequency?: string;
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
  assignments?: Assignment[];
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
  tasks_total?: number;
  tasks_failing?: number;
  vuln_count?: number;
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
