import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import type { DashboardSummary } from "../types";
import { CheckStatusBadge, relTime } from "../components/StatusBadge";
import { useI18n } from "../i18n";

// Tile ist eine Kennzahl-Kachel; klickbar führt sie z.B. zur Geräteliste.
function Tile({ label, value, sub, tone, onClick }: {
  label: string; value: number | string; sub?: string; tone?: "ok" | "warn" | "bad"; onClick?: () => void;
}) {
  return (
    <div className={`dash-tile${tone ? ` dash-${tone}` : ""}${onClick ? " dash-click" : ""}`} onClick={onClick}>
      <div className="dash-value">{value}</div>
      <div className="dash-label">{label}</div>
      {sub && <div className="muted small">{sub}</div>}
    </div>
  );
}

export function Dashboard() {
  const nav = useNavigate();
  const { t } = useI18n();
  const { data, isLoading } = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api.get<DashboardSummary>("/dashboard"),
    refetchInterval: 15000,
  });

  if (isLoading || !data) return <div className="page wide"><p className="muted">{t("Lädt…")}</p></div>;

  const onlinePct = data.devices_total ? Math.round((data.devices_online / data.devices_total) * 100) : 0;
  // Health-Donut: online (grün) / offline (rot) / unbekannt (grau).
  const seg = [
    { c: "#34c759", v: data.devices_online },
    { c: "#ff453a", v: data.devices_offline },
    { c: "#6b7280", v: data.devices_unknown },
  ];
  const tot = seg.reduce((a, s) => a + s.v, 0) || 1;
  let acc = 0;
  const stops = seg.map((s) => {
    const start = (acc / tot) * 100; acc += s.v; const end = (acc / tot) * 100;
    return `${s.c} ${start}% ${end}%`;
  }).join(", ");

  return (
    <div className="page wide">
      <header className="page-head">
        <div>
          <h1>{t("Übersicht")}</h1>
          <p className="muted">{t("Zusammenfassung über alle Geräte. Aktualisiert sich automatisch.")}</p>
        </div>
      </header>

      <div className="dash-grid">
        <Tile label={t("Geräte gesamt")} value={data.devices_total} onClick={() => nav("/devices")} />
        <Tile label={t("Online")} value={data.devices_online} sub={`${onlinePct}%`} tone="ok" onClick={() => nav("/devices")} />
        <Tile label={t("Offline")} value={data.devices_offline} tone={data.devices_offline > 0 ? "bad" : undefined} onClick={() => nav("/devices")} />
        <Tile label={t("Checks fehlerhaft")} value={data.failing_checks} sub={t("{n} Gerät(e)", { n: data.devices_with_failing_checks })} tone={data.failing_checks > 0 ? "bad" : "ok"} onClick={() => nav("/devices?filter=failing-checks")} />
        <Tile label={t("Tasks fehlerhaft")} value={data.failing_tasks} sub={t("{n} Gerät(e)", { n: data.devices_with_failing_tasks })} tone={data.failing_tasks > 0 ? "warn" : "ok"} onClick={() => nav("/devices?filter=failing-tasks")} />
        <Tile label={t("Ausstehende Patches")} value={data.pending_patches} sub={t("{n} Gerät(e)", { n: data.devices_with_pending_patches })} tone={data.pending_patches > 0 ? "warn" : "ok"} onClick={() => nav("/devices")} />
        <Tile label={t("Schwachstellen")} value={data.vulnerabilities} sub={t("{n} Gerät(e)", { n: data.devices_with_vulns })} tone={data.vulnerabilities > 0 ? "bad" : "ok"} onClick={() => nav("/devices?filter=vulns")} />
      </div>

      <div className="grid-2" style={{ marginTop: 18 }}>
        <section className="card">
          <h2>{t("Geräte-Status")}</h2>
          <div className="donut-wrap" style={{ flexDirection: "row", alignItems: "center", gap: 24 }}>
            <div className="donut" style={{ background: `conic-gradient(${stops})` }}><div className="donut-hole" /></div>
            <ul className="donut-legend" style={{ width: "auto" }}>
              <li><span className="donut-swatch" style={{ background: "#34c759" }} /> {t("Online")} <span className="muted small">{data.devices_online}</span></li>
              <li><span className="donut-swatch" style={{ background: "#ff453a" }} /> {t("Offline")} <span className="muted small">{data.devices_offline}</span></li>
              <li><span className="donut-swatch" style={{ background: "#6b7280" }} /> {t("Unbekannt")} <span className="muted small">{data.devices_unknown}</span></li>
            </ul>
          </div>
        </section>

        <section className="card">
          <h2>{t("Letzte Status-Wechsel")}</h2>
          {(data.recent_events ?? []).length === 0 ? (
            <p className="muted">{t("Keine Statuswechsel.")}</p>
          ) : (
            <div className="scroll-list" style={{ maxHeight: 300 }}>
              <table className="table">
                <tbody>
                  {data.recent_events.map((e) => (
                    <tr key={e.id} className="dash-click" onClick={() => nav(`/devices/${e.device_id}`)}>
                      <td className="muted" title={new Date(e.created_at).toLocaleString()} style={{ whiteSpace: "nowrap" }}>{relTime(e.created_at)}</td>
                      <td>{e.hostname || "—"}</td>
                      <td className="muted small">{e.check_name || e.check_id}</td>
                      <td style={{ whiteSpace: "nowrap" }}>
                        <CheckStatusBadge status={e.old_status} /> <span className="muted">→</span> <CheckStatusBadge status={e.new_status} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
