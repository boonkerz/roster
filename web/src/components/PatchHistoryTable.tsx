import { Link } from "react-router-dom";
import { useI18n } from "../i18n";
import type { PatchEvent } from "../types";

// Aktion → Badge-Klasse/Label des Patch-Verlaufs.
export const PATCH_ACTION: Record<PatchEvent["action"], { label: string; cls: string }> = {
  install: { label: "installiert", cls: "badge-online" },
  upgrade: { label: "aktualisiert", cls: "badge-warn" },
  remove: { label: "entfernt", cls: "badge-offline" },
};

export function PatchVersion({ e }: { e: PatchEvent }) {
  if (e.old_version && e.new_version) return <span className="mono small muted">{e.old_version} → {e.new_version}</span>;
  return <span className="mono small muted">{e.new_version || e.old_version || "—"}</span>;
}

// PatchHistoryTable listet Verlaufseinträge nach Tag gruppiert (neueste zuerst) –
// flottenweit (mit Gerätespalte) oder je Gerät.
export function PatchHistoryTable({ events, showDevice, maxHeight }: { events: PatchEvent[]; showDevice?: boolean; maxHeight?: string | number }) {
  const { t } = useI18n();
  const byDay: [string, PatchEvent[]][] = [];
  for (const e of events) {
    const day = new Date(e.ts).toLocaleDateString(undefined, { weekday: "short", year: "numeric", month: "2-digit", day: "2-digit" });
    const last = byDay[byDay.length - 1];
    if (last && last[0] === day) last[1].push(e); else byDay.push([day, [e]]);
  }
  const cols = showDevice ? 7 : 6;
  if (events.length === 0) return <p className="empty">{t("Keine Verlaufseinträge im gewählten Zeitraum.")}</p>;
  return (
    <div className="scroll-list" style={{ maxHeight: maxHeight ?? "70vh" }}>
      <table className="table">
        <thead><tr><th>{t("Zeit")}</th>{showDevice && <th>{t("Gerät")}</th>}<th>{t("Aktion")}</th><th>{t("Paket")}</th><th>{t("Version")}</th><th>{t("Quelle")}</th><th>{t("Details")}</th></tr></thead>
        <tbody>
          {byDay.map(([day, list]) => [
            <tr key={day} className="row-selected"><td colSpan={cols}><strong>{day}</strong> <span className="muted small">· {t("{n} Einträge", { n: list.length })}</span></td></tr>,
            ...list.map((e) => (
              <tr key={e.id} className="small">
                <td className="muted mono">{new Date(e.ts).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}</td>
                {showDevice && <td><Link to={`/devices/${e.device_id}`} className="link-strong">{e.hostname}</Link>{e.client_name && <span className="muted"> · {e.client_name}</span>}</td>}
                <td><span className={`badge ${PATCH_ACTION[e.action].cls}`}>{t(PATCH_ACTION[e.action].label)}</span></td>
                <td>{e.name}</td>
                <td><PatchVersion e={e} /></td>
                <td className="muted">{e.source || "—"}</td>
                <td className="muted" title={e.detail}>{e.detail ? (e.detail.length > 60 ? e.detail.slice(0, 60) + "…" : e.detail) : ""}</td>
              </tr>
            )),
          ])}
        </tbody>
      </table>
    </div>
  );
}
