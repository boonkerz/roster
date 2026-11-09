import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import type { Flow } from "../types";

// downloadFlow lädt den Ablauf im Austauschformat roster-flow (JSON) herunter.
export async function downloadFlow(f: { id: string; name: string }) {
  const res = await fetch(`/api/v1/flows/${f.id}/export`, { credentials: "same-origin" });
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).error ?? msg; } catch { /* Text bleibt */ }
    throw new Error(msg);
  }
  const blob = await res.blob();
  const name = (res.headers.get("Content-Disposition") ?? "").match(/filename="([^"]+)"/)?.[1] ?? `${f.name}.roster-flow.json`;
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 2000);
}

// FlowImport nimmt eine roster-flow-Datei (Upload oder Einfügen) entgegen, prüft sie und legt
// den Ablauf an.
export function FlowImport({ onDone }: { onDone: (f: Flow) => void }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const parse = (): unknown => {
    try { return JSON.parse(text); } catch (e) { setMsg({ ok: false, text: t("Kein gültiges JSON: {err}", { err: (e as Error).message }) }); return null; }
  };
  const check = useMutation({
    mutationFn: (doc: unknown) => api.post<{ name: string; nodes: number; params: number }>("/flows/import?dry_run=1", doc),
    onSuccess: (r) => setMsg({ ok: true, text: t("Gültig: „{name}“ mit {n} Bausteinen und {p} Parametern.", { name: r.name, n: r.nodes, p: r.params }) }),
    onError: (e) => setMsg({ ok: false, text: e instanceof ApiError ? e.message : t("Fehler") }),
  });
  const imp = useMutation({
    mutationFn: (doc: unknown) => api.post<Flow>("/flows/import", doc),
    onSuccess: (f) => { qc.invalidateQueries({ queryKey: ["flows"] }); onDone(f); },
    onError: (e) => setMsg({ ok: false, text: e instanceof ApiError ? e.message : t("Fehler") }),
  });
  const onFile = async (file?: File) => {
    if (!file) return;
    setText(await file.text());
    setMsg(null);
  };
  return (
    <div style={{ minWidth: "min(720px, 90vw)" }}>
      <h2 style={{ marginTop: 0 }}>{t("Ablauf importieren")}</h2>
      <p className="muted small">{t("Datei im Austauschformat roster-flow (*.roster-flow.json) wählen oder den Inhalt einfügen. Der Ablauf wird geprüft und als neuer Ablauf angelegt; ein gleichnamiger bleibt unverändert.")}</p>
      <input type="file" accept=".json,application/json" onChange={(e) => onFile(e.target.files?.[0])} />
      <textarea className="mono" rows={14} style={{ width: "100%", marginTop: 8 }} placeholder='{ "format": "roster-flow", "version": 1, … }' value={text} onChange={(e) => { setText(e.target.value); setMsg(null); }} />
      {msg && <p className={msg.ok ? "form-ok small" : "error small"}>{msg.text}</p>}
      <div className="inline-form" style={{ gap: 8 }}>
        <button className="btn ghost" disabled={!text.trim() || check.isPending} onClick={() => { const d = parse(); if (d) check.mutate(d); }}>{t("Prüfen")}</button>
        <button className="btn primary" disabled={!text.trim() || imp.isPending} onClick={() => { const d = parse(); if (d) imp.mutate(d); }}>{t("Importieren")}</button>
        <a className="small" href="https://roster-rmm.dev/schemas/roster-flow-1.schema.json" target="_blank" rel="noreferrer">{t("JSON-Schema")}</a>
      </div>
    </div>
  );
}
