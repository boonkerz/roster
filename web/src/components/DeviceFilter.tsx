import { useState } from "react";
import { useI18n } from "../i18n";
import type { Device } from "../types";

export interface FCond { field: string; op: string; value: string; }
export interface DFilter { match: "all" | "any"; conditions: FCond[]; }

type Kind = "text" | "num" | "enum";
const FIELDS: { v: string; label: string; kind: Kind; values?: string[] }[] = [
  { v: "hostname", label: "Hostname", kind: "text" },
  { v: "os", label: "OS", kind: "text" },
  { v: "os_version", label: "OS-Version", kind: "text" },
  { v: "agent_version", label: "Agent-Version", kind: "text" },
  { v: "client_name", label: "Client", kind: "text" },
  { v: "site_name", label: "Standort", kind: "text" },
  { v: "status", label: "Status", kind: "enum", values: ["online", "offline", "unknown"] },
  { v: "checks_failing", label: "Fehlerhafte Checks", kind: "num" },
  { v: "tasks_failing", label: "Fehlerhafte Tasks", kind: "num" },
  { v: "vuln_count", label: "Schwachstellen", kind: "num" },
  { v: "updates_count", label: "Offene Updates", kind: "num" },
];
const OPS: Record<Kind, string[]> = { text: ["contains", "eq", "ne"], num: ["gt", "lt", "eq"], enum: ["eq", "ne"] };
const OP_LABEL: Record<string, string> = { eq: "=", ne: "≠", contains: "enthält", gt: ">", lt: "<" };

const fieldOf = (v: string) => FIELDS.find((f) => f.v === v) ?? FIELDS[0];

// evalFilter prüft, ob ein Gerät die Filterbedingungen erfüllt (clientseitig).
export function evalFilter(d: Device, f: DFilter): boolean {
  if (!f.conditions.length) return true;
  const rec = d as unknown as Record<string, unknown>;
  const test = (c: FCond) => {
    const fld = fieldOf(c.field);
    if (fld.kind === "num") {
      const dv = Number(rec[c.field] ?? 0), cv = Number(c.value || 0);
      return c.op === "gt" ? dv > cv : c.op === "lt" ? dv < cv : dv === cv;
    }
    const dv = String(rec[c.field] ?? "").toLowerCase(), cv = c.value.toLowerCase();
    return c.op === "eq" ? dv === cv : c.op === "ne" ? dv !== cv : dv.includes(cv);
  };
  return f.match === "any" ? f.conditions.some(test) : f.conditions.every(test);
}

const STORE_KEY = "pcinv-saved-filters";
interface Saved { name: string; filter: DFilter; }
function loadSaved(): Saved[] { try { return JSON.parse(localStorage.getItem(STORE_KEY) || "[]"); } catch { return []; } }
function storeSaved(s: Saved[]) { localStorage.setItem(STORE_KEY, JSON.stringify(s)); }

// DeviceFilter ist ein Builder für eigene, benannte Geräte-Filter (clientseitig).
export function DeviceFilter({ value, onChange }: { value: DFilter; onChange: (f: DFilter) => void }) {
  const { t } = useI18n();
  const [saved, setSaved] = useState<Saved[]>(loadSaved);
  const [name, setName] = useState("");

  const setCond = (i: number, c: Partial<FCond>) =>
    onChange({ ...value, conditions: value.conditions.map((x, j) => (j === i ? { ...x, ...c } : x)) });
  const addCond = () => onChange({ ...value, conditions: [...value.conditions, { field: "hostname", op: "contains", value: "" }] });
  const delCond = (i: number) => onChange({ ...value, conditions: value.conditions.filter((_, j) => j !== i) });

  const save = () => {
    if (!name.trim()) return;
    const next = [...saved.filter((s) => s.name !== name.trim()), { name: name.trim(), filter: value }];
    setSaved(next); storeSaved(next); setName("");
  };
  const del = (n: string) => { const next = saved.filter((s) => s.name !== n); setSaved(next); storeSaved(next); };

  return (
    <div className="card filter-builder">
      <div className="inline-form" style={{ alignItems: "center", flexWrap: "wrap" }}>
        <strong>{t("Filter")}</strong>
        {saved.length > 0 && (
          <select value="" onChange={(e) => { const s = saved.find((x) => x.name === e.target.value); if (s) onChange(s.filter); }}>
            <option value="">{t("Gespeicherten Filter laden…")}</option>
            {saved.map((s) => <option key={s.name} value={s.name}>{s.name}</option>)}
          </select>
        )}
        <span className="muted small">{t("Treffer, wenn")}</span>
        <select value={value.match} onChange={(e) => onChange({ ...value, match: e.target.value as "all" | "any" })}>
          <option value="all">{t("alle")}</option>
          <option value="any">{t("beliebige")}</option>
        </select>
        <span className="muted small">{t("Bedingungen zutreffen:")}</span>
      </div>

      {value.conditions.map((c, i) => {
        const f = fieldOf(c.field);
        return (
          <div className="inline-form" key={i} style={{ marginTop: 6 }}>
            <select value={c.field} onChange={(e) => { const nf = fieldOf(e.target.value); setCond(i, { field: e.target.value, op: OPS[nf.kind][0], value: nf.values ? nf.values[0] : "" }); }}>
              {FIELDS.map((ff) => <option key={ff.v} value={ff.v}>{ff.label}</option>)}
            </select>
            <select value={c.op} onChange={(e) => setCond(i, { op: e.target.value })}>
              {OPS[f.kind].map((o) => <option key={o} value={o}>{OP_LABEL[o]}</option>)}
            </select>
            {f.kind === "enum" ? (
              <select value={c.value} onChange={(e) => setCond(i, { value: e.target.value })}>
                {f.values!.map((v) => <option key={v} value={v}>{v}</option>)}
              </select>
            ) : (
              <input value={c.value} type={f.kind === "num" ? "number" : "text"} placeholder={f.kind === "num" ? "0" : t("Wert")} onChange={(e) => setCond(i, { value: e.target.value })} />
            )}
            <button type="button" className="btn ghost sm" onClick={() => delCond(i)}>✕</button>
          </div>
        );
      })}

      <div className="inline-form" style={{ marginTop: 8, alignItems: "center", flexWrap: "wrap" }}>
        <button type="button" className="btn ghost sm" onClick={addCond}>+ {t("Bedingung")}</button>
        {value.conditions.length > 0 && <button type="button" className="btn ghost sm" onClick={() => onChange({ match: "all", conditions: [] })}>{t("Zurücksetzen")}</button>}
        <span className="spacer" style={{ flex: 1 }} />
        <input placeholder={t("Filtername")} value={name} onChange={(e) => setName(e.target.value)} style={{ minWidth: 140 }} />
        <button type="button" className="btn ghost sm" disabled={!name.trim() || !value.conditions.length} onClick={save}>{t("Speichern")}</button>
      </div>
      {saved.length > 0 && (
        <div className="inline-form small muted" style={{ marginTop: 6, flexWrap: "wrap" }}>
          {t("Gespeichert:")} {saved.map((s) => (
            <span key={s.name} className="filter-chip" style={{ cursor: "default" }}>
              <span style={{ cursor: "pointer" }} onClick={() => onChange(s.filter)}>{s.name}</span>
              <span style={{ cursor: "pointer", marginLeft: 6 }} onClick={() => del(s.name)}>✕</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
