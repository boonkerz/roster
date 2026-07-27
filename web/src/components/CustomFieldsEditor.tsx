import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { CustomFieldValue } from "../types";
import { useAuth } from "../auth";

type Val = string | string[];

// parseValue wandelt den gespeicherten String je Typ in den UI-Wert.
function parseValue(cv: CustomFieldValue): Val {
  if (cv.field.type === "list" || cv.field.type === "multiselect") {
    try { const a = JSON.parse(cv.value || "[]"); return Array.isArray(a) ? a.map(String) : []; }
    catch { return cv.value ? [cv.value] : []; }
  }
  return cv.value ?? "";
}

// CustomFieldsEditor zeigt und bearbeitet benutzerdefinierte Felder einer Entität.
export function CustomFieldsEditor({ model, entityId }: { model: "client" | "site" | "device"; entityId: string }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { user } = useAuth();
  const canEdit = user?.role === "admin";
  const key = ["custom-field-values", model, entityId];
  const { data } = useQuery({ queryKey: key, queryFn: () => api.get<CustomFieldValue[]>(`/custom-field-values?model=${model}&entity_id=${entityId}`) });
  const [vals, setVals] = useState<Record<string, Val>>({});

  useEffect(() => {
    if (data) {
      const m: Record<string, Val> = {};
      for (const cv of data) m[cv.field.id] = parseValue(cv);
      setVals(m);
    }
  }, [data]);

  const save = useMutation({
    mutationFn: () => api.put("/custom-field-values", { model, entity_id: entityId, values: vals }),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
  });

  if (!data) return <div className="muted small">{t("Lädt…")}</div>;
  if (data.length === 0) return <p className="muted">{t("Keine Felder für diese Ebene definiert (unter „Einstellungen → Benutzerdefinierte Felder“).")}</p>;

  const set = (id: string, v: Val) => setVals((s) => ({ ...s, [id]: v }));

  // Name->ID-Map und die Menge der Auswahl-Begleitfelder (werden über die Checkboxen
  // der jeweiligen Quell-Liste gepflegt und daher nicht separat angezeigt).
  const byName: Record<string, string> = {};
  for (const cv of data) byName[cv.field.name] = cv.field.id;
  const selectionTargets = new Set(data.map((cv) => cv.field.selection_field).filter(Boolean) as string[]);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {data.map((cv) => {
        const f = cv.field;
        if (selectionTargets.has(f.name)) return null; // Begleitfeld: via Checkboxen gepflegt
        const v = vals[f.id] ?? (f.type === "list" || f.type === "multiselect" ? [] : "");
        return (
          <label key={f.id} className="field">
            <span className="muted small">{f.name}{f.required ? " *" : ""}</span>
            {f.type === "checkbox" ? (
              <input type="checkbox" disabled={!canEdit} checked={v === "true"} onChange={(e) => set(f.id, e.target.checked ? "true" : "false")} />
            ) : f.type === "number" ? (
              <input type="number" disabled={!canEdit} value={v as string} onChange={(e) => set(f.id, e.target.value)} />
            ) : f.type === "datetime" ? (
              <input type="datetime-local" disabled={!canEdit} value={v as string} onChange={(e) => set(f.id, e.target.value)} />
            ) : f.type === "select" ? (
              <select disabled={!canEdit} value={v as string} onChange={(e) => set(f.id, e.target.value)}>
                <option value="">—</option>
                {f.options.map((o) => <option key={o} value={o}>{o}</option>)}
              </select>
            ) : f.type === "multiselect" ? (
              <div className="chip-row">
                {f.options.map((o) => {
                  const arr = v as string[];
                  return (
                    <label key={o} className="chip">
                      <input type="checkbox" disabled={!canEdit} checked={arr.includes(o)}
                        onChange={(e) => set(f.id, e.target.checked ? [...arr, o] : arr.filter((x) => x !== o))} /> {o}
                    </label>
                  );
                })}
              </div>
            ) : f.type === "list" && f.selection_field ? (
              <SelectableList source={v as string[]} link={f.link}
                selected={(vals[byName[f.selection_field]] as string[]) ?? []}
                disabled={!canEdit}
                onChange={(a) => { const tid = byName[f.selection_field!]; if (tid) set(tid, a); }} />
            ) : f.type === "list" ? (
              <ListInput value={v as string[]} disabled={!canEdit || f.managed} link={f.link} onChange={(a) => set(f.id, a)} />
            ) : (
              <input type="text" disabled={!canEdit} value={v as string} onChange={(e) => set(f.id, e.target.value)} />
            )}
          </label>
        );
      })}
      {canEdit && (
        <div>
          <button className="btn primary" onClick={() => save.mutate()} disabled={save.isPending}>{t("Speichern")}</button>
          {save.isSuccess && <span className="muted small" style={{ marginLeft: 10 }}>{t("gespeichert ✓")}</span>}
        </div>
      )}
    </div>
  );
}

// linkHref macht aus einer Domain/URL eine absolute https-URL.
function linkHref(v: string): string {
  return /^https?:\/\//i.test(v) ? v : "https://" + v;
}

// entryLabel rendert einen Eintrag – optional als Link.
function entryLabel(x: string, link?: boolean) {
  return link ? <a href={linkHref(x)} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}>{x}</a> : <>{x}</>;
}

// ListInput: frei eingebbare Einträge (Chips zum Hinzufügen/Entfernen). Bei disabled
// nur Anzeige (z.B. agent-verwaltet). link => Einträge als Links.
function ListInput({ value, onChange, disabled, link }: { value: string[]; onChange: (a: string[]) => void; disabled?: boolean; link?: boolean }) {
  const [text, setText] = useState("");
  const add = () => { const t = text.trim(); if (t && !value.includes(t)) { onChange([...value, t]); setText(""); } };
  return (
    <div>
      <div className="chip-row">
        {value.map((x) => (
          <span key={x} className="chip">{entryLabel(x, link)}{!disabled && <button className="chip-x" onClick={() => onChange(value.filter((y) => y !== x))}>×</button>}</span>
        ))}
        {value.length === 0 && <span className="muted small">—</span>}
      </div>
      {!disabled && (
        <div className="inline-form" style={{ marginTop: 6 }}>
          <input value={text} placeholder="Eintrag…" onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } }} />
          <button className="btn" onClick={add} disabled={!text.trim()}>+ Hinzufügen</button>
        </div>
      )}
    </div>
  );
}

// SelectableList: read-only Quell-Einträge mit Checkbox; angehakte landen in `selected`
// (dem Begleitfeld). Optional als Links.
function SelectableList({ source, selected, onChange, link, disabled }: {
  source: string[]; selected: string[]; onChange: (a: string[]) => void; link?: boolean; disabled?: boolean;
}) {
  const toggle = (d: string, on: boolean) => onChange(on ? [...selected, d] : selected.filter((x) => x !== d));
  return (
    <div className="chip-row" style={{ flexDirection: "column", alignItems: "flex-start", gap: 4 }}>
      {source.length === 0 && <span className="muted small">—</span>}
      {source.map((d) => (
        <span key={d} className="chip">
          <input type="checkbox" disabled={disabled} checked={selected.includes(d)}
            onChange={(e) => toggle(d, e.target.checked)} style={{ marginRight: 6 }} />
          {entryLabel(d, link)}
        </span>
      ))}
    </div>
  );
}
