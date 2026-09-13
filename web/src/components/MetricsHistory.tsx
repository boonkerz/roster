import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";

interface Point { ts: number; cpu: number; mem: number; disk: number; temp?: number; }

const W = 600, H = 150, PAD = 4;

// MetricsHistory zeigt CPU/RAM/Disk als Verlaufschart (24h/7d/30d).
export function MetricsHistory({ deviceId }: { deviceId: string }) {
  const { t } = useI18n();
  const [range, setRange] = useState<"24h" | "7d" | "30d">("24h");
  const { data } = useQuery({
    queryKey: ["metrics-history", deviceId, range],
    queryFn: () => api.get<Point[]>(`/devices/${deviceId}/metrics-history?range=${range}`),
    refetchInterval: 60000,
  });
  const points = data ?? [];

  const path = (key: "cpu" | "mem" | "disk") => {
    if (points.length < 2) return "";
    return points.map((p, i) => {
      const x = PAD + (i / (points.length - 1)) * (W - 2 * PAD);
      const y = PAD + (1 - Math.min(100, p[key]) / 100) * (H - 2 * PAD);
      return `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`;
    }).join(" ");
  };

  const fmt = (ms: number) => new Date(ms).toLocaleString([], range === "24h"
    ? { hour: "2-digit", minute: "2-digit" }
    : { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
  const yPct = (v: number) => ((PAD + (1 - v / 100) * (H - 2 * PAD)) / H) * 100;
  const last = points[points.length - 1];
  const val = (v?: number) => (v == null ? "–" : `${Math.round(v)} %`);

  return (
    <div className="metrics-history">
      <div className="inline-form" style={{ justifyContent: "space-between", alignItems: "center" }}>
        <div className="chart-legend">
          <span className="lg cpu">CPU {val(last?.cpu)}</span>
          <span className="lg mem">RAM {val(last?.mem)}</span>
          <span className="lg disk">Disk {val(last?.disk)}</span>
        </div>
        <select value={range} onChange={(e) => setRange(e.target.value as "24h" | "7d" | "30d")}>
          <option value="24h">{t("24 Stunden")}</option>
          <option value="7d">{t("7 Tage")}</option>
          <option value="30d">{t("30 Tage")}</option>
        </select>
      </div>
      {points.length < 2 ? (
        <p className="muted small">{t("Noch keine Verlaufsdaten (werden je Checkin gesammelt).")}</p>
      ) : (
        <>
          <div className="chart-area">
            <div className="chart-yaxis">
              {[100, 75, 50, 25, 0].map((v) => (
                <span key={v} style={{ top: `${yPct(v)}%` }}>{v} %</span>
              ))}
            </div>
            <svg viewBox={`0 0 ${W} ${H}`} className="chart" preserveAspectRatio="none">
              {[25, 50, 75].map((g) => {
                const y = PAD + (1 - g / 100) * (H - 2 * PAD);
                return <line key={g} x1={PAD} y1={y} x2={W - PAD} y2={y} className="grid" />;
              })}
              <path d={path("disk")} className="line disk" />
              <path d={path("mem")} className="line mem" />
              <path d={path("cpu")} className="line cpu" />
            </svg>
          </div>
          <div className="chart-x muted small">
            <span>{fmt(points[0].ts)}</span>
            <span>{fmt(points[points.length - 1].ts)}</span>
          </div>
          <TempChart points={points} fmt={fmt} />
        </>
      )}
    </div>
  );
}

// TempChart zeigt die Leittemperatur (heißester CPU-Sensor) als eigenes Diagramm –
// bewusst nicht in der %-Grafik, damit keine zwei Einheiten auf einer Achse landen.
// Die °C-Achse passt sich dem Wertebereich an; Lücken (keine Sensordaten) bleiben offen.
function TempChart({ points, fmt }: { points: Point[]; fmt: (ms: number) => string }) {
  const { t } = useI18n();
  const [hover, setHover] = useState<number | null>(null);
  const temps = points.map((p) => p.temp).filter((v): v is number => v != null);
  if (temps.length < 2) return null;

  // Achse: unten auf 10 °C abgerundet, drei gleiche Schritte in 5-°C-Stufen → runde Beschriftung.
  const lo = Math.max(0, Math.floor((Math.min(...temps) - 5) / 10) * 10);
  const step = Math.max(5, Math.ceil((Math.max(...temps) + 5 - lo) / 3 / 5) * 5);
  const hi = lo + 3 * step;
  const x = (i: number) => PAD + (i / (points.length - 1)) * (W - 2 * PAD);
  const y = (v: number) => PAD + (1 - (v - lo) / (hi - lo)) * (H - 2 * PAD);
  const ticks = [0, 1, 2, 3].map((k) => lo + step * k);

  let d = "";
  let pen = false;
  points.forEach((p, i) => {
    if (p.temp == null) { pen = false; return; }
    d += `${pen ? "L" : "M"}${x(i).toFixed(1)},${y(p.temp).toFixed(1)} `;
    pen = true;
  });

  // Auf den nächstgelegenen Punkt mit Messwert einrasten – auch mitten in einer Lücke.
  const onMove = (e: React.MouseEvent<SVGSVGElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    const at = Math.round(((e.clientX - r.left) / r.width) * (points.length - 1));
    for (let off = 0; off < points.length; off++) {
      for (const i of [at - off, at + off]) {
        if (i >= 0 && i < points.length && points[i].temp != null) { setHover(i); return; }
      }
    }
    setHover(null);
  };
  const last = [...points].reverse().find((p) => p.temp != null);
  const hp = hover != null ? points[hover] : null;

  return (
    <>
      <div className="chart-title">{t("Temperatur (heißester CPU-Sensor)")} · {last?.temp != null ? `${Math.round(last.temp)} °C` : "–"}</div>
      <div className="chart-area">
        <div className="chart-yaxis">
          {ticks.map((v) => <span key={v} style={{ top: `${(y(v) / H) * 100}%` }}>{v} °C</span>)}
        </div>
        <svg viewBox={`0 0 ${W} ${H}`} className="chart" preserveAspectRatio="none"
          onMouseMove={onMove} onMouseLeave={() => setHover(null)} role="img"
          aria-label={t("Temperaturverlauf")}>
          {ticks.slice(1, -1).map((v) => <line key={v} x1={PAD} y1={y(v)} x2={W - PAD} y2={y(v)} className="grid" />)}
          <path d={d} className="line temp" />
          {hp && <line x1={x(hover!)} y1={PAD} x2={x(hover!)} y2={H - PAD} className="crosshair" />}
        </svg>
        {hp && hp.temp != null && (
          <div className="chart-tip" style={{ left: `calc(40px + (100% - 40px) * ${(x(hover!) / W).toFixed(4)})` }}>
            {Math.round(hp.temp)} °C · {fmt(hp.ts)}
          </div>
        )}
      </div>
    </>
  );
}
