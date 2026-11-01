import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { AuthEvent, SecurityIncident } from "../types";
import { Flag } from "./Country";

export const INCIDENT_KIND: Record<string, string> = {
  brute_force: "Brute-Force", brute_force_success: "Anmeldung nach Brute-Force", new_source_ip: "Neue Quell-IP",
  account_change: "Kontoänderung", lockout: "Kontosperre", fim_change: "Datei-Änderung", scan: "Angriffsmuster",
};
export const AUTH_KIND: Record<string, { label: string; cls: string }> = {
  login_failed: { label: "Anmeldung fehlgeschlagen", cls: "badge-offline" },
  invalid_user: { label: "unbekannter Benutzer", cls: "badge-offline" },
  login_ok: { label: "Anmeldung", cls: "badge-online" },
  lockout: { label: "Kontosperre", cls: "badge-warn" },
  sudo: { label: "sudo", cls: "badge-unknown" },
  su: { label: "su", cls: "badge-unknown" },
  user_added: { label: "Benutzer angelegt", cls: "badge-warn" },
  user_removed: { label: "Benutzer gelöscht", cls: "badge-warn" },
  user_modified: { label: "Benutzer geändert", cls: "badge-unknown" },
  group_changed: { label: "Gruppe geändert", cls: "badge-warn" },
  password_changed: { label: "Passwort geändert", cls: "badge-unknown" },
  probe: { label: "verdächtige Anfrage", cls: "badge-warn" },
};

export function SeverityPill({ severity }: { severity: string }) {
  const { t } = useI18n();
  const cls = severity === "critical" ? "badge-offline" : severity === "warning" ? "badge-warn" : "badge-unknown";
  const label = severity === "critical" ? "kritisch" : severity === "warning" ? "Warnung" : "Info";
  return <span className={`badge ${cls}`}>{t(label)}</span>;
}

// IncidentsTable listet Vorfälle mit Bestätigen/Erledigen und aufklappbaren Details.
export function IncidentsTable({ incidents, showDevice, canOperate, maxHeight, actions, selected, onSelect, onSelectAll }: {
  incidents: SecurityIncident[]; showDevice?: boolean; canOperate: boolean; maxHeight?: string | number;
  actions?: (inc: SecurityIncident) => React.ReactNode;
  // Mehrfachauswahl (nur Vorfälle mit Quell-IP), z. B. für „IPs sperren“.
  selected?: Set<string>; onSelect?: (id: string, on: boolean) => void; onSelectAll?: (on: boolean) => void;
}) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [open, setOpen] = useState<string | null>(null);
  const setStatus = useMutation({
    mutationFn: (v: { id: string; status: string }) => api.put(`/security/incidents/${v.id}`, { status: v.status }),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["incidents"] }); qc.invalidateQueries({ queryKey: ["security-summary"] }); qc.invalidateQueries({ queryKey: ["device"] }); qc.invalidateQueries({ queryKey: ["devices"] }); qc.invalidateQueries({ queryKey: ["dashboard"] }); },
  });
  if (incidents.length === 0) return <p className="empty">{t("Keine Vorfälle im Zeitraum.")}</p>;
  const selectable = incidents.filter((i) => !!i.source_ip);
  const allOn = selectable.length > 0 && selectable.every((i) => selected?.has(i.id));
  const cols = (showDevice ? 7 : 6) + (onSelect ? 1 : 0);
  return (
    <div className="scroll-list" style={{ maxHeight: maxHeight ?? "65vh" }}>
      <table className="table">
        <thead><tr>{onSelect && <th style={{ width: 24 }}><input type="checkbox" checked={allOn} onChange={(e) => onSelectAll?.(e.target.checked)} title={t("Alle mit Quell-IP auswählen")} /></th>}<th>{t("Zeit")}</th>{showDevice && <th>{t("Gerät")}</th>}<th>{t("Schwere")}</th><th>{t("Vorfall")}</th><th>{t("Quelle")}</th><th>{t("Status")}</th><th></th></tr></thead>
        <tbody>
          {incidents.map((i) => [
            <tr key={i.id} className={`small ${i.status !== "open" ? "muted" : ""}`} style={{ cursor: "pointer" }} onClick={() => setOpen(open === i.id ? null : i.id)}>
              {onSelect && <td onClick={(e) => e.stopPropagation()}>{i.source_ip && <input type="checkbox" checked={selected?.has(i.id) ?? false} onChange={(e) => onSelect(i.id, e.target.checked)} />}</td>}
              <td className="mono muted">{new Date(i.ts).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })}</td>
              {showDevice && <td><Link to={`/devices/${i.device_id}`} className="link-strong" onClick={(e) => e.stopPropagation()}>{i.hostname}</Link>{i.client_name && <span className="muted"> · {i.client_name}</span>}</td>}
              <td><SeverityPill severity={i.severity} /></td>
              <td><span className="muted">{open === i.id ? "▾" : "▸"}</span> <strong>{t(INCIDENT_KIND[i.kind] ?? i.kind)}</strong> <span className="muted">· {i.title}</span></td>
              <td className="mono"><Flag cc={i.country} />{i.source_ip || "—"}{i.username && <span className="muted"> · {i.username}</span>}</td>
              <td>{i.status === "open" ? <span className="badge badge-warn">{t("offen")}</span> : i.status === "acknowledged" ? <span className="badge badge-unknown">{t("bestätigt")}</span> : <span className="badge badge-online">{t("erledigt")}</span>}
                {i.response && <div className="muted small" title={i.response_status}>↳ {i.response}{i.response_status ? ` (${i.response_status})` : ""}</div>}</td>
              <td style={{ whiteSpace: "nowrap" }} onClick={(e) => e.stopPropagation()}>
                {canOperate && i.status === "open" && <button className="btn ghost sm" disabled={setStatus.isPending} onClick={() => setStatus.mutate({ id: i.id, status: "acknowledged" })}>{t("Bestätigen")}</button>}
                {canOperate && i.status !== "resolved" && <button className="btn ghost sm" disabled={setStatus.isPending} onClick={() => setStatus.mutate({ id: i.id, status: "resolved" })}>✓ {t("Erledigt")}</button>}
                {canOperate && i.status === "resolved" && <button className="btn ghost sm" disabled={setStatus.isPending} onClick={() => setStatus.mutate({ id: i.id, status: "open" })}>{t("Wieder öffnen")}</button>}
                {actions?.(i)}
              </td>
            </tr>,
            ...(open === i.id ? [(
              <tr key={i.id + ":d"}><td colSpan={cols} style={{ paddingLeft: 28 }}>
                <div className="small" style={{ whiteSpace: "pre-wrap" }}>{i.detail || "—"}</div>
                {i.acknowledged_by && <div className="muted small">{t("Bearbeitet von {u} {t}", { u: i.acknowledged_by, t: i.acknowledged_at ? new Date(i.acknowledged_at).toLocaleString() : "" })}</div>}
              </td></tr>
            )] : []),
          ])}
        </tbody>
      </table>
    </div>
  );
}

// AuthEventsTable listet Rohereignisse.
export function AuthEventsTable({ events, showDevice, maxHeight }: { events: AuthEvent[]; showDevice?: boolean; maxHeight?: string | number }) {
  const { t } = useI18n();
  if (events.length === 0) return <p className="empty">{t("Keine Anmeldeereignisse im Zeitraum.")}</p>;
  return (
    <div className="scroll-list" style={{ maxHeight: maxHeight ?? "65vh" }}>
      <table className="table">
        <thead><tr><th>{t("Zeit")}</th>{showDevice && <th>{t("Gerät")}</th>}<th>{t("Ereignis")}</th><th>{t("Benutzer")}</th><th>{t("Quelle")}</th><th>{t("Dienst")}</th><th>{t("Details")}</th></tr></thead>
        <tbody>
          {events.map((e) => {
            const m = AUTH_KIND[e.kind] ?? { label: e.kind, cls: "badge-unknown" };
            return (
              <tr key={e.id} className="small">
                <td className="mono muted">{new Date(e.ts).toLocaleString(undefined, { dateStyle: "short", timeStyle: "medium" })}</td>
                {showDevice && <td><Link to={`/devices/${e.device_id}`} className="link-strong">{e.hostname}</Link></td>}
                <td><span className={`badge ${m.cls}`}>{t(m.label)}</span></td>
                <td>{e.username || "—"}</td>
                <td className="mono"><Flag cc={e.country} />{e.source_ip || "—"}</td>
                <td className="muted">{e.service || "—"}</td>
                <td className="muted" title={e.detail}>{e.detail ? (e.detail.length > 80 ? e.detail.slice(0, 80) + "…" : e.detail) : ""}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
