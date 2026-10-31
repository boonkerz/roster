import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { FIMEvent } from "../types";

export const FIM_CHANGE: Record<string, { label: string; cls: string }> = {
  created: { label: "neu", cls: "badge-warn" },
  modified: { label: "geändert", cls: "badge-offline" },
  deleted: { label: "gelöscht", cls: "badge-offline" },
  attrs: { label: "Rechte/Besitzer", cls: "badge-warn" },
  baseline: { label: "Baseline", cls: "badge-online" },
  limit: { label: "Limit", cls: "badge-unknown" },
};

const short = (h?: string) => (h ? h.slice(0, 10) + "…" : "—");

// FimEventsTable listet Datei-Änderungen mit Bestätigen; flottenweit mit Gerätespalte.
export function FimEventsTable({ events, showDevice, canOperate, onChanged, maxHeight }: {
  events: FIMEvent[]; showDevice?: boolean; canOperate: boolean; onChanged?: () => void; maxHeight?: string | number;
}) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [sel, setSel] = useState<Set<string>>(new Set());
  const open = useMemo(() => events.filter((e) => !e.acknowledged && e.change !== "baseline" && e.change !== "limit"), [events]);
  const ack = useMutation({
    mutationFn: (ids: string[]) => api.put<{ acknowledged: number }>("/security/fim/ack", { ids }),
    onSuccess: () => { setSel(new Set()); qc.invalidateQueries({ queryKey: ["fim"] }); qc.invalidateQueries({ queryKey: ["device"] }); qc.invalidateQueries({ queryKey: ["devices"] }); onChanged?.(); },
  });
  const toggle = (id: string) => setSel((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n; });
  if (events.length === 0) return <p className="empty">{t("Keine Datei-Änderungen im Zeitraum.")}</p>;
  return (
    <>
      {canOperate && open.length > 0 && (
        <div className="bulk-bar">
          <strong>{t("{n} offen", { n: open.length })}</strong>
          <button className="btn sm" disabled={sel.size === 0 || ack.isPending} onClick={() => ack.mutate([...sel])}>✓ {t("Ausgewählte bestätigen")}</button>
          <button className="btn sm ghost" disabled={ack.isPending} onClick={() => ack.mutate(open.map((e) => e.id))}>{t("Alle offenen bestätigen")}</button>
        </div>
      )}
      <div className="scroll-list" style={{ maxHeight: maxHeight ?? "65vh" }}>
        <table className="table">
          <thead><tr>{canOperate && <th></th>}<th>{t("Zeit")}</th>{showDevice && <th>{t("Gerät")}</th>}<th>{t("Änderung")}</th><th>{t("Pfad")}</th><th>{t("Details")}</th><th></th></tr></thead>
          <tbody>
            {events.map((e) => {
              const m = FIM_CHANGE[e.change] ?? { label: e.change, cls: "badge-unknown" };
              const isMeta = e.change === "baseline" || e.change === "limit";
              return (
                <tr key={e.id} className={`small ${e.acknowledged ? "muted" : ""}`}>
                  {canOperate && <td>{!isMeta && !e.acknowledged && <input type="checkbox" checked={sel.has(e.id)} onChange={() => toggle(e.id)} />}</td>}
                  <td className="mono muted" title={new Date(e.ts).toLocaleString()}>{new Date(e.ts).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })}</td>
                  {showDevice && <td><Link to={`/devices/${e.device_id}`} className="link-strong">{e.hostname}</Link>{e.client_name && <span className="muted"> · {e.client_name}</span>}</td>}
                  <td><span className={`badge ${m.cls}`}>{t(m.label)}</span>{!e.alert && !isMeta && <span className="muted" title={t("Regel ohne Alarm")}> 🔕</span>}</td>
                  <td className="mono" style={{ wordBreak: "break-all" }}>{e.change === "baseline" ? t("Baseline erstellt: {n} Dateien", { n: e.size }) : e.change === "limit" ? t("Obergrenze von {n} Einträgen erreicht – Regeln eingrenzen", { n: e.size }) : e.path}</td>
                  <td className="muted mono">
                    {e.change === "modified" && <>{short(e.old_hash)} → {short(e.new_hash)}</>}
                    {e.change === "attrs" && <>{e.old_mode} {e.old_owner} → {e.new_mode} {e.new_owner}</>}
                    {e.change === "created" && <>{e.new_mode} {e.new_owner} · {e.size} B</>}
                    {e.change === "deleted" && <>{e.old_mode} {e.old_owner}</>}
                  </td>
                  <td>{e.acknowledged && !isMeta && <span className="muted small">✓ {t("bestätigt")}</span>}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </>
  );
}
