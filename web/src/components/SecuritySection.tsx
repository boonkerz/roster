import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import type { Policy, PolicyFIMRule, PolicyLogSource, PolicyResponse, PolicySecurity, Script } from "../types";
import { useQuery } from "@tanstack/react-query";
import { CATEGORY_LABEL, SEVERITY_LABEL, ruleTitle, useComplianceRules } from "./Compliance";

// Sicherheitsüberwachung einer Richtlinie: Datei-Integrität (überwachte Pfade, Intervall)
// und Anmeldungs-Überwachung. Bei mehreren Richtlinien werden die Regeln vereinigt, die
// Schalter per ODER verknüpft und das kleinste Intervall genommen.

const PLATFORMS: [string, string][] = [["linux", "Linux"], ["windows", "Windows"], ["darwin", "macOS"]];

export const TRIGGER_LABEL: Record<string, string> = {
  brute_force: "Brute-Force", brute_force_success: "Anmeldung nach Brute-Force", new_source_ip: "Neue Quell-IP",
  account_change: "Kontoänderung", lockout: "Kontosperre", fim_change: "Datei-Änderung", scan: "Angriffsmuster",
};
export const ACTION_LABEL: Record<string, string> = { block_ip: "Quell-IP sperren", lock_user: "Konto sperren", run_script: "Skript ausführen" };
const IP_TRIGGERS = ["brute_force", "brute_force_success", "new_source_ip", "scan"];

// Log-Quellen: Formate und Vorlagen für typische Container/Dateien.
const LOG_FORMATS: [PolicyLogSource["format"], string][] = [
  ["nginx_access", "nginx-Zugriffsprotokoll"], ["nginx_error", "nginx-Fehlerprotokoll"], ["sshd", "sshd (SSH-/SFTP-Container)"],
  ["ftp", "FTP (vsftpd, proftpd, pure-ftpd)"], ["regex", "Eigenes Muster"],
];
type LogDraft = { name: string; type: PolicyLogSource["type"]; target: string; format: PolicyLogSource["format"]; pattern: string; kind: "login_failed" | "probe" };
const LOG_TEMPLATES: (LogDraft & { label: string })[] = [
  { label: "nginx-proxy-Container", name: "nginx-proxy", type: "docker", target: "nginx-proxy", format: "nginx_access", pattern: "", kind: "probe" },
  { label: "SFTP-Container (atmoz/sftp)", name: "sftp", type: "docker", target: "*sftp*", format: "sshd", pattern: "", kind: "login_failed" },
  { label: "nginx auf dem Host", name: "nginx", type: "file", target: "/var/log/nginx/access.log", format: "nginx_access", pattern: "", kind: "probe" },
  { label: "nginx error.log", name: "nginx", type: "file", target: "/var/log/nginx/error.log", format: "nginx_error", pattern: "", kind: "probe" },
];
const emptyLog = (): LogDraft => ({ name: "", type: "docker", target: "", format: "nginx_access", pattern: "", kind: "login_failed" });
const logDraftFrom = (l: PolicyLogSource): LogDraft => ({ name: l.name, type: l.type, target: l.target, format: l.format, pattern: l.pattern ?? "", kind: l.kind ?? "login_failed" });
const USER_TRIGGERS = ["brute_force_success", "account_change", "lockout", "new_source_ip"];

// Standardpfade je Plattform (nur Dateien, keine Registry).
const TEMPLATES: { path: string; recursive: boolean; exclude: string[]; platforms: string[] }[] = [
  { path: "/etc", recursive: true, exclude: ["/etc/mtab", "*.dpkg-new", "/etc/ld.so.cache", "/etc/resolv.conf", "/etc/adjtime"], platforms: ["linux"] },
  { path: "/usr/local/bin", recursive: true, exclude: [], platforms: ["linux", "darwin"] },
  { path: "/usr/local/sbin", recursive: true, exclude: [], platforms: ["linux"] },
  { path: "/root/.ssh", recursive: true, exclude: [], platforms: ["linux"] },
  { path: "/home/*/.ssh/authorized_keys", recursive: false, exclude: [], platforms: ["linux"] },
  { path: "/var/spool/cron", recursive: true, exclude: [], platforms: ["linux"] },
  { path: "/Library/LaunchDaemons", recursive: true, exclude: [], platforms: ["darwin"] },
  { path: "/Library/LaunchAgents", recursive: true, exclude: [], platforms: ["darwin"] },
  { path: "C:\\Windows\\System32\\drivers\\etc", recursive: true, exclude: [], platforms: ["windows"] },
  { path: "C:\\Windows\\System32\\Tasks", recursive: true, exclude: [], platforms: ["windows"] },
  { path: "C:\\ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\StartUp", recursive: true, exclude: [], platforms: ["windows"] },
];

type Draft = { path: string; recursive: boolean; exclude: string; platforms: string[]; alert: boolean };
const emptyDraft = (): Draft => ({ path: "", recursive: true, exclude: "", platforms: [], alert: true });
const draftFrom = (r: PolicyFIMRule): Draft => ({ path: r.path, recursive: r.recursive, exclude: (r.exclude ?? []).join("\n"), platforms: r.platforms ?? [], alert: r.alert });

export function SecuritySection({ policy }: { policy: Policy }) {
  const { t, lang } = useI18n();
  const qc = useQueryClient();
  const sec = policy.security;
  const rules = policy.fim_rules ?? [];
  const catalog = useComplianceRules();
  const [interval, setInterval_] = useState(60);
  const [auth, setAuth] = useState(false);
  const [compOn, setCompOn] = useState(false);
  const [compProfile, setCompProfile] = useState<"server" | "workstation">("server");
  const [compInterval, setCompInterval] = useState(24);
  const [compDisabled, setCompDisabled] = useState<string[]>([]);
  const [ruleFilter, setRuleFilter] = useState("");
  const [showRules, setShowRules] = useState(false);
  const invalidate = () => qc.invalidateQueries({ queryKey: ["policies"] });
  const { data: scripts } = useQuery({ queryKey: ["scripts"], queryFn: () => api.get<Script[]>("/scripts") });
  const responses = policy.responses ?? [];
  const [rTrigger, setRTrigger] = useState("brute_force");
  const [rAction, setRAction] = useState<PolicyResponse["action"]>("block_ip");
  const [rMinutes, setRMinutes] = useState(60);
  const [rScript, setRScript] = useState("");
  const [showResp, setShowResp] = useState(false);
  const saveResp = useMutation({
    mutationFn: () => api.post(`/policies/${policy.id}/responses`, { trigger: rTrigger, action: rAction, minutes: rMinutes, script_id: rScript, platforms: [] }),
    onSuccess: () => { setErr(""); setShowResp(false); invalidate(); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });
  const toggleResp = useMutation({
    mutationFn: (r: PolicyResponse) => api.put(`/responses/${r.id}`, { ...r, enabled: !r.enabled }),
    onSuccess: invalidate,
  });
  const delResp = useMutation({ mutationFn: (id: string) => api.del(`/responses/${id}`), onSuccess: invalidate });
  // Log-Quellen (nginx, SFTP-/FTP-Container, eigene Muster)
  const logSources = policy.log_sources ?? [];
  const [logDraft, setLogDraft] = useState<LogDraft>(emptyLog());
  const [logEditId, setLogEditId] = useState<string | null>(null);
  const [showLog, setShowLog] = useState(false);
  const saveLog = useMutation({
    mutationFn: (d: LogDraft) => logEditId ? api.put(`/log-sources/${logEditId}`, d) : api.post(`/policies/${policy.id}/log-sources`, d),
    onSuccess: () => { setErr(""); setLogDraft(emptyLog()); setLogEditId(null); setShowLog(false); invalidate(); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });
  const toggleLog = useMutation({ mutationFn: (l: PolicyLogSource) => api.put(`/log-sources/${l.id}`, { ...l, enabled: !l.enabled }), onSuccess: invalidate });
  const delLog = useMutation({ mutationFn: (id: string) => api.del(`/log-sources/${id}`), onSuccess: invalidate });
  const addLogTemplates = useMutation({
    mutationFn: async () => {
      const have = new Set(logSources.map((l) => `${l.type}|${l.target}|${l.format}`));
      for (const tpl of LOG_TEMPLATES.slice(0, 2)) {
        if (have.has(`${tpl.type}|${tpl.target}|${tpl.format}`)) continue;
        const { label: _l, ...body } = tpl;
        void _l;
        await api.post(`/policies/${policy.id}/log-sources`, body);
      }
    },
    onSuccess: invalidate,
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });
  const actionAllowed = (a: string, trig: string) => a === "run_script" || (a === "block_ip" && IP_TRIGGERS.includes(trig)) || (a === "lock_user" && USER_TRIGGERS.includes(trig));
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<Draft>(emptyDraft());
  const [editId, setEditId] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);

  useEffect(() => {
    setInterval_(sec?.fim_interval_min ?? 60);
    setAuth(sec?.auth_monitor ?? false);
    setCompOn(sec?.compliance_enabled ?? false);
    setCompProfile(sec?.compliance_profile ?? "server");
    setCompInterval(sec?.compliance_interval_h ?? 24);
    setCompDisabled(sec?.compliance_disabled ?? []);
    setErr("");
  }, [policy.id, sec?.fim_interval_min, sec?.auth_monitor, sec?.compliance_enabled, sec?.compliance_profile, sec?.compliance_interval_h, sec?.compliance_disabled]);

  const saveSec = useMutation({
    mutationFn: (patch: Partial<PolicySecurity>) => api.put(`/policies/${policy.id}/security`, {
      ...(sec ?? {}), fim_interval_min: interval, auth_monitor: auth,
      compliance_enabled: compOn, compliance_profile: compProfile, compliance_interval_h: compInterval, compliance_disabled: compDisabled, ...patch,
    }),
    onSuccess: () => { setErr(""); invalidate(); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });
  const saveRule = useMutation({
    mutationFn: (d: Draft) => {
      const body = { path: d.path, recursive: d.recursive, exclude: d.exclude.split("\n").map((x) => x.trim()).filter(Boolean), platforms: d.platforms, alert: d.alert };
      return editId ? api.put(`/fim/${editId}`, body) : api.post(`/policies/${policy.id}/fim`, body);
    },
    onSuccess: () => { setErr(""); setDraft(emptyDraft()); setEditId(null); setShowForm(false); invalidate(); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });
  const delRule = useMutation({ mutationFn: (id: string) => api.del(`/fim/${id}`), onSuccess: invalidate });
  const addTemplates = useMutation({
    mutationFn: async () => {
      const have = new Set(rules.map((r) => r.path));
      for (const tpl of TEMPLATES) {
        if (have.has(tpl.path)) continue;
        await api.post(`/policies/${policy.id}/fim`, { ...tpl, alert: true });
      }
    },
    onSuccess: invalidate,
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });

  const dirtySec = interval !== (sec?.fim_interval_min ?? 60) || auth !== (sec?.auth_monitor ?? false)
    || compOn !== (sec?.compliance_enabled ?? false) || compProfile !== (sec?.compliance_profile ?? "server")
    || compInterval !== (sec?.compliance_interval_h ?? 24) || compDisabled.join(",") !== (sec?.compliance_disabled ?? []).join(",");
  const toggleRule = (id: string) => setCompDisabled((d) => (d.includes(id) ? d.filter((x) => x !== id) : [...d, id]));
  const visibleRules = catalog.filter((r) => ((r.profiles ?? []).length === 0 || (r.profiles ?? []).includes(compProfile)) && (!ruleFilter || `${r.id} ${r.title} ${r.title_en} ${r.category}`.toLowerCase().includes(ruleFilter.toLowerCase())));
  const byCategory = visibleRules.reduce<Record<string, typeof visibleRules>>((acc, r) => { (acc[r.category] ??= []).push(r); return acc; }, {});
  const togglePlatform = (p: string) => setDraft((d) => ({ ...d, platforms: d.platforms.includes(p) ? d.platforms.filter((x) => x !== p) : [...d.platforms, p] }));

  return (
    <section className="card">
      <h2>{t("Sicherheitsüberwachung")}</h2>
      <p className="muted small">
        {t("Datei-Integrität: Der Agent merkt sich Hash, Rechte und Besitzer der überwachten Pfade und meldet jede Abweichung. Anmeldungen: fehlgeschlagene/erfolgreiche Logins, sudo und Kontoänderungen werden gesammelt und auf Brute-Force oder neue Quell-IPs geprüft. Bei mehreren Richtlinien werden Regeln vereinigt.")}
      </p>

      <h3 className="muted small" style={{ marginTop: 14 }}>{t("Datei-Integrität (FIM)")} <span className="tab-count">{rules.length}</span></h3>
      {rules.length === 0 && <p className="muted small">{t("Noch keine überwachten Pfade.")}</p>}
      {rules.length > 0 && (
        <table className="table">
          <thead><tr><th>{t("Pfad")}</th><th>{t("Rekursiv")}</th><th>{t("Ausnahmen")}</th><th>{t("Plattform")}</th><th>{t("Alarm")}</th><th></th></tr></thead>
          <tbody>
            {rules.map((r) => (
              <tr key={r.id} className="small">
                <td className="mono">{r.path}</td>
                <td>{r.recursive ? t("ja") : t("nein")}</td>
                <td className="muted mono">{(r.exclude ?? []).join(", ") || "—"}</td>
                <td className="muted">{(r.platforms ?? []).length ? r.platforms.join(", ") : t("alle")}</td>
                <td>{r.alert ? "🔔" : <span className="muted">—</span>}</td>
                <td style={{ textAlign: "right", whiteSpace: "nowrap" }}>
                  <button className="btn ghost sm" onClick={() => { setEditId(r.id); setDraft(draftFrom(r)); setShowForm(true); }}>{t("Bearbeiten")}</button>
                  <button className="btn ghost sm" onClick={() => { if (confirm(t("Regel löschen?"))) delRule.mutate(r.id); }}>✕</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="inline-form" style={{ marginTop: 8 }}>
        {!showForm && <button className="btn ghost sm" onClick={() => { setEditId(null); setDraft(emptyDraft()); setShowForm(true); }}>+ {t("Pfad überwachen")}</button>}
        <button className="btn ghost sm" disabled={addTemplates.isPending} onClick={() => addTemplates.mutate()} title={t("Typische Systempfade je Plattform (/etc, /usr/local/bin, ~/.ssh, cron; Windows hosts/Tasks/Autostart; macOS LaunchDaemons)")}>{t("Standardpfade einfügen")}</button>
        <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>{t("Prüfabstand (Min.)")}
          <input type="number" min={5} max={1440} value={interval} onChange={(e) => setInterval_(Number(e.target.value))} style={{ width: 80 }} />
        </label>
      </div>
      {showForm && (
        <form className="form-col" style={{ marginTop: 10, maxWidth: 640 }} onSubmit={(e) => { e.preventDefault(); saveRule.mutate(draft); }}>
          <label className="field"><span>{t("Pfad (Datei, Verzeichnis oder Glob, z. B. /home/*/.ssh/authorized_keys)")}</span>
            <input className="mono" value={draft.path} onChange={(e) => setDraft({ ...draft, path: e.target.value })} placeholder="/etc" required />
          </label>
          <div className="inline-form">
            <label className="chip"><input type="checkbox" checked={draft.recursive} onChange={(e) => setDraft({ ...draft, recursive: e.target.checked })} /> {t("Unterverzeichnisse einschließen")}</label>
            <label className="chip"><input type="checkbox" checked={draft.alert} onChange={(e) => setDraft({ ...draft, alert: e.target.checked })} /> {t("Änderungen alarmieren")}</label>
            {PLATFORMS.map(([k, label]) => (
              <label key={k} className={`chip ${draft.platforms.includes(k) ? "chip-on" : ""}`}><input type="checkbox" checked={draft.platforms.includes(k)} onChange={() => togglePlatform(k)} /> {label}</label>
            ))}
            <span className="muted small">{t("keine Plattform = alle")}</span>
          </div>
          <label className="field"><span>{t("Ausnahmen (ein Glob-Muster je Zeile: *.log, cache/, /var/lib/tmp)")}</span>
            <textarea className="notes-input" rows={2} value={draft.exclude} onChange={(e) => setDraft({ ...draft, exclude: e.target.value })} />
          </label>
          <div className="inline-form">
            <button className="btn primary sm" type="submit" disabled={saveRule.isPending}>{editId ? t("Speichern") : t("Hinzufügen")}</button>
            <button className="btn ghost sm" type="button" onClick={() => { setShowForm(false); setEditId(null); setDraft(emptyDraft()); }}>{t("Abbrechen")}</button>
          </div>
        </form>
      )}

      <h3 className="muted small" style={{ marginTop: 18 }}>{t("Anmeldungen")}</h3>
      <label className="chip" style={{ display: "inline-flex", whiteSpace: "nowrap" }}>
        <input type="checkbox" checked={auth} onChange={(e) => setAuth(e.target.checked)} /> {t("Anmeldungen überwachen (SSH/RDP/Konsole, sudo, Kontoänderungen)")}
      </label>
      <p className="muted small">{t("Linux liest journald bzw. /var/log/auth.log, Windows das Sicherheitsprotokoll (Anmeldeüberwachung muss aktiv sein). Schwellwerte für Brute-Force stehen in den Einstellungen.")}</p>

      <h3 className="muted small" style={{ marginTop: 18 }}>{t("Log-Quellen (nginx, SFTP-/FTP-Container, eigene Muster)")} <span className="tab-count">{logSources.length}</span></h3>
      <p className="muted small">{t("Zusätzliche Protokolle, die der Agent jede Minute inkrementell liest: Dateien (Glob erlaubt) oder die Ausgabe von Docker-Containern (Name oder Muster wie *sftp*). 401-Antworten und fehlgeschlagene Logins zählen als Fehlversuche, Scanner-Pfade (wp-login.php, /.env, phpmyadmin …) und kaputte Anfragen als verdächtige Anfragen → Vorfall „Angriffsmuster“. Beides kann die IP automatisch sperren.")}</p>
      {logSources.length > 0 && (
        <table className="table">
          <thead><tr><th>{t("Dienst")}</th><th>{t("Quelle")}</th><th>{t("Format")}</th><th>{t("Aktiv")}</th><th></th></tr></thead>
          <tbody>
            {logSources.map((l) => (
              <tr key={l.id} className="small">
                <td>{l.name}</td>
                <td className="mono">{l.type === "docker" ? "🐳 " : ""}{l.target}</td>
                <td className="muted">{t(LOG_FORMATS.find(([k]) => k === l.format)?.[1] ?? l.format)}{l.format === "regex" && <span className="mono"> {l.pattern}</span>}</td>
                <td><input type="checkbox" checked={l.enabled} onChange={() => toggleLog.mutate(l)} /></td>
                <td style={{ textAlign: "right", whiteSpace: "nowrap" }}>
                  <button className="btn ghost sm" onClick={() => { setLogEditId(l.id); setLogDraft(logDraftFrom(l)); setShowLog(true); }}>{t("Bearbeiten")}</button>
                  <button className="btn ghost sm" onClick={() => { if (confirm(t("Regel löschen?"))) delLog.mutate(l.id); }}>✕</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="inline-form" style={{ marginTop: 8 }}>
        {!showLog && <button className="btn ghost sm" onClick={() => { setLogEditId(null); setLogDraft(emptyLog()); setShowLog(true); }}>+ {t("Log-Quelle hinzufügen")}</button>}
        <button className="btn ghost sm" disabled={addLogTemplates.isPending} onClick={() => addLogTemplates.mutate()} title={t("Container „nginx-proxy“ (Zugriffsprotokoll) und „*sftp*“ (sshd-Ausgabe) überwachen")}>{t("nginx-proxy + SFTP-Container einfügen")}</button>
      </div>
      {showLog && (
        <form className="form-col" style={{ marginTop: 10, maxWidth: 720 }} onSubmit={(e) => { e.preventDefault(); saveLog.mutate(logDraft); }}>
          <div className="inline-form">
            <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>{t("Vorlage")}
              <select value="" onChange={(e) => { const tpl = LOG_TEMPLATES[Number(e.target.value)]; if (tpl) { const { label: _l, ...d } = tpl; void _l; setLogDraft(d); } }}>
                <option value="">—</option>
                {LOG_TEMPLATES.map((tpl, i) => <option key={tpl.label} value={i}>{t(tpl.label)}</option>)}
              </select>
            </label>
            <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>{t("Dienstname")}
              <input value={logDraft.name} onChange={(e) => setLogDraft({ ...logDraft, name: e.target.value })} placeholder="nginx-proxy" required style={{ width: 140 }} />
            </label>
            <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>{t("Typ")}
              <select value={logDraft.type} onChange={(e) => setLogDraft({ ...logDraft, type: e.target.value as LogDraft["type"] })}>
                <option value="docker">{t("Docker-Container")}</option>
                <option value="file">{t("Datei")}</option>
              </select>
            </label>
          </div>
          <label className="field"><span>{logDraft.type === "docker" ? t("Container-Name oder Muster (z. B. nginx-proxy, *sftp*)") : t("Dateipfad (Glob erlaubt, z. B. /var/log/nginx/*access.log)")}</span>
            <input className="mono" value={logDraft.target} onChange={(e) => setLogDraft({ ...logDraft, target: e.target.value })} required />
          </label>
          <div className="inline-form">
            <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>{t("Format")}
              <select value={logDraft.format} onChange={(e) => setLogDraft({ ...logDraft, format: e.target.value as LogDraft["format"] })}>
                {LOG_FORMATS.map(([k, l]) => <option key={k} value={k}>{t(l)}</option>)}
              </select>
            </label>
            {logDraft.format === "regex" && (
              <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>{t("Treffer zählt als")}
                <select value={logDraft.kind} onChange={(e) => setLogDraft({ ...logDraft, kind: e.target.value as LogDraft["kind"] })}>
                  <option value="login_failed">{t("Anmeldung fehlgeschlagen")}</option>
                  <option value="probe">{t("verdächtige Anfrage")}</option>
                </select>
              </label>
            )}
          </div>
          {logDraft.format === "regex" && (
            <label className="field"><span>{t("Regulärer Ausdruck mit benannten Gruppen (?P<ip>…) und optional (?P<user>…)")}</span>
              <input className="mono" value={logDraft.pattern} onChange={(e) => setLogDraft({ ...logDraft, pattern: e.target.value })} placeholder={"login failed for (?P<user>\\S+) from (?P<ip>[0-9.]+)"} required />
            </label>
          )}
          <div className="inline-form">
            <button className="btn primary sm" type="submit" disabled={saveLog.isPending}>{logEditId ? t("Speichern") : t("Hinzufügen")}</button>
            <button className="btn ghost sm" type="button" onClick={() => { setShowLog(false); setLogEditId(null); setLogDraft(emptyLog()); }}>{t("Abbrechen")}</button>
          </div>
        </form>
      )}

      <h3 className="muted small" style={{ marginTop: 18 }}>{t("Compliance (Baseline-Regeln)")}</h3>
      <div className="inline-form">
        <label className="chip" style={{ display: "inline-flex", whiteSpace: "nowrap" }}><input type="checkbox" checked={compOn} onChange={(e) => setCompOn(e.target.checked)} /> {t("Compliance-Prüfung aktivieren")}</label>
        <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>{t("Profil")}
          <select value={compProfile} onChange={(e) => setCompProfile(e.target.value as "server" | "workstation")} disabled={!compOn}>
            <option value="server">{t("Server")}</option>
            <option value="workstation">{t("Arbeitsplatz")}</option>
          </select>
        </label>
        <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>{t("Prüfabstand (Std.)")}
          <input type="number" min={1} max={168} value={compInterval} onChange={(e) => setCompInterval(Number(e.target.value))} disabled={!compOn} style={{ width: 70 }} />
        </label>
        <button className="btn ghost sm" type="button" onClick={() => setShowRules((v) => !v)} disabled={!compOn}>
          {showRules ? t("Regeln ausblenden") : t("Regeln auswählen")} ({catalog.length - compDisabled.length}/{catalog.length})
        </button>
      </div>
      <p className="muted small">{t("Der Agent prüft CIS-artige Regeln (SSH, Dateirechte, Firewall, sysctl; Windows: UAC, SMBv1, RDP-NLA, Firewall, Defender, LSA; macOS: FileVault, Firewall, Gatekeeper, SIP). Der Check-Typ „Compliance-Score“ alarmiert bei Unterschreitung.")}</p>
      {showRules && compOn && (
        <div style={{ marginTop: 8 }}>
          <input className="search" placeholder={t("Regel suchen…")} value={ruleFilter} onChange={(e) => setRuleFilter(e.target.value)} />
          {Object.entries(byCategory).map(([cat, rs]) => (
            <div key={cat} style={{ marginTop: 8 }}>
              <div className="muted small"><strong>{t(CATEGORY_LABEL[cat] ?? cat)}</strong></div>
              <div className="chips">
                {rs.map((r) => (
                  <label key={r.id} className={`chip ${compDisabled.includes(r.id) ? "" : "chip-on"}`} title={`${r.description}\n${r.platforms.join(", ")} · ${t(SEVERITY_LABEL[r.severity])}`}>
                    <input type="checkbox" checked={!compDisabled.includes(r.id)} onChange={() => toggleRule(r.id)} /> {ruleTitle(r, lang, r.id)} <span className="muted small">({(r.platforms ?? []).map((p) => p[0].toUpperCase()).join("")})</span>
                  </label>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      <h3 className="muted small" style={{ marginTop: 18 }}>{t("Automatische Reaktion (Active Response)")} <span className="tab-count">{responses.length}</span></h3>
      <p className="muted small">{t("Reagiert sofort auf Vorfälle: Quell-IP sperren (nftables/iptables, Windows-Firewall, pf), Konto sperren oder ein Skript mit ROSTER_TRIGGER/ROSTER_SOURCE_IP/ROSTER_USER ausführen. Ohne eigene Regel greift die globale automatische Sperre aus Einstellungen → Sicherheitsüberwachung (dort auch Nie-sperren-Liste, Cooldown, Flotten- und Netzbereichs-Sperre). root/Administrator werden nie gesperrt.")}</p>
      {responses.length > 0 && (
        <table className="table">
          <thead><tr><th>{t("Auslöser")}</th><th>{t("Aktion")}</th><th>{t("Details")}</th><th>{t("Aktiv")}</th><th></th></tr></thead>
          <tbody>
            {responses.map((r) => (
              <tr key={r.id} className="small">
                <td>{t(TRIGGER_LABEL[r.trigger] ?? r.trigger)}</td>
                <td>{t(ACTION_LABEL[r.action] ?? r.action)}</td>
                <td className="muted">{r.action === "block_ip" ? (r.minutes > 0 ? t("{n} Minuten", { n: r.minutes }) : t("dauerhaft")) : r.action === "run_script" ? (scripts ?? []).find((s) => s.id === r.script_id)?.name ?? r.script_id : "—"}</td>
                <td><input type="checkbox" checked={r.enabled} onChange={() => toggleResp.mutate(r)} /></td>
                <td style={{ textAlign: "right" }}><button className="btn ghost sm" onClick={() => { if (confirm(t("Regel löschen?"))) delResp.mutate(r.id); }}>✕</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {!showResp && <button className="btn ghost sm" onClick={() => setShowResp(true)}>+ {t("Reaktion hinzufügen")}</button>}
      {showResp && (
        <div className="inline-form" style={{ marginTop: 8 }}>
          <select value={rTrigger} onChange={(e) => { setRTrigger(e.target.value); if (!actionAllowed(rAction, e.target.value)) setRAction("run_script"); }}>
            {Object.entries(TRIGGER_LABEL).map(([k, l]) => <option key={k} value={k}>{t(l)}</option>)}
          </select>
          <span className="muted">→</span>
          <select value={rAction} onChange={(e) => setRAction(e.target.value as PolicyResponse["action"])}>
            {Object.entries(ACTION_LABEL).filter(([k]) => actionAllowed(k, rTrigger)).map(([k, l]) => <option key={k} value={k}>{t(l)}</option>)}
          </select>
          {rAction === "block_ip" && <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>{t("Minuten (0 = dauerhaft)")}<input type="number" min={0} max={43200} value={rMinutes} onChange={(e) => setRMinutes(Number(e.target.value))} style={{ width: 80 }} /></label>}
          {rAction === "run_script" && (
            <select value={rScript} onChange={(e) => setRScript(e.target.value)}>
              <option value="">{t("— Skript wählen —")}</option>
              {(scripts ?? []).filter((s) => !s.check_only).map((s) => <option key={s.id} value={s.id}>{s.name} ({s.shell})</option>)}
            </select>
          )}
          <button className="btn primary sm" disabled={saveResp.isPending || (rAction === "run_script" && !rScript)} onClick={() => saveResp.mutate()}>{t("Hinzufügen")}</button>
          <button className="btn ghost sm" onClick={() => setShowResp(false)}>{t("Abbrechen")}</button>
        </div>
      )}

      {err && <p className="error small">{err}</p>}
      {dirtySec && (
        <div className="inline-form">
          <button className="btn primary sm" disabled={saveSec.isPending} onClick={() => saveSec.mutate({})}>{t("Speichern")}</button>
        </div>
      )}
    </section>
  );
}
