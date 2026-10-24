import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import { useI18n } from "../i18n";
import { usePref } from "../prefs";
import type { CheckEvent, DashChart, DashRange, DashWidget, DashWidth, StatSourceKind, StatSources, StatsResponse } from "../types";
import { BarChart, DonutChart, LineChart, seriesColor, type Datum, type Series } from "../charts";
import { CheckStatusBadge, relTime } from "./StatusBadge";

const RANGE_MS: Record<DashRange, number> = { "24h": 86_400_000, "7d": 7 * 86_400_000, "30d": 30 * 86_400_000, "90d": 90 * 86_400_000, "365d": 365 * 86_400_000 };
const RANGE_LABEL: Record<DashRange, string> = { "24h": "24 Stunden", "7d": "7 Tage", "30d": "30 Tage", "90d": "90 Tage", "365d": "1 Jahr" };
const SUMMARY_LABEL: Record<string, string> = {
  device_status: "Geräte-Status", devices_total: "Geräte gesamt", devices_online: "Online", devices_offline: "Offline", devices_unknown: "Unbekannt",
  failing_checks: "Checks fehlerhaft", failing_tasks: "Tasks fehlerhaft", pending_patches: "Ausstehende Patches", vulnerabilities: "Schwachstellen",
};
// Status-Farben sind reserviert (nie als Reihenfarbe) und stehen immer neben dem Label.
const STATUS_COLOR: Record<string, string> = { passing: "var(--viz-status-ok)", online: "var(--viz-status-ok)", warning: "var(--viz-status-warn)", failing: "var(--viz-status-bad)", offline: "var(--viz-status-bad)", unknown: "var(--viz-status-unknown)" };
const STATUS_LABEL: Record<string, string> = { passing: "OK", warning: "Warnung", failing: "Fehlerhaft", unknown: "Unbekannt", online: "Online", offline: "Offline" };
const MAX_SERIES = 7; // danach „Andere“ – mehr Farben lassen sich nicht sicher unterscheiden

const WIDTH_LABEL: Record<DashWidth, string> = { 1: "Viertel", 2: "Halb", 3: "Dreiviertel", 4: "Voll" };

// Standard-Kacheln für Nutzer ohne eigene Konfiguration: die bisherigen festen Bereiche.
const DEFAULT_WIDGETS: DashWidget[] = [
  { id: "status", title: "", source: "summary", key: "device_status", chart: "donut", range: "7d", width: 2 },
  { id: "events", title: "", source: "events", key: "recent", chart: "list", range: "7d", width: 2 },
];

const newID = () => Math.random().toString(36).slice(2, 10);

// sourceName liefert den Anzeigenamen einer Quelle (für Kacheltitel und Auswahl).
function sourceName(w: Pick<DashWidget, "source" | "key">, src: StatSources | undefined, t: (s: string) => string): string {
  if (w.source === "events") return t("Letzte Status-Wechsel");
  if (w.source === "summary") return t(SUMMARY_LABEL[w.key] ?? w.key);
  const list = w.source === "check" ? src?.checks : src?.fields;
  return list?.find((s) => s.id === w.key)?.name ?? w.key;
}

// labelFor macht Rohlabels lesbar: Check-/Gerätestatus übersetzt, Checkbox-Felder als Ja/Nein.
function labelFor(w: DashWidget, raw: string, src: StatSources | undefined, t: (s: string) => string): string {
  if (w.source === "check" || (w.source === "summary" && w.key === "device_status")) return t(STATUS_LABEL[raw] ?? raw);
  if (w.source === "field" && src?.fields.find((f) => f.id === w.key)?.type === "checkbox") return raw === "true" ? t("Ja") : t("Nein");
  if (w.source === "summary") return t(SUMMARY_LABEL[w.key] ?? w.key);
  return raw || "—";
}

// topN behält die größten Einträge und fasst den Rest als „Andere“ zusammen.
function topN<T extends { value: number }>(items: T[], n: number, other: (sum: number) => T): T[] {
  if (items.length <= n) return items;
  const sorted = [...items].sort((a, b) => b.value - a.value);
  const keep = sorted.slice(0, n), rest = sorted.slice(n);
  return [...keep, other(rest.reduce((a, x) => a + x.value, 0))];
}

// EventsList sind die letzten Check-Status-Wechsel (vorher fester Bereich der Übersicht).
function EventsList({ events }: { events: CheckEvent[] }) {
  const { t } = useI18n();
  const nav = useNavigate();
  if (events.length === 0) return <p className="muted viz-empty">{t("Keine Statuswechsel.")}</p>;
  return (
    <div className="scroll-list" style={{ maxHeight: 300 }}>
      <table className="table">
        <tbody>
          {events.map((e) => (
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
  );
}

function Widget({ w, sources, events, onEdit, onRemove }: { w: DashWidget; sources?: StatSources; events: CheckEvent[]; onEdit: () => void; onRemove: () => void }) {
  const { t } = useI18n();
  const isLine = w.chart === "line";
  const since = isLine ? Date.now() - RANGE_MS[w.range] : 0;
  const { data, isLoading, isError } = useQuery({
    queryKey: ["dash-stats", w.source, w.key, isLine ? w.range : ""],
    queryFn: () => api.get<StatsResponse>(`/dashboard/stats?source=${encodeURIComponent(w.source)}&key=${encodeURIComponent(w.key)}${isLine ? `&since=${since}` : ""}`),
    refetchInterval: 60_000,
    retry: false,
    enabled: w.source !== "events",
  });
  const statusColored = w.source === "check" || (w.source === "summary" && w.key === "device_status");
  const title = w.title.trim() || sourceName(w, sources, t);

  const current: Datum[] = useMemo(() => {
    const items = (data?.current ?? []).filter((c) => c.value > 0).map((c) => ({
      label: labelFor(w, c.label, sources, t), value: c.value, color: statusColored ? STATUS_COLOR[c.label] : undefined,
    }));
    const limit = w.chart === "bar" ? 12 : MAX_SERIES;
    const folded = topN(items, limit, (sum) => ({ label: t("Andere"), value: sum, color: "var(--viz-status-unknown)" }));
    return statusColored ? folded.sort((a, b) => b.value - a.value) : folded.map((d, i) => ({ ...d, color: d.color ?? seriesColor(i) }));
  }, [data, w, sources, t, statusColored]);

  const series: Series[] = useMemo(() => {
    const byLabel = new Map<string, { ts: number; value: number }[]>();
    for (const p of data?.history ?? []) {
      const arr = byLabel.get(p.label) ?? [];
      arr.push({ ts: p.ts, value: p.value });
      byLabel.set(p.label, arr);
    }
    const all = [...byLabel.entries()]
      .map(([raw, points]) => ({ raw, points: points.sort((a, b) => a.ts - b.ts) }))
      .sort((a, b) => (b.points[b.points.length - 1]?.value ?? 0) - (a.points[a.points.length - 1]?.value ?? 0));
    const kept = all.length > MAX_SERIES + 1 ? all.slice(0, MAX_SERIES) : all;
    const out: Series[] = kept.map((s, i) => ({
      label: labelFor(w, s.raw, sources, t), points: s.points,
      color: statusColored ? (STATUS_COLOR[s.raw] ?? "var(--viz-status-unknown)") : seriesColor(i),
    }));
    if (kept.length < all.length) {
      // „Andere“ im Verlauf: Summe der weggefalteten Reihen je Zeitpunkt.
      const sums = new Map<number, number>();
      for (const s of all.slice(kept.length)) for (const p of s.points) sums.set(p.ts, (sums.get(p.ts) ?? 0) + p.value);
      out.push({ label: t("Andere"), color: "var(--viz-status-unknown)", points: [...sums.entries()].map(([ts, value]) => ({ ts, value })).sort((a, b) => a.ts - b.ts) });
    }
    return out;
  }, [data, w, sources, t, statusColored]);

  return (
    <section className="card dash-widget">
      <header className="dash-widget-head">
        <span className="dash-handle" title={t("Ziehen zum Verschieben")}>⠿</span>
        <h2 title={title}>{title}</h2>
        {isLine && <span className="muted small">{t(RANGE_LABEL[w.range])}</span>}
        <span className="spacer" />
        <button className="btn ghost sm" onClick={onEdit} title={t("Bearbeiten")}>✎</button>
        <button className="btn ghost sm" onClick={onRemove} title={t("Entfernen")}>✕</button>
      </header>
      {w.source === "events" ? <EventsList events={events} />
        : isLoading ? <p className="muted small viz-empty">{t("Lädt…")}</p>
        : isError ? <p className="muted small viz-empty">{t("Daten konnten nicht geladen werden.")}</p>
        : w.chart === "bar" ? <BarChart data={current} empty={t("Noch keine Werte.")} />
        : w.chart === "donut" ? <DonutChart data={current} empty={t("Noch keine Werte.")} />
        : <LineChart series={series} from={since} to={Date.now()} legend={series.length > 1 || (series.length === 1 && series[0].label !== title)}
            empty={t("Noch keine Historie – der erste Snapshot entsteht zur nächsten vollen Stunde.")} />}
    </section>
  );
}

function WidgetDialog({ initial, isNew, sources, onSave, onClose }: { initial: DashWidget; isNew: boolean; sources?: StatSources; onSave: (w: DashWidget) => void; onClose: () => void }) {
  const { t } = useI18n();
  const [w, setW] = useState<DashWidget>(initial);
  const keys = w.source === "summary" ? (sources?.summary ?? []).map((k) => ({ id: k, name: t(SUMMARY_LABEL[k] ?? k) }))
    : w.source === "check" ? (sources?.checks ?? []) : (sources?.fields ?? []);
  const setSource = (source: StatSourceKind) => {
    if (source === "events") { setW({ ...w, source, key: "recent", chart: "list" }); return; }
    const first = source === "summary" ? (sources?.summary[0] ?? "device_status") : source === "check" ? (sources?.checks[0]?.id ?? "") : (sources?.fields[0]?.id ?? "");
    setW({ ...w, source, key: first, chart: w.chart === "list" ? "donut" : w.chart });
  };
  const isEvents = w.source === "events";
  const srcLabel: Record<StatSourceKind, string> = { summary: "Kennzahl", check: "Check", field: "Feld", events: "Status-Wechsel" };
  const chartHint: Partial<Record<DashChart, string>> = {
    bar: "Aktuelle Verteilung als Balken – gut für viele Werte, z. B. Versionen.",
    donut: "Aktuelle Anteile als Ring – gut für wenige Kategorien, z. B. Check-Status.",
    line: "Verlauf über die Zeit – je Wert eine Linie, stündliche Snapshots.",
  };
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" style={{ width: "min(560px, 100%)" }} onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h2>{isNew ? t("Diagramm hinzufügen") : t("Diagramm bearbeiten")}</h2>
          <button className="modal-close" onClick={onClose} aria-label={t("Schließen")}>×</button>
        </header>
        <div className="form-grid" style={{ marginTop: 10 }}>
          <div>{t("Quelle")}
            <div className="chip-row" style={{ marginTop: 6 }}>
              {(["summary", "check", "field", "events"] as StatSourceKind[]).map((s) => (
                <label key={s} className={`chip${w.source === s ? " chip-on" : ""}`}>
                  <input type="radio" name="src" checked={w.source === s} onChange={() => setSource(s)} />
                  {t(srcLabel[s])}
                </label>
              ))}
            </div>
          </div>
          {!isEvents && <label>{t(srcLabel[w.source])}
            <select value={w.key} onChange={(e) => setW({ ...w, key: e.target.value })}>
              {keys.length === 0 && <option value="">{w.source === "check" ? t("— keine Checks —") : t("— keine Felder —")}</option>}
              {keys.map((k) => <option key={k.id} value={k.id}>{k.name}</option>)}
            </select>
          </label>}
          {!isEvents && <div>{t("Darstellung")}
            <div className="chip-row" style={{ marginTop: 6 }}>
              {(["bar", "donut", "line"] as DashChart[]).map((c) => (
                <label key={c} className={`chip${w.chart === c ? " chip-on" : ""}`}>
                  <input type="radio" name="chart" checked={w.chart === c} onChange={() => setW({ ...w, chart: c })} />
                  {c === "bar" ? t("Balken") : c === "donut" ? t("Ring") : t("Verlauf")}
                </label>
              ))}
            </div>
            <div className="muted small" style={{ marginTop: 6 }}>{t(chartHint[w.chart] ?? "")}</div>
          </div>}
          {w.chart === "line" && (
            <label>{t("Zeitraum")}
              <select value={w.range} onChange={(e) => setW({ ...w, range: e.target.value as DashRange })}>
                {(Object.keys(RANGE_LABEL) as DashRange[]).map((r) => <option key={r} value={r}>{t(RANGE_LABEL[r])}</option>)}
              </select>
            </label>
          )}
          <div>{t("Breite")}
            <div className="chip-row" style={{ marginTop: 6 }}>
              {([1, 2, 3, 4] as DashWidth[]).map((n) => (
                <label key={n} className={`chip${(w.width ?? 2) === n ? " chip-on" : ""}`}>
                  <input type="radio" name="width" checked={(w.width ?? 2) === n} onChange={() => setW({ ...w, width: n })} />
                  {t(WIDTH_LABEL[n])}
                </label>
              ))}
            </div>
          </div>
          <label>{t("Titel")} <span className="muted small">({t("optional")})</span>
            <input value={w.title} placeholder={sourceName(w, sources, t)} onChange={(e) => setW({ ...w, title: e.target.value })} />
          </label>
        </div>
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 10, marginTop: 20 }}>
          <button className="btn ghost" onClick={onClose}>{t("Abbrechen")}</button>
          <button className="btn primary" disabled={!w.key} onClick={() => onSave({ ...w, width: w.width ?? 2 })}>{t("Speichern")}</button>
        </div>
      </div>
    </div>
  );
}

// DashWidgets ist der Bereich „Eigene Diagramme“ der Übersicht: frei konfigurierbare
// Kacheln über Kennzahlen, Check-Status und benutzerdefinierte Felder, gespeichert im
// Benutzerkonto (folgen dem Nutzer auf jeden Browser).
export function DashWidgets({ events }: { events: CheckEvent[] }) {
  const { t } = useI18n();
  const [widgets, setWidgets] = usePref<DashWidget[]>("dashboard.widgets", DEFAULT_WIDGETS);
  const [editing, setEditing] = useState<{ w: DashWidget; isNew: boolean } | null>(null);
  const [drag, setDrag] = useState<number | null>(null);
  const [over, setOver] = useState<number | null>(null);
  const { data: sources } = useQuery({
    queryKey: ["dash-stat-sources"],
    queryFn: () => api.get<StatSources>("/dashboard/stats/sources"),
    staleTime: 60_000,
    retry: false,
  });
  const add = () => setEditing({ w: { id: newID(), title: "", source: "summary", key: "device_status", chart: "donut", range: "7d", width: 2 }, isNew: true });
  const save = (w: DashWidget) => {
    setWidgets(widgets.some((x) => x.id === w.id) ? widgets.map((x) => (x.id === w.id ? w : x)) : [...widgets, w]);
    setEditing(null);
  };
  // Drag-and-drop: die gezogene Kachel wird an der Stelle der Zielkachel eingefügt.
  const drop = (to: number) => {
    if (drag === null || drag === to) { setDrag(null); setOver(null); return; }
    const next = [...widgets];
    const [moved] = next.splice(drag, 1);
    next.splice(to, 0, moved);
    setWidgets(next); setDrag(null); setOver(null);
  };
  return (
    <>
      <div className="dash-widgets-head">
        <h2>{t("Eigene Diagramme")}</h2>
        <span className="muted small">{t("Kennzahlen, Check-Status, Feldwerte und Status-Wechsel – aktuell oder im Verlauf. Kacheln lassen sich ziehen.")}</span>
        <span className="spacer" />
        <button className="btn ghost sm" onClick={add}>+ {t("Diagramm")}</button>
      </div>
      {widgets.length === 0 ? (
        <section className="card dash-widget-empty">
          <p className="muted">{t("Noch keine Diagramme. Lege z. B. eine Verteilung der Docker-Image-Versionen aus einem Feld an, den Status eines Checks über alle Geräte oder den Verlauf der fehlerhaften Checks.")}</p>
          <button className="btn primary" onClick={add}>+ {t("Diagramm hinzufügen")}</button>
        </section>
      ) : (
        <div className="dash-widgets">
          {widgets.map((w, i) => (
            <div key={w.id} className={`dash-widget-slot dash-w${w.width ?? 2}${drag === i ? " dragging" : ""}${over === i && drag !== i ? " drag-over" : ""}`}
              draggable onDragStart={(e) => { setDrag(i); e.dataTransfer.effectAllowed = "move"; }}
              onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = "move"; if (over !== i) setOver(i); }}
              onDragLeave={() => { if (over === i) setOver(null); }}
              onDrop={(e) => { e.preventDefault(); drop(i); }} onDragEnd={() => { setDrag(null); setOver(null); }}>
              <Widget w={w} sources={sources} events={events} onEdit={() => setEditing({ w, isNew: false })} onRemove={() => setWidgets(widgets.filter((x) => x.id !== w.id))} />
            </div>
          ))}
        </div>
      )}
      {editing && <WidgetDialog initial={editing.w} isNew={editing.isNew} sources={sources} onSave={save} onClose={() => setEditing(null)} />}
    </>
  );
}
