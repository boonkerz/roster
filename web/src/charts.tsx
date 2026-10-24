import { useEffect, useMemo, useRef, useState } from "react";

// Kleine, abhängigkeitsfreie SVG-Diagramme für das Dashboard. Farben kommen aus den
// CSS-Variablen --series-1…8 (kategorisch, feste Reihenfolge) bzw. --viz-status-* –
// beide sind je Theme in styles.css definiert und auf Farbfehlsichtigkeit geprüft.

export interface Datum { label: string; value: number; color?: string }
export interface Series { label: string; color?: string; points: { ts: number; value: number }[] }

export const seriesColor = (i: number) => `var(--series-${(i % 8) + 1})`;
export const fmtNum = (v: number) => (Number.isInteger(v) ? String(v) : v.toFixed(1));

// useWidth misst die Breite des umgebenden Elements, damit Balken und Verläufe die Kachel
// genau ausfüllen (statt ein festes Koordinatensystem zu skalieren).
function useWidth(min = 260): [React.RefObject<HTMLDivElement>, number] {
  const ref = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const update = () => setW(Math.floor(el.getBoundingClientRect().width));
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, Math.max(w, min)];
}

// Tooltip ist die schwebende Werteanzeige über einem Diagramm.
function Tooltip({ left, children }: { left: string; children: React.ReactNode }) {
  return <div className="viz-tip" style={{ left }}>{children}</div>;
}

// BarChart: horizontale Balken, eine Farbe (die Identität steht am Balken, nicht in der Farbe).
export function BarChart({ data, empty }: { data: Datum[]; empty: string }) {
  const [hover, setHover] = useState<number | null>(null);
  const [ref, w] = useWidth();
  if (data.length === 0) return <p className="muted small viz-empty">{empty}</p>;
  const max = Math.max(...data.map((d) => d.value), 1);
  const rowH = 26, labelW = Math.min(150, Math.round(w * 0.3)), h = data.length * rowH + 4;
  return (
    <div className="viz-wrap" ref={ref}>
      <svg viewBox={`0 0 ${w} ${h}`} width={w} height={h} className="viz-svg" role="img">
        {data.map((d, i) => {
          const bw = Math.max(((w - labelW - 50) * d.value) / max, d.value > 0 ? 3 : 0);
          const y = i * rowH + 2;
          return (
            <g key={d.label} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)}>
              <rect x={0} y={y} width={w} height={rowH} fill="transparent" />
              <text x={labelW - 10} y={y + rowH / 2 + 4} textAnchor="end" className="viz-label">
                {d.label.length > 22 ? d.label.slice(0, 21) + "…" : d.label}
              </text>
              <rect x={labelW} y={y + 5} width={bw} height={rowH - 10} rx={3} fill={d.color ?? seriesColor(0)} opacity={hover === null || hover === i ? 1 : 0.55} />
              <text x={labelW + bw + 8} y={y + rowH / 2 + 4} className="viz-value">{fmtNum(d.value)}</text>
              <title>{d.label}: {fmtNum(d.value)}</title>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

// DonutChart: Anteile mit Legende; Farben folgen der Reihenfolge der Daten.
export function DonutChart({ data, empty }: { data: Datum[]; empty: string }) {
  const [hover, setHover] = useState<number | null>(null);
  const total = data.reduce((a, d) => a + d.value, 0);
  if (data.length === 0 || total === 0) return <p className="muted small viz-empty">{empty}</p>;
  const r = 60, cx = 70, cy = 70, stroke = 22, circ = 2 * Math.PI * r;
  let acc = 0;
  const gap = 2; // 2px Oberflächen-Lücke zwischen Segmenten
  return (
    <div className="viz-donut">
      <svg viewBox="0 0 140 140" width={140} height={140} role="img">
        {data.map((d, i) => {
          const len = (d.value / total) * circ;
          const off = acc; acc += len;
          const dash = Math.max(len - (data.length > 1 ? gap : 0), 0);
          return (
            <circle key={d.label} cx={cx} cy={cy} r={r} fill="none" stroke={d.color ?? seriesColor(i)} strokeWidth={hover === i ? stroke + 4 : stroke}
              strokeDasharray={`${dash} ${circ - dash}`} strokeDashoffset={-off} transform={`rotate(-90 ${cx} ${cy})`}
              opacity={hover === null || hover === i ? 1 : 0.5} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)}>
              <title>{d.label}: {fmtNum(d.value)} ({Math.round((d.value / total) * 100)}%)</title>
            </circle>
          );
        })}
        <text x={cx} y={cy + 1} textAnchor="middle" className="viz-center">{hover === null ? fmtNum(total) : fmtNum(data[hover].value)}</text>
        <text x={cx} y={cy + 16} textAnchor="middle" className="viz-center-sub">{hover === null ? "" : `${Math.round((data[hover].value / total) * 100)}%`}</text>
      </svg>
      <ul className="donut-legend">
        {data.map((d, i) => (
          <li key={d.label} onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)} style={{ opacity: hover === null || hover === i ? 1 : 0.6 }}>
            <span className="donut-swatch" style={{ background: d.color ?? seriesColor(i) }} />
            <span className="donut-leg-name" title={d.label}>{d.label}</span>
            <span className="muted small">{fmtNum(d.value)}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

// LineChart: Verlauf mehrerer Reihen über eine gemeinsame Zeitachse, mit Fadenkreuz-Tooltip.
export function LineChart({ series, from, to, empty, legend }: { series: Series[]; from: number; to: number; empty: string; legend?: boolean }) {
  const [hoverTs, setHoverTs] = useState<number | null>(null);
  const [ref, w] = useWidth();
  const h = 190, padL = 38, padR = 10, padT = 8, padB = 22;
  const allTs = useMemo(() => Array.from(new Set(series.flatMap((s) => s.points.map((p) => p.ts)))).sort((a, b) => a - b), [series]);
  if (series.length === 0 || allTs.length === 0) return <p className="muted small viz-empty">{empty}</p>;
  const max = Math.max(1, ...series.flatMap((s) => s.points.map((p) => p.value)));
  const niceMax = niceCeil(max);
  const x = (ts: number) => padL + ((ts - from) / Math.max(to - from, 1)) * (w - padL - padR);
  const y = (v: number) => padT + (1 - v / niceMax) * (h - padT - padB);
  // Achsenwerte bleiben ganzzahlig (es sind Anzahlen): bei Maximum 1 nur 0 und 1.
  const ticks = niceMax < 2 ? [0, niceMax] : [0, Math.round(niceMax / 2), niceMax];
  const span = to - from;
  const fmtTs = (ts: number) => {
    const d = new Date(ts);
    return span <= 2 * 86_400_000 ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : d.toLocaleDateString([], { day: "2-digit", month: "2-digit" });
  };
  const onMove = (e: React.MouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = ((e.clientX - rect.left) / rect.width) * w;
    const ts = from + ((px - padL) / (w - padL - padR)) * span;
    let best = allTs[0];
    for (const t of allTs) if (Math.abs(t - ts) < Math.abs(best - ts)) best = t;
    setHoverTs(best);
  };
  const hoverVals = hoverTs === null ? [] : series.map((s) => ({ s, v: s.points.find((p) => p.ts === hoverTs)?.value }));
  const tipX = hoverTs === null ? 0 : (x(hoverTs) / w) * 100;
  return (
    <div className="viz-wrap" ref={ref}>
      <svg viewBox={`0 0 ${w} ${h}`} width={w} height={h} className="viz-svg" role="img" onMouseMove={onMove} onMouseLeave={() => setHoverTs(null)}>
        {ticks.map((tv) => (
          <g key={tv}>
            <line x1={padL} x2={w - padR} y1={y(tv)} y2={y(tv)} className="viz-grid" />
            <text x={padL - 6} y={y(tv) + 4} textAnchor="end" className="viz-axis">{fmtNum(tv)}</text>
          </g>
        ))}
        {[from, from + span / 2, to].map((ts, i) => (
          <text key={i} x={x(ts)} y={h - 6} textAnchor={i === 0 ? "start" : i === 2 ? "end" : "middle"} className="viz-axis">{fmtTs(ts)}</text>
        ))}
        {series.map((s, i) => {
          const pts = [...s.points].sort((a, b) => a.ts - b.ts);
          if (pts.length === 1) {
            return <circle key={s.label} cx={x(pts[0].ts)} cy={y(pts[0].value)} r={4} fill={s.color ?? seriesColor(i)} />;
          }
          const d = pts.map((p, k) => `${k === 0 ? "M" : "L"}${x(p.ts).toFixed(1)},${y(p.value).toFixed(1)}`).join(" ");
          return <path key={s.label} d={d} fill="none" stroke={s.color ?? seriesColor(i)} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />;
        })}
        {hoverTs !== null && (
          <g>
            <line x1={x(hoverTs)} x2={x(hoverTs)} y1={padT} y2={h - padB} className="viz-cross" />
            {hoverVals.map(({ s, v }, i) => v !== undefined && (
              <circle key={s.label} cx={x(hoverTs)} cy={y(v)} r={4} fill={s.color ?? seriesColor(i)} stroke="var(--surface)" strokeWidth={2} />
            ))}
          </g>
        )}
      </svg>
      {hoverTs !== null && (
        <Tooltip left={`${Math.min(tipX, 70)}%`}>
          <div className="muted small">{new Date(hoverTs).toLocaleString()}</div>
          {hoverVals.map(({ s, v }, i) => (
            <div key={s.label} className="viz-tip-row">
              <span className="donut-swatch" style={{ background: s.color ?? seriesColor(i) }} />
              <span className="donut-leg-name">{s.label}</span>
              <span>{v === undefined ? "–" : fmtNum(v)}</span>
            </div>
          ))}
        </Tooltip>
      )}
      {(legend ?? series.length > 1) && (
        <ul className="donut-legend viz-legend">
          {series.map((s, i) => (
            <li key={s.label}>
              <span className="donut-swatch" style={{ background: s.color ?? seriesColor(i) }} />
              <span className="donut-leg-name" title={s.label}>{s.label}</span>
              <span className="muted small">{fmtNum(s.points[s.points.length - 1]?.value ?? 0)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// niceCeil rundet den Achsen-Maximalwert auf 1/2/5·10^n auf.
function niceCeil(v: number): number {
  if (v <= 0) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 5, 10]) if (m * p >= v) return m * p;
  return 10 * p;
}
