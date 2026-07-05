import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import type { Device, Group, ClientTree, Script, CheckEvent, TaskResult, SoftwareEvent } from "../types";
import { StatusBadge, UpdatesBadge, HealthBadge, TaskHealthBadge, CheckStatusBadge, SeverityBadge, relTime } from "./StatusBadge";
import { DeviceTerminal } from "./DeviceTerminal";
import { DeviceRemote } from "./DeviceRemote";
import { CustomFieldsEditor } from "./CustomFieldsEditor";
import { TreeSizePanel } from "./TreeSizePanel";
import { ServicesProcesses } from "./ServicesProcesses";
import { FileBrowser } from "./FileBrowser";
import { SecurityPanel } from "./SecurityPanel";
import { EventLog } from "./EventLog";
import { CopyText } from "./CopyText";
import { LiveMetrics } from "./LiveMetrics";
import { MetricsHistory } from "./MetricsHistory";
import { Vulnerabilities } from "./Vulnerabilities";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

type Tab = "summary" | "live" | "checks" | "tasks" | "history" | "storage" | "system" | "security" | "vulns" | "events" | "files" | "software" | "updates" | "network" | "run" | "terminal" | "remote" | "fields";

// fmtSize formatiert Bytes als TB/GB/MB.
function fmtSize(n: number): string {
  if (!n) return "—";
  const tb = n / 1024 ** 4, gb = n / 1024 ** 3, mb = n / 1024 ** 2;
  if (tb >= 1) return `${tb.toFixed(1)} TB`;
  if (gb >= 1) return `${gb.toFixed(0)} GB`;
  return `${mb.toFixed(0)} MB`;
}

// DevicePanel zeigt die Details eines Geräts mit Tabs – einsetzbar im unteren Panel
// der Geräteliste und als eigene Seite.
export function DevicePanel({ id, focusTab, focusKey }: { id: string; focusTab?: string; focusKey?: number }) {
  const qc = useQueryClient();
  const nav = useNavigate();
  const { user } = useAuth();
  const { t } = useI18n();
  const isAdmin = user?.role === "admin";
  const canOperate = user?.role === "admin" || user?.role === "technician";
  const [tab, setTab] = useState<Tab>("summary");
  // Direktsprung in einen Tab (z.B. Klick auf Checks/Tasks in der Geräteliste).
  useEffect(() => {
    if (focusTab) setTab(focusTab as Tab);
  }, [focusKey]); // eslint-disable-line react-hooks/exhaustive-deps
  const [swFilter, setSwFilter] = useState("");
  const [runScriptId, setRunScriptId] = useState("");
  const [aptMode, setAptMode] = useState<"full" | "safe">("full");
  const [notes, setNotes] = useState<string | null>(null); // null = noch nicht bearbeitet

  const { data: device } = useQuery({ queryKey: ["device", id], queryFn: () => api.get<Device>(`/devices/${id}`), refetchInterval: 15000 });
  const { data: groups } = useQuery({ queryKey: ["groups"], queryFn: () => api.get<Group[]>("/groups") });
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: scripts } = useQuery({ queryKey: ["scripts"], queryFn: () => api.get<Script[]>("/scripts"), enabled: canOperate });
  const { data: events } = useQuery({
    queryKey: ["device-events", id],
    queryFn: () => api.get<CheckEvent[]>(`/devices/${id}/events`),
    enabled: tab === "history",
    refetchInterval: 15000,
  });
  const { data: taskRuns } = useQuery({
    queryKey: ["device-task-runs", id],
    queryFn: () => api.get<TaskResult[]>(`/devices/${id}/task-runs`),
    enabled: tab === "history",
    refetchInterval: 15000,
  });
  const { data: swEvents } = useQuery({
    queryKey: ["device-sw-events", id],
    queryFn: () => api.get<SoftwareEvent[]>(`/devices/${id}/software-events`),
    enabled: tab === "history",
    refetchInterval: 15000,
  });

  const invalidate = () => qc.invalidateQueries({ queryKey: ["device", id] });
  const runScript = useMutation({ mutationFn: () => api.post(`/devices/${id}/run`, { script_id: runScriptId }), onSuccess: invalidate });
  const scanUpdates = useMutation({ mutationFn: () => api.post(`/devices/${id}/scan-updates`), onSuccess: invalidate });
  const installUpdates = useMutation({ mutationFn: (v: { approved: boolean; apt_mode?: string }) => api.post(`/devices/${id}/install-updates`, v), onSuccess: invalidate });
  const approvePatch = useMutation({ mutationFn: (v: { name: string; approved: boolean }) => api.put(`/devices/${id}/patches/approve`, v), onSuccess: invalidate });
  const reboot = useMutation({ mutationFn: () => api.post(`/devices/${id}/reboot`), onSuccess: invalidate });
  const saveNotes = useMutation({
    mutationFn: (v: string) => api.put(`/devices/${id}/notes`, { notes: v }),
    onSuccess: () => { setNotes(null); invalidate(); },
  });
  const wake = useMutation({
    mutationFn: () => api.post<{ mac: string }>(`/devices/${id}/wake`),
    onSuccess: (d) => alert(t("Aufweck-Signal (Wake-on-LAN) an {mac} gesendet.", { mac: d.mac })),
    onError: (e) => alert((e as Error).message),
  });
  const setGroups = useMutation({ mutationFn: (g: string[]) => api.put(`/devices/${id}/groups`, { group_ids: g }), onSuccess: invalidate });
  const setSite = useMutation({
    mutationFn: (siteID: string | null) => api.put(`/devices/${id}/site`, { site_id: siteID }),
    onSuccess: () => { invalidate(); qc.invalidateQueries({ queryKey: ["clients"] }); qc.invalidateQueries({ queryKey: ["devices"] }); },
  });
  const revoke = useMutation({ mutationFn: () => api.post(`/devices/${id}/revoke`), onSuccess: invalidate });
  const remove = useMutation({
    mutationFn: () => api.del(`/devices/${id}`),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["devices"] }); nav("/devices"); },
  });

  if (!device) return <div className="muted" style={{ padding: 20 }}>{t("Lädt…")}</div>;

  const memberIDs = new Set((device.groups ?? []).map((g) => g.id));
  const toggleGroup = (gid: string) => {
    const next = new Set(memberIDs);
    next.has(gid) ? next.delete(gid) : next.add(gid);
    setGroups.mutate([...next]);
  };

  // Tabs in Gruppen; jeder Tab mit Icon. run/terminal/files nur für Bediener.
  const TAB_META: Record<Tab, { label: string; icon: string }> = {
    summary: { label: "Übersicht", icon: "🖥" },
    live: { label: "Auslastung", icon: "📈" },
    checks: { label: "Checks", icon: "✓" },
    tasks: { label: "Tasks", icon: "🗓" },
    history: { label: "Verlauf", icon: "🕑" },
    storage: { label: "Speicher", icon: "💾" },
    system: { label: "Dienste/Prozesse", icon: "⚙" },
    security: { label: "Sicherheit", icon: "🛡" },
    vulns: { label: "Schwachstellen", icon: "🐞" },
    events: { label: "Ereignisse", icon: "📃" },
    software: { label: "Software", icon: "📦" },
    updates: { label: "Patches", icon: "⬇" },
    network: { label: "Netzwerk", icon: "🌐" },
    fields: { label: "Felder", icon: "🏷" },
    files: { label: "Dateien", icon: "📁" },
    run: { label: "Ausführen", icon: "▶" },
    terminal: { label: "Terminal", icon: "❯_" },
    remote: { label: "Fernsteuern", icon: "🖱" },
  };
  const tabGroups: { name: string; icon: string; tabs: Tab[] }[] = [
    { name: "Übersicht", icon: "🖥", tabs: ["summary", "live"] },
    { name: "Zustand", icon: "✓", tabs: ["checks", "tasks", "history"] },
    { name: "Inventar", icon: "📦", tabs: ["software", "updates", "storage", "network", "fields"] },
    { name: "System", icon: "⚙", tabs: ["system", "security", "vulns", "events"] },
  ];
  if (canOperate) tabGroups.push({ name: "Zugriff", icon: "❯_", tabs: ["files", "run", "terminal", "remote"] });

  const activeGroup = tabGroups.find((g) => g.tabs.includes(tab)) ?? tabGroups[0];
  const tabBadge = (k: Tab) => (
    <>
      {k === "checks" && (device.checks_total ?? 0) > 0 && <span className="tab-badge"><HealthBadge total={device.checks_total} failing={device.checks_failing} /></span>}
      {k === "tasks" && (device.tasks_total ?? 0) > 0 && <span className="tab-badge"><TaskHealthBadge total={device.tasks_total} failing={device.tasks_failing} /></span>}
      {k === "updates" && <span className="tab-badge"><UpdatesBadge count={device.updates_count} /></span>}
      {k === "software" && <span className="tab-count">{(device.software ?? []).length}</span>}
    </>
  );
  // Fehler-/Warn-Indikator an der Gruppe „Zustand" / „Inventar".
  const groupAlert = (g: { tabs: Tab[] }) =>
    (g.tabs.includes("checks") && (device.checks_failing ?? 0) > 0) ||
    (g.tabs.includes("tasks") && (device.tasks_failing ?? 0) > 0) ||
    (g.tabs.includes("updates") && (device.updates_count ?? 0) > 0);

  return (
    <div className="device-panel">
      <div className="panel-head">
        <div className="panel-title">
          <strong>{device.hostname || "(unbenannt)"}</strong>
          <StatusBadge status={device.status} />
          <span className="muted">{device.os} {device.os_version} · Agent {device.agent_version || "—"}</span>
          {device.logged_in_users && device.logged_in_users.length > 0 && (
            <span className="muted">· {device.logged_in_users.join(", ")}</span>
          )}
          <span className="muted">· {t("zuletzt")} {relTime(device.last_seen)}</span>
        </div>
        <div className="actions">
          <button className="btn ghost sm" onClick={invalidate} title={t("Aktualisieren")}>↻</button>
          {canOperate && device.status !== "online" && (
            <button className="btn ghost sm" disabled={wake.isPending}
              onClick={() => wake.mutate()} title={t("Wake-on-LAN über einen Nachbarn im selben Standort")}>
              {t("Aufwecken")}
            </button>
          )}
          {canOperate && (
            <button className="btn ghost sm" disabled={reboot.isPending}
              onClick={() => confirm(t("„{host}\" jetzt neu starten?", { host: device.hostname })) && reboot.mutate()}>
              {t("Neustart")}
            </button>
          )}
          {isAdmin && !device.revoked && <button className="btn ghost sm" onClick={() => revoke.mutate()}>{t("Token widerrufen")}</button>}
          {isAdmin && <button className="btn danger sm" onClick={() => confirm(t("Gerät wirklich löschen?")) && remove.mutate()}>{t("Löschen")}</button>}
        </div>
      </div>

      <div className="tab-groups">
        {tabGroups.map((g) => (
          <button key={g.name}
            className={`tab-group ${activeGroup.name === g.name ? "tab-group-on" : ""}`}
            onClick={() => { if (!g.tabs.includes(tab)) setTab(g.tabs[0]); }}>
            <span className="tab-icon">{g.icon}</span> {t(g.name)}
            {groupAlert(g) && activeGroup.name !== g.name && <span className="tab-group-dot" />}
          </button>
        ))}
      </div>
      <div className="tabs">
        {activeGroup.tabs.map((k) => (
          <button key={k} className={`tab ${tab === k ? "tab-on" : ""}`} onClick={() => setTab(k)}>
            <span className="tab-icon">{TAB_META[k].icon}</span> {t(TAB_META[k].label)}
            {tabBadge(k)}
          </button>
        ))}
      </div>

      <div className="panel-body">
        {tab === "summary" && (<>
          <div className="grid-2">
            <section className="card">
              <h2>{t("Hardware")}</h2>
              <dl className="kv">
                <dt>{t("Hersteller")}</dt><dd>{device.vendor || "—"}</dd>
                <dt>{t("Modell")}</dt><dd>{device.model || "—"}</dd>
                <dt>{t("Seriennummer")}</dt><dd className="mono">{device.serial || "—"}</dd>
                <dt>{t("Betriebssystem")}</dt><dd>{device.os} {device.os_version}</dd>
                <dt>CPU</dt>
                <dd>
                  {(device.cpu_sockets ?? 0) > 1 ? `${device.cpu_sockets}× ` : ""}{device.cpu_model || "—"}
                  {device.cpu_cores ? ` (${device.cpu_cores}C/${device.cpu_threads || device.cpu_cores}T)` : ""}
                </dd>
                <dt>{t("Arbeitsspeicher")}</dt><dd>{fmtSize(device.memory_bytes)}</dd>
                {(device.gpus ?? []).length > 0 && (<><dt>{t("Grafik")}</dt><dd>{device.gpus!.join(", ")}</dd></>)}
                {(device.physical_disks ?? []).length > 0 && (
                  <>
                    <dt>{t("Festplatten")}</dt>
                    <dd>{device.physical_disks!.map((d, i) => <div key={i}>{d.model} ({fmtSize(d.size_bytes)})</div>)}</dd>
                  </>
                )}
                <dt>{t("Öffentliche IP")}</dt><dd className="mono">{device.public_ip || "—"}</dd>
                <dt>{t("Agent-Version")}</dt><dd className="mono">{device.agent_version || "—"}</dd>
                <dt>{t("Erstmals gesehen")}</dt><dd>{new Date(device.first_seen).toLocaleString()}</dd>
              </dl>
            </section>
            <section className="card">
              <h2>{t("Standort")}</h2>
              <select style={{ width: "100%" }} value={device.site_id ?? ""} disabled={!isAdmin} onChange={(e) => setSite.mutate(e.target.value || null)}>
                <option value="">{t("— nicht zugeordnet —")}</option>
                {(tree?.clients ?? []).map((c) => (
                  <optgroup key={c.id} label={c.name}>
                    {(c.sites ?? []).map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
                  </optgroup>
                ))}
              </select>
              <h2 style={{ marginTop: 18 }}>{t("Tags")}</h2>
              {(groups ?? []).length === 0 && <p className="muted">{t("Noch keine Tags angelegt (unter „Tags“).")}</p>}
              <div className="chips">
                {(groups ?? []).map((g) => (
                  <label key={g.id} className={`chip ${memberIDs.has(g.id) ? "chip-on" : ""}`}>
                    <input type="checkbox" checked={memberIDs.has(g.id)} onChange={() => toggleGroup(g.id)} /> {g.name}
                  </label>
                ))}
              </div>
              {(device.printers ?? []).length > 0 && (
                <>
                  <h2 style={{ marginTop: 18 }}>{t("Drucker")}</h2>
                  {device.printers!.map((p, i) => <div key={i} className="muted">{p.name}{p.default ? " (Standard)" : ""}</div>)}
                </>
              )}
              <h2 style={{ marginTop: 18 }}>{t("Notizen")}</h2>
              <textarea
                className="notes-input"
                placeholder={canOperate ? t("Freitext-Doku zu diesem Gerät…") : t("Keine Notizen.")}
                value={notes ?? device.notes ?? ""}
                readOnly={!canOperate}
                onChange={(e) => setNotes(e.target.value)}
              />
              {canOperate && notes !== null && notes !== (device.notes ?? "") && (
                <div className="inline-form" style={{ marginTop: 6 }}>
                  <button className="btn primary sm" disabled={saveNotes.isPending} onClick={() => saveNotes.mutate(notes)}>{t("Speichern")}</button>
                  <button className="btn ghost sm" onClick={() => setNotes(null)}>{t("Verwerfen")}</button>
                </div>
              )}
            </section>
          </div>
          {(device.disks ?? []).length > 0 && (
            <section className="card">
              <h2>{t("Datenträger")}</h2>
              <div className="disks">
                {device.disks!.map((d, i) => (
                  <div key={i} className="disk-row">
                    <div className="disk-label"><strong>{d.name}</strong> <span className="muted">{d.fs_type}</span></div>
                    <div className="disk-bar"><span className={`disk-fill ${d.used_percent > 90 ? "warn" : ""}`} style={{ width: `${Math.min(100, d.used_percent)}%` }} /></div>
                    <div className="muted small">{fmtSize(d.free_bytes)} {t("frei von")} {fmtSize(d.size_bytes)}</div>
                  </div>
                ))}
              </div>
            </section>
          )}
        </>)}

        {tab === "checks" && (
          <section className="card">
            {(device.check_results ?? []).length === 0 ? (
              (device.assigned_checks ?? 0) > 0
                ? <p className="muted">Wird ausgewertet – warte auf den Agent ({device.assigned_checks} Check(s) zugewiesen).</p>
                : <p className="muted">{t("Keine Checks zugewiesen (über Richtlinien).")}</p>
            ) : (
              <table className="table">
                <thead><tr><th>{t("Status")}</th><th>{t("Check")}</th><th>{t("Ergebnis")}</th><th>{t("Geprüft")}</th></tr></thead>
                <tbody>
                  {device.check_results!.map((c) => (
                    <tr key={c.check_id}>
                      <td><CheckStatusBadge status={c.status} /></td>
                      <td>{c.name || c.type}</td>
                      <td className="muted">{c.output}</td>
                      <td className="muted">{relTime(c.updated_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </section>
        )}

        {tab === "tasks" && (
          <section className="card">
            <h3 className="muted small">{t("Letzter Lauf je Task")} <span className="muted" style={{ fontWeight: 400 }}>· {t("vollständige Historie unter „Verlauf“")}</span></h3>
            {(device.task_results ?? []).length === 0 ? (
              <p className="muted">{(device.assigned_tasks ?? 0) > 0
                ? `Noch keine Läufe (${device.assigned_tasks} Task(s) zugewiesen).`
                : "Keine Tasks zugewiesen (über Richtlinien)."}</p>
            ) : (
              <div className="scroll-list">
                <table className="table">
                  <thead><tr><th>{t("Status")}</th><th>{t("Task")}</th><th>{t("Ergebnis")}</th><th>{t("Gelaufen")}</th></tr></thead>
                  <tbody>
                    {device.task_results!.map((tk) => (
                      <tr key={tk.id}>
                        <td><span className={`badge ${tk.exit_code === 0 ? "badge-online" : "badge-offline"}`}><span className="dot" /> {tk.exit_code === 0 ? "OK" : t("Fehler")}</span></td>
                        <td>{tk.name || tk.task_id}</td>
                        <td className="muted mono small">{(tk.output || "").slice(0, 160) || `Exit ${tk.exit_code}`}</td>
                        <td className="muted">{relTime(tk.ran_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

        {tab === "history" && (<>
          <section className="card">
            <h3 className="muted small">{t("Status-Wechsel (Checks)")}</h3>
            {(events ?? []).length === 0 ? (
              <p className="muted">{t("Noch keine Statuswechsel protokolliert.")}</p>
            ) : (
              <div className="scroll-list">
                <table className="table">
                  <thead><tr><th>{t("Zeitpunkt")}</th><th>{t("Check")}</th><th>{t("Wechsel")}</th><th>{t("Ergebnis")}</th><th>{t("Benachrichtigt")}</th></tr></thead>
                  <tbody>
                    {events!.map((e) => (
                      <tr key={e.id}>
                        <td className="muted" title={new Date(e.created_at).toLocaleString()}>{relTime(e.created_at)}</td>
                        <td>{e.check_name || e.check_id}</td>
                        <td style={{ whiteSpace: "nowrap" }}>
                          <CheckStatusBadge status={e.old_status} /> <span className="muted">→</span> <CheckStatusBadge status={e.new_status} />
                        </td>
                        <td className="muted mono small">{(e.output || "").slice(0, 120) || "—"}</td>
                        <td>
                          {e.notified
                            ? <span className="badge badge-online" title={e.notified_at ? new Date(e.notified_at).toLocaleString() : ""}><span className="dot" /> {e.notified_at ? relTime(e.notified_at) : "ja"}</span>
                            : <span className="muted small">—</span>}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section className="card">
            <h3 className="muted small">{t("Task-Läufe")}</h3>
            {(taskRuns ?? []).length === 0 ? (
              <p className="muted">{t("Noch keine Task-Läufe.")}</p>
            ) : (
              <div className="scroll-list">
                <table className="table">
                  <thead><tr><th>{t("Zeitpunkt")}</th><th>{t("Task")}</th><th>{t("Status")}</th><th>{t("Ergebnis")}</th></tr></thead>
                  <tbody>
                    {taskRuns!.map((tk) => (
                      <tr key={tk.id}>
                        <td className="muted" title={new Date(tk.ran_at).toLocaleString()}>{relTime(tk.ran_at)}</td>
                        <td>{tk.name || tk.task_id}</td>
                        <td><span className={`badge ${tk.exit_code === 0 ? "badge-online" : "badge-offline"}`}><span className="dot" /> {tk.exit_code === 0 ? "OK" : t("Fehler")}</span></td>
                        <td className="muted mono small">{(tk.output || "").slice(0, 120) || `Exit ${tk.exit_code}`}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          <section className="card">
            <h3 className="muted small">{t("Software-Änderungen")}</h3>
            {(swEvents ?? []).length === 0 ? (
              <p className="muted">{t("Keine Software-Änderungen erfasst.")}</p>
            ) : (
              <div className="scroll-list">
                <table className="table">
                  <thead><tr><th>{t("Zeitpunkt")}</th><th>{t("Änderung")}</th><th>{t("Programm")}</th><th>{t("Version")}</th></tr></thead>
                  <tbody>
                    {swEvents!.map((e) => (
                      <tr key={e.id}>
                        <td className="muted" title={new Date(e.created_at).toLocaleString()}>{relTime(e.created_at)}</td>
                        <td>
                          {e.change === "added" && <span className="badge badge-online"><span className="dot" /> {t("installiert")}</span>}
                          {e.change === "removed" && <span className="badge badge-offline"><span className="dot" /> {t("entfernt")}</span>}
                          {e.change === "updated" && <span className="badge badge-warn"><span className="dot" /> {t("aktualisiert")}</span>}
                        </td>
                        <td>{e.name}</td>
                        <td className="muted mono small">
                          {e.change === "updated" ? `${e.old_version || "?"} → ${e.version || "?"}` : (e.version || "—")}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </>)}

        {tab === "storage" && (
          <TreeSizePanel deviceId={id} disks={device.disks ?? []} />
        )}

        {tab === "system" && (
          <ServicesProcesses deviceId={id} isAdmin={canOperate} />
        )}

        {tab === "files" && canOperate && (
          <FileBrowser deviceId={id} />
        )}

        {tab === "live" && (
          <>
            <LiveMetrics deviceId={id} />
            <section className="card">
              <h3 style={{ marginTop: 0 }}>{t("Verlauf")}</h3>
              <MetricsHistory deviceId={id} />
            </section>
          </>
        )}

        {tab === "vulns" && <Vulnerabilities deviceId={id} />}

        {tab === "security" && (
          <SecurityPanel deviceId={id} />
        )}

        {tab === "events" && (
          <EventLog deviceId={id} os={device.os} />
        )}

        {tab === "software" && (
          <section className="card">
            <div className="page-head" style={{ marginBottom: 12 }}>
              <h2 style={{ margin: 0 }}>Installierte Software ({(device.software ?? []).length})</h2>
              {(device.software ?? []).length > 0 && (
                <input className="search" placeholder={t("Software filtern…")} value={swFilter} onChange={(e) => setSwFilter(e.target.value)} />
              )}
            </div>
            {(device.software ?? []).length === 0 ? (
              <p className="muted">{t("Keine Software gemeldet.")}</p>
            ) : (
              <div className="scroll-list">
                <table className="table">
                  <thead><tr><th>{t("Name")}</th><th>{t("Version")}</th><th>{t("Herausgeber")}</th></tr></thead>
                  <tbody>
                    {device.software!.filter((s) => s.name.toLowerCase().includes(swFilter.toLowerCase())).map((s, i) => (
                      <tr key={i}><td>{s.name}</td><td className="muted mono">{s.version || "—"}</td><td className="muted">{s.publisher || "—"}</td></tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

        {tab === "updates" && (
          <section className="card">
            <div className="page-head" style={{ marginBottom: 10 }}>
              <h2 style={{ margin: 0, display: "flex", alignItems: "center", gap: 10 }}>Patches <UpdatesBadge count={device.updates_count} /></h2>
              {canOperate && (
                <div className="actions">
                  <button className="btn ghost sm" onClick={() => scanUpdates.mutate()} disabled={scanUpdates.isPending}>↻ {t("Jetzt prüfen")}</button>
                  {(device.updates_count ?? 0) > 0 && (
                    <>
                      {/^linux|debian|ubuntu/i.test(device.os) && (
                        <select value={aptMode} onChange={(e) => setAptMode(e.target.value as "full" | "safe")}
                          title={t("apt-Strategie bei „__ALLINSTALL__“")}>
                          <option value="full">{t("Voll-Upgrade (dist-upgrade)")}</option>
                          <option value="safe">{t("Sicheres Upgrade (upgrade)")}</option>
                        </select>
                      )}
                      <button className="btn ghost sm" disabled={installUpdates.isPending}
                        onClick={() => confirm(t("Nur die genehmigten Patches installieren?")) && installUpdates.mutate({ approved: true, apt_mode: aptMode })}>
                        __GENAPPROVE__
                      </button>
                      <button className="btn primary sm" disabled={installUpdates.isPending}
                        onClick={() => confirm(t("ALLE ausstehenden Updates installieren? Kann lange dauern und ggf. einen Neustart erfordern.")) && installUpdates.mutate({ approved: false, apt_mode: aptMode })}>
                        __ALLINSTALL__
                      </button>
                    </>
                  )}
                </div>
              )}
            </div>
            {installUpdates.isSuccess && <p className="muted small">{t("Installation eingereiht – Ergebnis erscheint unter „Ausführen“. Der Agent meldet es nach Abschluss.")}</p>}
            {device.updates_checked_at && (
              <p className="muted small">Zuletzt geprüft {relTime(device.updates_checked_at)}</p>
            )}
            {(device.available_updates ?? []).length === 0 ? (
              <p className="muted">
                {device.updates_count === undefined || device.updates_count === null
                  ? t("Noch nicht geprüft – mit „Jetzt prüfen“ einen Scan starten.")
                  : t("Keine ausstehenden Patches.")}
              </p>
            ) : (
              <div className="scroll-list">
                <table className="table">
                  <thead><tr><th>{t("Genehmigt")}</th><th>{t("Schwere")}</th><th>{t("Name")}</th><th>{t("Mehr Info")}</th></tr></thead>
                  <tbody>
                    {device.available_updates!.map((u, i) => (
                      <tr key={i}>
                        <td>
                          <input type="checkbox" checked={u.approved} disabled={!canOperate}
                            onChange={(e) => approvePatch.mutate({ name: u.name, approved: e.target.checked })} />
                        </td>
                        <td><SeverityBadge severity={u.severity} /></td>
                        <td>{u.name}</td>
                        <td>{u.url ? <a className="link-strong" href={u.url} target="_blank" rel="noreferrer">{t("Link")} ↗</a> : <span className="muted">—</span>}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

        {tab === "network" && (
          <section className="card">
            <table className="table">
              <thead><tr><th>{t("Name")}</th><th>{t("MAC-Adresse")}</th><th>IPv4</th><th>IPv6</th></tr></thead>
              <tbody>
                {(device.interfaces ?? []).map((i, idx) => (
                  <tr key={idx}><td>{i.name}</td><td className="mono"><CopyText value={i.mac || "—"} /></td><td className="mono">{(i.ipv4 || "—").split(",").map((ip, k) => <div key={k}><CopyText value={ip.trim()} /></div>)}</td><td className="mono">{(i.ipv6 || "—").split(",").map((ip, k) => <div key={k}><CopyText value={ip.trim()} /></div>)}</td></tr>
                ))}
                {(device.interfaces ?? []).length === 0 && <tr><td colSpan={4} className="empty">{t("Keine Schnittstellen gemeldet.")}</td></tr>}
              </tbody>
            </table>
          </section>
        )}

        {tab === "run" && canOperate && (
          <section className="card">
            <form className="inline-form" onSubmit={(e) => { e.preventDefault(); if (runScriptId) runScript.mutate(); }}>
              <select value={runScriptId} onChange={(e) => setRunScriptId(e.target.value)}>
                <option value="">{t("— Skript wählen —")}</option>
                {(scripts ?? []).filter((s) => !s.check_only).map((s) => <option key={s.id} value={s.id}>{s.name} ({s.shell})</option>)}
              </select>
              <button className="btn primary" type="submit" disabled={runScript.isPending || !runScriptId}>Ausführen</button>
              <span className="muted small">{t("Der Agent führt es beim nächsten Checkin aus.")}</span>
            </form>
            {(device.commands ?? []).length > 0 && (
              <div className="scroll-list" style={{ marginTop: 10 }}>
                <table className="table">
                  <thead><tr><th>{t("Skript")}</th><th>{t("Status")}</th><th>Exit</th><th>{t("Zeit")}</th><th>{t("Ausgabe")}</th></tr></thead>
                  <tbody>
                    {device.commands!.map((c) => (
                      <tr key={c.id}>
                        <td>{c.label}</td>
                        <td>{c.status === "done"
                          ? <span className={`badge ${c.exit_code === 0 ? "badge-online" : "badge-offline"}`}>{t("fertig")}</span>
                          : <span className="badge badge-unknown">{c.status === "sent" ? t("läuft") : t("wartet")}</span>}</td>
                        <td>{c.status === "done" ? c.exit_code : "—"}</td>
                        <td className="muted">{relTime(c.ran_at || c.created_at)}</td>
                        <td className="mono small">{(c.output || "").slice(0, 160)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

        {tab === "terminal" && canOperate && (
          <DeviceTerminal id={id} os={device.os} />
        )}

        {tab === "remote" && canOperate && (
          <section className="card">
            <DeviceRemote id={id} os={device.os} />
          </section>
        )}

        {tab === "fields" && (
          <section className="card">
            <CustomFieldsEditor model="device" entityId={id} />
          </section>
        )}
      </div>
    </div>
  );
}
