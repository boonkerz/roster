import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import { AuthEventsTable, IncidentsTable } from "./SecurityEvents";
import { IncidentActions } from "./IncidentActions";
import type { AuthEvent, IPBlock, LogSourceSpec, SecurityIncident, SecurityMonitoring } from "../types";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { relTime } from "./StatusBadge";
import { LogSourceTest } from "./LogSourceTest";

// DeviceIncidents zeigt Sicherheitsvorfälle und aktive IP-Sperren eines Geräts (Tab „Vorfälle“).
export function DeviceIncidents({ deviceId, canOperate }: { deviceId: string; canOperate: boolean }) {
  const { t } = useI18n();
  const [days, setDays] = useState("7");
  const { data: incidents } = useQuery({ queryKey: ["incidents", deviceId, days], queryFn: () => api.get<SecurityIncident[]>(`/devices/${deviceId}/incidents?days=${days}`), refetchInterval: 30000 });
  const qc = useQueryClient();
  const { data: blocks } = useQuery({ queryKey: ["ip-blocks", deviceId], queryFn: () => api.get<IPBlock[]>(`/devices/${deviceId}/blocks`), refetchInterval: 30000 });
  const unblock = useMutation({ mutationFn: (b: IPBlock) => api.post(`/devices/${deviceId}/unblock-ip`, { ip: b.ip }), onSuccess: () => qc.invalidateQueries({ queryKey: ["ip-blocks"] }) });
  const activeBlocks = (blocks ?? []).filter((b) => b.status === "active" || b.status === "pending");
  const open = (incidents ?? []).filter((i) => i.status === "open").length;
  return (
    <>
      <section className="card">
        <div className="page-head" style={{ marginBottom: 10 }}>
          <h2 style={{ margin: 0 }}>{t("Sicherheitsvorfälle")} {open > 0 && <span className="tab-count tab-count-bad">{open}</span>}</h2>
          <select value={days} onChange={(e) => setDays(e.target.value)}>
            <option value="7">{t("7 Tage")}</option>
            <option value="30">{t("30 Tage")}</option>
            <option value="90">{t("90 Tage")}</option>
          </select>
        </div>
        <IncidentsTable incidents={incidents ?? []} canOperate={canOperate} maxHeight="60vh" actions={canOperate ? (i) => <IncidentActions incident={i} /> : undefined} />
      </section>
      <section className="card">
        <h2 style={{ margin: "0 0 8px" }}>{t("Aktive IP-Sperren")} {activeBlocks.length > 0 && <span className="tab-count">{activeBlocks.length}</span>}</h2>
        {activeBlocks.length === 0 ? <p className="muted small">{t("Keine aktiven Sperren auf diesem Gerät.")}</p> : (
          <div>
            {activeBlocks.map((b) => (
              <span key={b.id} className="badge badge-offline" style={{ marginRight: 6, marginBottom: 6, display: "inline-flex", alignItems: "center" }} title={b.reason}>🚫 {b.ip}{b.until ? ` · ${t("bis")} ${new Date(b.until).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })}` : ` · ${t("dauerhaft")}`}
                {canOperate && <button className="btn ghost sm" style={{ marginLeft: 4, padding: "0 4px" }} onClick={() => unblock.mutate(b)} title={t("Aufheben")}>✕</button>}
              </span>
            ))}
          </div>
        )}
        <p className="muted small" style={{ marginTop: 6 }}><Link to="/security?tab=blocks" className="link-strong">{t("Alle Sperren mit Suche")}</Link></p>
      </section>
    </>
  );
}

// DeviceAuth zeigt die wirksame Überwachung und die Anmeldeereignisse eines Geräts (Tab „Anmeldungen“).
export function DeviceAuth({ deviceId, canOperate }: { deviceId: string; canOperate: boolean }) {
  const { t } = useI18n();
  const [days, setDays] = useState("7");
  const [kind, setKind] = useState("");
  const { data: events, isLoading } = useQuery({ queryKey: ["auth-events", deviceId, days, kind], queryFn: () => api.get<AuthEvent[]>(`/devices/${deviceId}/auth-events?days=${days}${kind ? `&kind=${kind}` : ""}&limit=500`), refetchInterval: 30000 });
  const { data: mon } = useQuery({ queryKey: ["security-monitoring", deviceId], queryFn: () => api.get<SecurityMonitoring>(`/devices/${deviceId}/security-monitoring`), refetchInterval: 60000 });
  const [testSrc, setTestSrc] = useState<LogSourceSpec | null>(null);
  return (
    <>
      {mon && <MonitoringCard mon={mon} deviceId={deviceId} canOperate={canOperate} testSrc={testSrc} setTestSrc={setTestSrc} />}
      <section className="card">
        <div className="page-head" style={{ marginBottom: 10 }}>
          <h2 style={{ margin: 0 }}>{t("Anmeldungen")}</h2>
          <div className="head-actions">
            <select value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="">{t("Alle Ereignisse")}</option>
              <option value="failed">{t("Fehlversuche")}</option>
              <option value="probe">{t("verdächtige Anfrage")}</option>
              <option value="login_ok">{t("Anmeldung")}</option>
              <option value="sudo">sudo</option>
              <option value="user_added">{t("Benutzer angelegt")}</option>
              <option value="group_changed">{t("Gruppe geändert")}</option>
            </select>
            <select value={days} onChange={(e) => setDays(e.target.value)}>
              <option value="1">{t("24 Stunden")}</option>
              <option value="7">{t("7 Tage")}</option>
              <option value="30">{t("30 Tage")}</option>
            </select>
          </div>
        </div>
        <p className="muted small">{t("Anmeldeereignisse des Systems (SSH/RDP/Konsole, sudo, Kontoänderungen) und Treffer der Log-Quellen. Vorfälle daraus stehen im Tab „Vorfälle“.")}</p>
        {isLoading ? <p className="muted">{t("Lädt…")}</p> : <AuthEventsTable events={events ?? []} maxHeight="60vh" />}
      </section>
    </>
  );
}

// MonitoringCard zeigt, welche Sicherheitsüberwachung auf dem Gerät wirkt – mit dem vom
// Agent gemeldeten Lesestatus je Log-Quelle (gefundene Ziele, Ereignisse, letzter Treffer).
function MonitoringCard({ mon, deviceId, canOperate, testSrc, setTestSrc }: { mon: SecurityMonitoring; deviceId: string; canOperate: boolean; testSrc: LogSourceSpec | null; setTestSrc: (s: LogSourceSpec | null) => void }) {
  const { t } = useI18n();
  const none = !mon.auth_monitor && mon.log_sources.length === 0 && mon.fim_rules === 0 && !mon.compliance;
  return (
    <section className="card">
      <h2 style={{ margin: "0 0 8px" }}>{t("Überwachung auf diesem Gerät")}</h2>
      {none ? <p className="muted small">{t("Keine Richtlinie mit Sicherheitsüberwachung zugewiesen. Aktivieren Sie in einer Richtlinie (Tab Sicherheit) die Anmeldungs-Überwachung oder Log-Quellen und weisen Sie sie dem Gerät, Standort oder Client zu.")}</p> : (
        <div className="chip-row" style={{ marginBottom: 8 }}>
          <span className={`badge ${mon.auth_monitor ? "badge-online" : "badge-unknown"}`}>{mon.auth_monitor ? `✓ ${t("Anmeldungen")}` : t("Anmeldungen: aus")}{mon.auth_monitor && mon.auth_monitor_since ? ` · ${t("seit")} ${new Date(mon.auth_monitor_since).toLocaleDateString()}` : ""}</span>
          <span className={`badge ${mon.fim_rules > 0 ? "badge-online" : "badge-unknown"}`}>{mon.fim_rules > 0 ? `✓ ${t("Datei-Integrität")} · ${mon.fim_rules}` : t("Datei-Integrität: aus")}</span>
          <span className={`badge ${mon.compliance ? "badge-online" : "badge-unknown"}`}>{mon.compliance ? `✓ ${t("Compliance")}` : t("Compliance: aus")}</span>
          <span className={`badge ${mon.log_sources.length > 0 ? "badge-online" : "badge-unknown"}`}>{t("Log-Quellen")} · {mon.log_sources.length}</span>
        </div>
      )}
      {mon.log_sources.length > 0 && (
        <table className="table">
          <thead><tr><th>{t("Dienst")}</th><th>{t("Quelle")}</th><th>{t("Richtlinie")}</th><th style={{ minWidth: 280 }}>{t("Status")}</th><th title={t("Zählt nur neue Zeilen seit dem Start des Agents; „Testen“ liest dagegen die letzten Zeilen rückwirkend.")}>{t("Ereignisse seit Agent-Start")}</th><th>{t("Letzter Treffer")}</th><th></th></tr></thead>
          <tbody>
            {mon.log_sources.map((l) => {
              const ok = l.reported && !l.error && l.targets > 0;
              return (
                <tr key={l.id} className="small">
                  <td>{l.name}</td>
                  <td className="mono">{l.type === "docker" ? "🐳 " : ""}{l.target}</td>
                  <td>{l.policy_id ? <Link to={`/policies?id=${l.policy_id}`} className="link-strong">{l.policy_name || l.policy_id}</Link> : "—"}</td>
                  <td>
                    {!l.reported ? <span className="badge badge-unknown" title={t("Der Agent hat noch keinen Status gemeldet (Agent zu alt oder noch kein Lauf).")}>{t("noch nicht gemeldet")}</span>
                      : ok ? <span className="badge badge-online">✓ {l.type === "docker" ? t("{n} Container", { n: l.targets }) : t("{n} Datei(en)", { n: l.targets })}</span>
                      : <span className="badge badge-offline" title={l.error}>{t("Fehler")}</span>}
                    {l.error && <div className="muted small" style={{ maxWidth: 480, overflowWrap: "anywhere" }} title={l.error}>{l.error.length > 160 ? l.error.slice(0, 160) + "…" : l.error}</div>}
                    {l.reported && l.last_run_at && <div className="muted small">{t("zuletzt gelesen")} {relTime(l.last_run_at)}</div>}
                  </td>
                  <td>{l.reported ? l.events : "—"}</td>
                  <td className="muted">{l.last_event_at ? relTime(l.last_event_at) : "—"}</td>
                  <td style={{ textAlign: "right", whiteSpace: "nowrap" }}>{canOperate && <button className="btn ghost sm" onClick={() => setTestSrc({ name: l.name, type: l.type, target: l.target, format: l.format ?? "nginx_access", pattern: l.pattern, kind: l.kind })}>▶ {t("Testen")}</button>}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {testSrc && <LogSourceTest source={testSrc} deviceId={deviceId} onClose={() => setTestSrc(null)} />}
    </section>
  );
}
