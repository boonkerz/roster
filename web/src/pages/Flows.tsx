import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api } from "../api";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import type { Flow, FlowRunSummary, FlowTemplate } from "../types";
import { FlowEditor } from "../flows/FlowEditor";
import { FlowRunWizard } from "../flows/FlowRunWizard";
import { localizeTemplate } from "../flows/nodeTypes";
import { FlowImport, downloadFlow } from "../flows/FlowImport";
import { Modal } from "../components/Modal";

// Flows listet die Abläufe (Flow-Designer) mit Vorlagen und letzten Läufen.
export function Flows() {
  const { t } = useI18n();
  const nav = useNavigate();
  const qc = useQueryClient();
  const { hasPerm } = useAuth();
  const canEdit = hasPerm("page.scripts");
  const canRun = hasPerm("devices.operate");
  const { data: flows } = useQuery({ queryKey: ["flows"], queryFn: () => api.get<Flow[]>("/flows") });
  const { data: templates } = useQuery({ queryKey: ["flow-templates"], queryFn: () => api.get<FlowTemplate[]>("/flows/templates") });
  const [open, setOpen] = useState<string | null>(null);
  const [importing, setImporting] = useState(false);
  const [exportErr, setExportErr] = useState("");
  const del = useMutation({ mutationFn: (id: string) => api.del(`/flows/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["flows"] }) });
  const copy = useMutation({ mutationFn: (f: Flow) => api.post<Flow>(`/flows/${f.id}/copy`, { name: `${f.name} (${t("Kopie")})` }), onSuccess: () => qc.invalidateQueries({ queryKey: ["flows"] }) });

  return (
    <div className="page wide">
      <header className="page-head">
        <div>
          <h1>{t("Abläufe")}</h1>
          <p className="muted">{t("Wiederkehrende Änderungen auf vielen Servern als Ablauf aus Bausteinen: Dateien finden, Text ersetzen, Docker Compose, Befehle, API-Abfragen, Prüfen und Zurückrollen. Ausführung mit Vorschau auf einem Server, Testlauf und Rollout.")}</p>
        </div>
        {canEdit && (
          <div className="inline-form" style={{ gap: 8 }}>
            <select value="" onChange={(e) => e.target.value && nav(`/flows/new?template=${e.target.value}`)}>
              <option value="">{t("Aus Vorlage …")}</option>
              {(templates ?? []).map((tp) => <option key={tp.key} value={tp.key}>{t(tp.name)}</option>)}
            </select>
            <button className="btn ghost" onClick={() => setImporting(true)}>{t("Importieren")}</button>
            <button className="btn primary" onClick={() => nav("/flows/new")}>{t("Neuer Ablauf")}</button>
          </div>
        )}
      </header>
      {importing && <Modal onClose={() => setImporting(false)}><FlowImport onDone={(f) => { setImporting(false); nav(`/flows/${f.id}`); }} /></Modal>}
      {exportErr && <p className="error">{exportErr}</p>}
      <section className="card">
        {(flows ?? []).length === 0 ? (
          <p className="muted">{t("Noch keine Abläufe. Am schnellsten über „Aus Vorlage …“, z. B. „Image in Compose ersetzen“.")}</p>
        ) : (
          <table className="table">
            <thead><tr><th>{t("Name")}</th><th>{t("Beschreibung")}</th><th>{t("Bausteine")}</th><th>{t("Geändert")}</th><th></th></tr></thead>
            <tbody>
              {(flows ?? []).map((f) => [
                <tr key={f.id}>
                  <td className="link-strong">{f.name}</td>
                  <td className="muted small">{f.description}</td>
                  <td>{f.graph.nodes.length}</td>
                  <td className="muted small">{new Date(f.updated_at).toLocaleString()}</td>
                  <td style={{ whiteSpace: "nowrap", textAlign: "right" }}>
                    {canRun && <button className="btn primary sm" onClick={() => nav(`/flows/${f.id}/run`)}>{t("Ausführen")}</button>}
                    {canEdit && <button className="btn ghost sm" style={{ marginLeft: 4 }} onClick={() => nav(`/flows/${f.id}`)}>{t("Bearbeiten")}</button>}
                    {canEdit && <button className="btn ghost sm" style={{ marginLeft: 4 }} disabled={copy.isPending} onClick={() => copy.mutate(f)}>{t("Kopieren")}</button>}
                    <button className="btn ghost sm" style={{ marginLeft: 4 }} title={t("Als roster-flow-Datei (JSON) herunterladen")} onClick={() => { setExportErr(""); downloadFlow(f).catch((e) => setExportErr(e.message)); }}>{t("Exportieren")}</button>
                    <button className="btn ghost sm" style={{ marginLeft: 4 }} onClick={() => setOpen(open === f.id ? null : f.id)}>{t("Läufe")}</button>
                    {canEdit && <button className="btn ghost sm danger" style={{ marginLeft: 4 }} onClick={() => window.confirm(t("Ablauf „{name}“ löschen?", { name: f.name })) && del.mutate(f.id)}>{t("Löschen")}</button>}
                  </td>
                </tr>,
                open === f.id && <tr key={f.id + "-runs"}><td colSpan={5}><FlowRuns flowId={f.id} /></td></tr>,
              ])}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

function FlowRuns({ flowId }: { flowId: string }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { hasPerm } = useAuth();
  const { data } = useQuery({ queryKey: ["flow-runs", flowId], queryFn: () => api.get<FlowRunSummary[]>(`/flows/${flowId}/runs`) });
  const delRun = useMutation({ mutationFn: (id: string) => api.del(`/flow-runs/${id}`), onSuccess: () => qc.invalidateQueries({ queryKey: ["flow-runs", flowId] }) });
  if (!data) return null;
  if (data.length === 0) return <p className="muted small">{t("Noch keine Läufe.")}</p>;
  return (
    <table className="table">
      <thead><tr><th>{t("Gestartet")}</th><th>{t("von")}</th><th>{t("Geräte")}</th><th>{t("erfolgreich")}</th><th>{t("fehlgeschlagen")}</th><th>{t("ausstehend")}</th><th></th></tr></thead>
      <tbody>
        {data.map((r) => (
          <tr key={r.id}>
            <td className="small">{new Date(r.created_at).toLocaleString()}</td>
            <td className="small">{r.started_by || "—"}</td>
            <td>{r.devices}</td><td>{r.ok}</td><td>{r.failed > 0 ? <span className="badge badge-offline">{r.failed}</span> : 0}</td><td>{r.pending}</td>
            <td style={{ whiteSpace: "nowrap" }}><Link to={`/flows/${flowId}/run?run=${r.id}`}>{t("Öffnen")} →</Link>
              {hasPerm("page.scripts") && <button className="btn ghost sm danger" style={{ marginLeft: 8 }} onClick={() => window.confirm(t("Lauf vom {date} löschen?", { date: new Date(r.created_at).toLocaleString() })) && delRun.mutate(r.id)}>{t("Löschen")}</button>}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// FlowEditPage lädt einen Ablauf (oder eine Vorlage) in den Editor.
export function FlowEditPage() {
  const { t } = useI18n();
  const { id } = useParams();
  const nav = useNavigate();
  const [params] = useSearchParams();
  const isNew = !id || id === "new";
  const { data: flow } = useQuery({ queryKey: ["flow", id], enabled: !isNew, queryFn: () => api.get<Flow>(`/flows/${id}`) });
  const tplKey = params.get("template");
  const { data: templates } = useQuery({ queryKey: ["flow-templates"], enabled: isNew && !!tplKey, queryFn: () => api.get<FlowTemplate[]>("/flows/templates") });
  const tpl = templates?.find((x) => x.key === tplKey);
  const start: Partial<Flow> | undefined = isNew ? (tplKey ? (tpl ? localizeTemplate(tpl, t) : undefined) : { name: "", description: "", graph: { nodes: [], edges: [] }, params: [] }) : flow;
  return (
    <div className="page wide">
      <header className="page-head">
        <div className="inline-form">
          <button className="btn ghost sm" onClick={() => nav("/flows")}>← {t("Abläufe")}</button>
          <h1 style={{ margin: 0 }}>{isNew ? t("Neuer Ablauf") : t("Ablauf bearbeiten")}</h1>
        </div>
      </header>
      {start ? <FlowEditor key={id ?? "new"} flow={start} onSaved={(f) => nav(`/flows/${f.id}`, { replace: true })} onCancel={() => nav("/flows")}
        onCopied={(f) => nav(`/flows/${f.id}`)} onDeleted={() => nav("/flows")} /> : <p className="muted">{t("Lädt …")}</p>}
    </div>
  );
}

// FlowRunPage zeigt den Assistenten; Parameter/Gerät/Ziele können per URL vorbelegt werden.
export function FlowRunPage() {
  const { t } = useI18n();
  const { id } = useParams();
  const nav = useNavigate();
  const [q] = useSearchParams();
  const { data: flow } = useQuery({ queryKey: ["flow", id], queryFn: () => api.get<Flow>(`/flows/${id}`) });
  const initialParams: Record<string, string> = {};
  q.forEach((v, k) => { if (k.startsWith("p.")) initialParams[k.slice(2)] = v; });
  const targets = (q.get("devices") ?? "").split(",").filter(Boolean);
  return (
    <div className="page wide">
      <header className="page-head">
        <div className="inline-form">
          <button className="btn ghost sm" onClick={() => nav("/flows")}>← {t("Abläufe")}</button>
          <h1 style={{ margin: 0 }}>{t("Ausführen")}: {flow?.name ?? "…"}</h1>
        </div>
      </header>
      {flow && <FlowRunWizard flow={flow} initialParams={initialParams} initialDevice={q.get("device") ?? targets[0]} initialTargets={targets} initialRun={q.get("run") ?? undefined} />}
    </div>
  );
}
