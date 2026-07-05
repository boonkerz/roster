import { FormEvent, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { Group } from "../types";
import { useAuth } from "../auth";

interface Cond { field: string; op: string; value: string; }
interface Rule { match: "all" | "any"; conditions: Cond[]; }

const FIELDS: { v: string; label: string; ops: string[]; values?: string[]; number?: boolean }[] = [
  { v: "os", label: "OS", ops: ["eq", "ne", "contains"] },
  { v: "os_version", label: "OS-Version", ops: ["eq", "ne", "contains"] },
  { v: "hostname", label: "Hostname", ops: ["eq", "ne", "contains"] },
  { v: "agent_version", label: "Agent-Version", ops: ["eq", "ne", "contains"] },
  { v: "vendor", label: "Hersteller", ops: ["eq", "ne", "contains"] },
  { v: "model", label: "Modell", ops: ["eq", "ne", "contains"] },
  { v: "status", label: "Status", ops: ["eq"], values: ["online", "offline"] },
  { v: "updates_count", label: "Offene Updates", ops: ["gt", "lt", "eq"], number: true },
];
const OP_LABEL: Record<string, string> = { eq: "=", ne: "≠", contains: "enthält", gt: ">", lt: "<" };

// RuleEditor bearbeitet eine Smart-Group-Regel (Bedingungen mit UND/ODER).
function RuleEditor({ rule, onChange }: { rule: Rule; onChange: (r: Rule) => void }) {
  const { t } = useI18n();
  const setCond = (i: number, c: Partial<Cond>) =>
    onChange({ ...rule, conditions: rule.conditions.map((x, j) => (j === i ? { ...x, ...c } : x)) });
  const fieldOf = (name: string) => FIELDS.find((f) => f.v === name) ?? FIELDS[0];

  return (
    <div className="rule-editor">
      <div className="inline-form">
        <span className="muted small">{t("Treffer, wenn")}</span>
        <select value={rule.match} onChange={(e) => onChange({ ...rule, match: e.target.value as "all" | "any" })}>
          <option value="all">{t("alle")}</option>
          <option value="any">{t("beliebige")}</option>
        </select>
        <span className="muted small">{t("Bedingungen zutreffen:")}</span>
      </div>
      {rule.conditions.map((c, i) => {
        const f = fieldOf(c.field);
        return (
          <div className="inline-form" key={i} style={{ marginTop: 6 }}>
            <select value={c.field} onChange={(e) => { const nf = fieldOf(e.target.value); setCond(i, { field: e.target.value, op: nf.ops[0], value: nf.values ? nf.values[0] : "" }); }}>
              {FIELDS.map((ff) => <option key={ff.v} value={ff.v}>{ff.label}</option>)}
            </select>
            <select value={c.op} onChange={(e) => setCond(i, { op: e.target.value })}>
              {f.ops.map((o) => <option key={o} value={o}>{OP_LABEL[o]}</option>)}
            </select>
            {f.values ? (
              <select value={c.value} onChange={(e) => setCond(i, { value: e.target.value })}>
                {f.values.map((v) => <option key={v} value={v}>{v}</option>)}
              </select>
            ) : (
              <input value={c.value} type={f.number ? "number" : "text"}
                placeholder={f.number ? "0" : t("Wert")} onChange={(e) => setCond(i, { value: e.target.value })} />
            )}
            <button type="button" className="btn ghost sm" onClick={() => onChange({ ...rule, conditions: rule.conditions.filter((_, j) => j !== i) })}>✕</button>
          </div>
        );
      })}
      <button type="button" className="btn ghost sm" style={{ marginTop: 6 }}
        onClick={() => onChange({ ...rule, conditions: [...rule.conditions, { field: "os", op: "eq", value: "" }] })}>
        + {t("Bedingung")}
      </button>
    </div>
  );
}

const emptyRule: Rule = { match: "all", conditions: [{ field: "os", op: "eq", value: "" }] };
function parseRule(s: string): Rule {
  try { const r = JSON.parse(s); if (r && Array.isArray(r.conditions)) return r; } catch { /* */ }
  return { ...emptyRule, conditions: [...emptyRule.conditions] };
}

export function Groups() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [smart, setSmart] = useState(false);
  const [rule, setRule] = useState<Rule>({ ...emptyRule, conditions: [...emptyRule.conditions] });
  const [editId, setEditId] = useState<string | null>(null);
  const [editRule, setEditRule] = useState<Rule>(emptyRule);

  const { data: groups } = useQuery({ queryKey: ["groups"], queryFn: () => api.get<Group[]>("/groups") });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["groups"] });

  const cleanRule = (r: Rule) => ({ match: r.match, conditions: r.conditions.filter((c) => c.value !== "" || c.field === "status") });
  const create = useMutation({
    mutationFn: () => api.post<Group>("/groups", { name, description, rule: smart ? JSON.stringify(cleanRule(rule)) : "" }),
    onSuccess: () => { setName(""); setDescription(""); setSmart(false); setRule({ ...emptyRule, conditions: [...emptyRule.conditions] }); invalidate(); },
  });
  const saveRule = useMutation({
    mutationFn: (g: Group) => api.put(`/groups/${g.id}`, { name: g.name, description: g.description, parent_id: g.parent_id, rule: JSON.stringify(cleanRule(editRule)) }),
    onSuccess: () => { setEditId(null); invalidate(); },
  });
  const remove = useMutation({ mutationFn: (id: string) => api.del(`/groups/${id}`), onSuccess: invalidate });

  const submit = (e: FormEvent) => { e.preventDefault(); if (name.trim()) create.mutate(); };

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>{t("Tags")}</h1>
          <p className="muted">{t("Freie Labels zum Querschneiden (n:m). Smart Groups bestimmen ihre Mitglieder automatisch per Regel.")}</p>
        </div>
      </header>

      {isAdmin && (
        <form className="card" onSubmit={submit}>
          <div className="inline-form" style={{ alignItems: "center" }}>
            <input placeholder={t("Tag-Name")} value={name} onChange={(e) => setName(e.target.value)} />
            <input placeholder={t("Beschreibung (optional)")} value={description} onChange={(e) => setDescription(e.target.value)} />
            <label className="check-inline">
              <input type="checkbox" checked={smart} onChange={(e) => setSmart(e.target.checked)} />
              <span>{t("Smart Group")}</span>
            </label>
            <button className="btn primary" type="submit" disabled={create.isPending}>{t("Anlegen")}</button>
          </div>
          {smart && <RuleEditor rule={rule} onChange={setRule} />}
        </form>
      )}

      <div className="cards">
        {(groups ?? []).map((g) => (
          <div className="card group-card" key={g.id}>
            <div style={{ flex: 1 }}>
              <div className="group-name">
                {g.name}
                {g.rule && <span className="badge badge-online" style={{ marginLeft: 8 }} title={t("Smart Group – Mitglieder per Regel")}>{t("Smart")}</span>}
              </div>
              <div className="muted">{g.description || "—"}</div>
              {editId === g.id && (
                <div style={{ marginTop: 8 }}>
                  <RuleEditor rule={editRule} onChange={setEditRule} />
                  <div className="inline-form" style={{ marginTop: 6 }}>
                    <button className="btn primary sm" onClick={() => saveRule.mutate(g)}>{t("Speichern")}</button>
                    <button className="btn ghost sm" onClick={() => setEditId(null)}>{t("Abbrechen")}</button>
                  </div>
                </div>
              )}
            </div>
            <div className="group-meta">
              <span className="count">{g.device_count ?? 0} {t("Geräte")}</span>
              {isAdmin && editId !== g.id && (
                <button className="btn ghost sm" onClick={() => { setEditId(g.id); setEditRule(parseRule(g.rule)); }}>
                  {g.rule ? t("Regel bearbeiten") : t("Zur Smart Group machen")}
                </button>
              )}
              {isAdmin && (
                <button className="btn ghost sm" onClick={() => confirm(t("Tag „{name}“ löschen?", { name: g.name })) && remove.mutate(g.id)}>
                  {t("Löschen")}
                </button>
              )}
            </div>
          </div>
        ))}
        {(groups ?? []).length === 0 && <p className="muted">{t("Noch keine Tags.")}</p>}
      </div>
    </div>
  );
}
