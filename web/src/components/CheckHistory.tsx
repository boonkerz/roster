import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { CheckResult, CheckHistoryResponse } from "../types";
import { LineChart, fmtNum, type Band } from "../charts";
import { CheckStatusBadge } from "./StatusBadge";

type Range = "1h" | "6h" | "24h" | "7d" | "30d";
const RANGES: { id: Range; label: string }[] = [
  { id: "1h", label: "1 Stunde" }, { id: "6h", label: "6 Stunden" }, { id: "24h", label: "24 Stunden" },
  { id: "7d", label: "7 Tage" }, { id: "30d", label: "30 Tage" },
];
// Einheit des Messwerts je Check-Typ (sonst einheitenlos: Anzahl, Statuscode, Script-Zahl).
const UNIT: Record<string, string> = { disk: "%", memory: "%", cpu: "%", ping: "ms", temperature: "°C" };
// Checks, deren Wert eine echte Messgröße ist – auch wenn sie gerade 0 beträgt (z. B. 0 Updates).
const NUMERIC = new Set(["disk", "memory", "cpu", "updates", "ping", "tcp", "http", "temperature", "script", "ports", "fan", "proxmox_backup", "cert", "uptime"]);
const STATUS_COLOR: Record<string, string> = {
  passing: "var(--viz-status-ok)", warning: "var(--viz-status-warn)", failing: "var(--viz-status-bad)", unknown: "var(--viz-status-unknown)",
};
const STATUS_LABEL: Record<string, string> = { passing: "OK", warning: "Warnung", failing: "Fehler", unknown: "Unbekannt" };

// CheckHistoryDialog zeigt den Verlauf eines Checks am Gerät: Messwert als Linie und
// darunter den Status je Zeitfenster als farbiger Streifen. Auflösung ist das
// Checkin-Intervall; längere Zeiträume werden serverseitig zusammengefasst.
export function CheckHistoryDialog({ deviceId, check, onClose }: { deviceId: string; check: CheckResult; onClose: () => void }) {
  const { t } = useI18n();
  const [range, setRange] = useState<Range>("24h");
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  const { data, isLoading } = useQuery({
    queryKey: ["check-history", deviceId, check.check_id, range],
    queryFn: () => api.get<CheckHistoryResponse>(`/devices/${deviceId}/checks/${check.check_id}/history?range=${range}`),
    refetchInterval: 60000,
  });
  const points = data?.points ?? [];
  const unit = UNIT[check.type ?? ""];
  const numeric = NUMERIC.has(check.type ?? "") || points.some((p) => p.max !== 0);

  // Jedes Fenster gilt bis zum nächsten Punkt (bei Rohdaten also bis zum nächsten
  // Checkin); nach einer längeren Lücke (Gerät offline) endet das Band früher.
  // Aufeinanderfolgende Fenster gleichen Status werden zu einem Band zusammengefasst.
  const bands: (Band & { status: string })[] = [];
  if (data) {
    const gaps = points.slice(1).map((p, i) => p.ts - points[i].ts).sort((a, b) => a - b);
    const typical = gaps.length ? gaps[Math.floor(gaps.length / 2)] : data.bucket_ms;
    const maxSpan = Math.max(data.bucket_ms, 3 * typical);
    points.forEach((p, i) => {
      const next = points[i + 1];
      const to = Math.min(next ? next.ts : data.to, p.ts + maxSpan);
      const last = bands[bands.length - 1];
      if (last && last.status === p.status && last.to === p.ts) last.to = to;
      else bands.push({ from: p.ts, to, status: p.status, color: STATUS_COLOR[p.status] ?? STATUS_COLOR.unknown, label: t(STATUS_LABEL[p.status] ?? p.status) });
    });
  }
  const series = numeric ? [{ label: t("Wert"), points: points.map((p) => ({ ts: p.ts, value: p.value })) }] : [];

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" style={{ width: "min(780px, 100%)" }} onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h2>{check.name || check.type}</h2>
          <button className="modal-close" onClick={onClose} aria-label={t("Schließen")}>×</button>
        </header>
        <div className="inline-form" style={{ justifyContent: "space-between", alignItems: "center", gap: 10 }}>
          <div className="check-hist-now">
            <CheckStatusBadge status={check.status} />
            {numeric && check.value !== undefined && <strong>{fmtNum(check.value)}{unit ? ` ${unit}` : ""}</strong>}
            {check.output && <span className="muted small">{check.output}</span>}
          </div>
          <div className="chip-row">
            {RANGES.map((r) => (
              <label key={r.id} className={`chip${range === r.id ? " chip-on" : ""}`}>
                <input type="radio" name="check-range" checked={range === r.id} onChange={() => setRange(r.id)} />
                {t(r.label)}
              </label>
            ))}
          </div>
        </div>
        <div style={{ marginTop: 14 }}>
          {isLoading ? (
            <p className="muted small viz-empty">…</p>
          ) : points.length === 0 || !data ? (
            <p className="muted small viz-empty">{t("Noch keine Verlaufsdaten – je Checkin kommt ein Punkt dazu.")}</p>
          ) : numeric ? (
            <LineChart series={series} bands={bands} from={data.from} to={data.to} empty="" unit={unit} legend={false} />
          ) : (
            <StatusStrip bands={bands} from={data.from} to={data.to} t={t} />
          )}
        </div>
        {data && points.length > 0 && (
          <p className="muted small" style={{ marginTop: 10 }}>
            {!numeric ? t("Dieser Check liefert keinen Messwert – gezeigt wird der schlechteste Status je Zeitfenster.")
              : data.bucket_ms > 60_000 ? t("Werte sind je Zeitfenster gemittelt, der Status ist der schlechteste im Fenster.")
              : t("Ein Punkt je Checkin des Agents.")}
          </p>
        )}
      </div>
    </div>
  );
}

// StatusStrip: nur der Statusverlauf (für Checks ohne Messwert).
function StatusStrip({ bands, from, to, t }: { bands: Band[]; from: number; to: number; t: (k: string) => string }) {
  const span = Math.max(to - from, 1);
  const x = (ts: number) => ((ts - from) / span) * 1000;
  const fmt = (ts: number) => {
    const d = new Date(ts);
    return span <= 2 * 86_400_000 ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : d.toLocaleDateString([], { day: "2-digit", month: "2-digit" });
  };
  return (
    <div className="viz-wrap">
      <svg viewBox="0 0 1000 18" preserveAspectRatio="none" className="check-strip" role="img">
        {bands.map((b, i) => (
          <rect key={i} x={x(Math.max(b.from, from))} y={0} width={Math.max(1, x(Math.min(b.to, to)) - x(Math.max(b.from, from)))} height={18} fill={b.color}>
            <title>{b.label}</title>
          </rect>
        ))}
      </svg>
      <div className="muted small" style={{ display: "flex", justifyContent: "space-between", marginTop: 4 }}>
        <span>{fmt(from)}</span><span>{fmt(from + span / 2)}</span><span>{fmt(to)}</span>
      </div>
      <ul className="donut-legend viz-legend">
        {Object.entries(STATUS_COLOR).map(([k, c]) => (
          <li key={k}><span className="donut-swatch" style={{ background: c }} /><span className="donut-leg-name">{t(STATUS_LABEL[k])}</span></li>
        ))}
      </ul>
    </div>
  );
}
