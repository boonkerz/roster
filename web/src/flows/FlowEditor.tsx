import { useCallback, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ReactFlow, Background, Controls, MiniMap, Handle, Position, MarkerType,
  applyNodeChanges, applyEdgeChanges,
  type Node, type Edge, type Connection, type NodeChange, type EdgeChange, type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import { useTheme } from "../theme";
import type { Flow, FlowGraph, FlowNodeType, FlowParam, Script } from "../types";
import { NODE_TYPE, NODE_TYPES, nodeSummary, type FieldDef } from "./nodeTypes";
import { downloadFlow } from "./FlowImport";
import { YamlPlayground } from "./YamlPlayground";
import { Modal } from "../components/Modal";
import { ConfirmButton } from "../components/ConfirmButton";

type NodeData = { type: FlowNodeType; label: string; config: Record<string, string | number | boolean> };
type FNode = Node<NodeData, "flow">;

// FlowNodeCard ist die Kachel eines Bausteins mit Eingang oben und Ausgängen ok/fail unten.
function FlowNodeCard({ id, data, selected }: NodeProps<FNode>) {
  const { t } = useI18n();
  const def = NODE_TYPE[data.type];
  return (
    <div className={`flow-node ${selected ? "sel" : ""}`}>
      <Handle type="target" position={Position.Top} id="in" />
      <div className="flow-node-head"><span>{def?.icon}</span> <strong>{data.label || t(def?.label ?? data.type)}</strong></div>
      <div className="flow-node-sub mono">{id} · {t(def?.label ?? data.type)}</div>
      <div className="flow-node-sum">{nodeSummary(data.type, data.config)}</div>
      {data.config.only_if ? <div className="flow-node-sum" title={String(data.config.only_if)}>⤷ {t("nur wenn")} {String(data.config.only_if)}</div> : null}
      <Handle type="source" position={Position.Bottom} id="ok" className="h-ok" style={{ left: "30%" }} />
      <Handle type="source" position={Position.Bottom} id="fail" className="h-fail" style={{ left: "70%" }} />
      <div className="flow-node-ports"><span className="p-ok">ok</span><span className="p-fail">{t("Fehler")}</span></div>
    </div>
  );
}
const nodeTypes = { flow: FlowNodeCard };

let failLabel = "Fehler"; // wird beim Rendern des Editors übersetzt
const edgeStyle = (port: string) => ({
  style: { stroke: port === "fail" ? "var(--red)" : "var(--green)", strokeWidth: 2 },
  markerEnd: { type: MarkerType.ArrowClosed, color: port === "fail" ? "var(--red)" : "var(--green)" },
  label: port === "fail" ? failLabel : "",
  labelStyle: { fill: "var(--red)", fontSize: 11 },
  labelBgStyle: { fill: "var(--surface)" },
});

function toRF(g: FlowGraph): { nodes: FNode[]; edges: Edge[] } {
  return {
    nodes: g.nodes.map((n, i) => ({ id: n.id, type: "flow", position: { x: n.x ?? 0, y: n.y ?? i * 130 }, data: { type: n.type, label: n.label ?? "", config: { ...(n.config ?? {}) } } })),
    edges: g.edges.map((e) => ({ id: `${e.from}-${e.port}-${e.to}`, source: e.from, sourceHandle: e.port, target: e.to, targetHandle: "in", ...edgeStyle(e.port) })),
  };
}

function fromRF(nodes: FNode[], edges: Edge[]): FlowGraph {
  return {
    nodes: nodes.map((n) => ({ id: n.id, type: n.data.type, label: n.data.label || undefined, config: n.data.config, x: Math.round(n.position.x), y: Math.round(n.position.y) })),
    edges: edges.map((e) => ({ from: e.source, port: (e.sourceHandle === "fail" ? "fail" : "ok") as "ok" | "fail", to: e.target })),
  };
}

// FlowEditor bearbeitet einen Ablauf: Palette, Zeichenfläche, Eigenschaften, Parameter.
export function FlowEditor({ flow, onSaved, onCancel, onCopied, onDeleted, justSaved }: { flow: Partial<Flow>; onSaved: (f: Flow) => void; onCancel: () => void; onCopied?: (f: Flow) => void; onDeleted?: () => void; justSaved?: boolean }) {
  const { t } = useI18n();
  const { theme } = useTheme();
  const qc = useQueryClient();
  failLabel = t("Fehler");
  const initial = useMemo(() => toRF(flow.graph ?? { nodes: [], edges: [] }), [flow]);
  const [nodes, setNodes] = useState<FNode[]>(initial.nodes);
  const [edges, setEdges] = useState<Edge[]>(initial.edges);
  const [name, setName] = useState(flow.name ?? "");
  const [description, setDescription] = useState(flow.description ?? "");
  const [params, setParams] = useState<FlowParam[]>(flow.params ?? []);
  const [selected, setSelected] = useState<string | null>(null);
  const [err, setErr] = useState("");
  const [playground, setPlayground] = useState(false);
  const [savedAt, setSavedAt] = useState<Date | null>(justSaved ? new Date() : null);
  const [dirty, setDirty] = useState(false);
  const { data: scripts } = useQuery({ queryKey: ["scripts"], queryFn: () => api.get<Script[]>("/scripts") });

  const onNodesChange = useCallback((ch: NodeChange<FNode>[]) => {
    if (ch.some((c) => c.type !== "select" && c.type !== "dimensions")) setDirty(true);
    setNodes((ns) => applyNodeChanges(ch, ns));
  }, []);
  const onEdgesChange = useCallback((ch: EdgeChange[]) => {
    if (ch.some((c) => c.type !== "select")) setDirty(true);
    setEdges((es) => applyEdgeChanges(ch, es));
  }, []);
  const onConnect = useCallback((c: Connection) => {
    if (!c.source || !c.target || c.source === c.target) return;
    setDirty(true);
    const port = c.sourceHandle === "fail" ? "fail" : "ok";
    setEdges((es) => [
      ...es.filter((e) => !(e.source === c.source && (e.sourceHandle ?? "ok") === port)),
      { id: `${c.source}-${port}-${c.target}`, source: c.source!, sourceHandle: port, target: c.target!, targetHandle: "in", ...edgeStyle(port) },
    ]);
  }, []);

  const addNode = (type: FlowNodeType) => {
    const def = NODE_TYPE[type];
    let i = 1;
    while (nodes.some((n) => n.id === `${def.prefix}${i}`)) i++;
    const id = `${def.prefix}${i}`;
    const maxY = nodes.reduce((m, n) => Math.max(m, n.position.y), -130);
    const sel = nodes.find((n) => n.id === selected);
    const pos = sel ? { x: sel.position.x, y: sel.position.y + 140 } : { x: 0, y: maxY + 140 };
    setDirty(true);
    setNodes((ns) => [...ns.map((n) => ({ ...n, selected: false })), { id, type: "flow", position: pos, selected: true, data: { type, label: "", config: { ...def.defaults } } }]);
    // Automatisch an den ausgewählten Baustein anhängen (ok-Ausgang), wenn der frei ist.
    if (sel && !edges.some((e) => e.source === sel.id && (e.sourceHandle ?? "ok") === "ok")) {
      setEdges((es) => [...es, { id: `${sel.id}-ok-${id}`, source: sel.id, sourceHandle: "ok", target: id, targetHandle: "in", ...edgeStyle("ok") }]);
    }
    setSelected(id);
  };

  const selNode = nodes.find((n) => n.id === selected);
  const updateSel = (patch: Partial<NodeData>, cfgPatch?: Record<string, string | number | boolean>) => {
    setDirty(true);
    setNodes((ns) => ns.map((n) => n.id === selected ? { ...n, data: { ...n.data, ...patch, config: { ...n.data.config, ...(cfgPatch ?? {}) } } } : n));
  };
  const removeSel = () => {
    if (!selected) return;
    setNodes((ns) => ns.filter((n) => n.id !== selected));
    setEdges((es) => es.filter((e) => e.source !== selected && e.target !== selected));
    setSelected(null);
  };

  const save = useMutation({
    mutationFn: () => {
      const body = { name, description, params: params.filter((p) => p.name.trim()), graph: fromRF(nodes, edges) };
      return flow.id ? api.put<Flow>(`/flows/${flow.id}`, body) : api.post<Flow>("/flows", body);
    },
    onSuccess: (f) => { setErr(""); setSavedAt(new Date()); setDirty(false); qc.invalidateQueries({ queryKey: ["flows"] }); qc.invalidateQueries({ queryKey: ["flow", f.id] }); onSaved(f); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : t("Speichern fehlgeschlagen")),
  });

  // Kopie: speichert den aktuellen Stand des Editors als neuen Ablauf.
  const copy = useMutation({
    mutationFn: () => api.post<Flow>("/flows", { name: `${name || flow.name || ""} (${t("Kopie")})`, description, params: params.filter((p) => p.name.trim()), graph: fromRF(nodes, edges) }),
    onSuccess: (f) => { setErr(""); qc.invalidateQueries({ queryKey: ["flows"] }); onCopied?.(f); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : t("Speichern fehlgeschlagen")),
  });
  const del = useMutation({
    mutationFn: () => api.del(`/flows/${flow.id}`),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["flows"] }); onDeleted?.(); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : t("Fehler")),
  });

  const fieldInput = (f: FieldDef, cfg: Record<string, string | number | boolean>) => {
    const v = cfg[f.key];
    if (f.kind === "bool") {
      return <label className="chip" key={f.key}><input type="checkbox" checked={!!v} onChange={(e) => updateSel({}, { [f.key]: e.target.checked })} /> {t(f.label)}</label>;
    }
    let input;
    if (f.kind === "select") {
      const opts = f.key === "script_id" ? [["", t("— eigener Befehl —")], ...(scripts ?? []).filter((s) => !s.check_only).map((s) => [s.id, s.name])] : f.options ?? [];
      input = <select value={String(v ?? "")} onChange={(e) => updateSel({}, { [f.key]: e.target.value })}>{opts.map(([k, l]) => <option key={k} value={k}>{f.key === "script_id" ? l : t(l)}</option>)}</select>;
    } else if (f.kind === "textarea") {
      input = <textarea rows={3} className="mono" placeholder={f.placeholder} value={String(v ?? "")} onChange={(e) => updateSel({}, { [f.key]: e.target.value })} />;
    } else {
      input = <input type={f.kind === "number" ? "number" : "text"} className={f.kind === "text" ? "mono" : ""} placeholder={f.placeholder} value={String(v ?? "")}
        onChange={(e) => updateSel({}, { [f.key]: f.kind === "number" ? (e.target.value === "" ? "" : Number(e.target.value)) : e.target.value })} />;
    }
    return <label className="flow-field" key={f.key}><span className="muted small">{t(f.label)}</span>{input}</label>;
  };

  return (
    <div className="flow-editor">
      {playground && selNode?.data.type === "yaml" && (
        <Modal onClose={() => setPlayground(false)}>
          <YamlPlayground ops={String(selNode.data.config.ops ?? "")} params={params} onOpsChange={(v) => updateSel({}, { ops: v })} />
        </Modal>
      )}
      <div className="flow-meta card">
        <div className="inline-form" style={{ gap: 8, flexWrap: "wrap" }}>
          <input placeholder={t("Name des Ablaufs")} value={name} onChange={(e) => { setName(e.target.value); setDirty(true); }} style={{ minWidth: 260, fontWeight: 600 }} />
          <input placeholder={t("Beschreibung (optional)")} value={description} onChange={(e) => { setDescription(e.target.value); setDirty(true); }} style={{ flex: 1, minWidth: 260 }} />
          <button className="btn primary" disabled={save.isPending} onClick={() => save.mutate()}>{t("Speichern")}</button>
          <button className="btn ghost" onClick={onCancel}>{t("Abbrechen")}</button>
          {flow.id && <button className="btn ghost" title={t("Gespeicherten Stand als roster-flow-Datei (JSON) herunterladen")} onClick={() => downloadFlow({ id: flow.id!, name: flow.name ?? "" }).catch((e) => setErr(e.message))}>{t("Exportieren")}</button>}
          {flow.id && <button className="btn ghost" disabled={copy.isPending} onClick={() => copy.mutate()} title={t("Speichert den aktuellen Stand als neuen Ablauf")}>{t("Kopieren")}</button>}
          {flow.id && <ConfirmButton className="btn ghost danger" disabled={del.isPending} confirmLabel={t("Wirklich löschen?")} onConfirm={() => del.mutate()}>{t("Löschen")}</ConfirmButton>}
          {save.isPending ? <span className="muted small">{t("Speichert …")}</span>
            : dirty ? <span className="small" style={{ color: "var(--amber)" }}>● {t("Ungespeicherte Änderungen")}</span>
            : savedAt ? <span className="form-ok small">✓ {t("Gespeichert um {time}", { time: savedAt.toLocaleTimeString() })}</span> : null}
        </div>
        {err && <p className="error small" style={{ marginBottom: 0 }}>{err}</p>}
        <details style={{ marginTop: 8 }} open={params.length > 0}>
          <summary className="small"><strong>{t("Parameter")}</strong> <span className="muted">({params.length}) – {t("fragt der Assistent vor dem Start ab, Verwendung als {{param.NAME}}")}</span></summary>
          {params.map((p, i) => (
            <div className="inline-form" key={i} style={{ gap: 6, marginTop: 6 }}>
              <input className="mono" placeholder="NAME" value={p.name} onChange={(e) => { setDirty(true); setParams(params.map((x, j) => j === i ? { ...x, name: e.target.value } : x)); }} style={{ width: 140 }} />
              <input placeholder={t("Beschriftung")} value={p.label ?? ""} onChange={(e) => { setDirty(true); setParams(params.map((x, j) => j === i ? { ...x, label: e.target.value } : x)); }} style={{ flex: 1 }} />
              <input placeholder={t("Standardwert")} value={p.default ?? ""} onChange={(e) => { setDirty(true); setParams(params.map((x, j) => j === i ? { ...x, default: e.target.value } : x)); }} style={{ width: 200 }} />
              <label className="chip"><input type="checkbox" checked={!!p.required} onChange={(e) => { setDirty(true); setParams(params.map((x, j) => j === i ? { ...x, required: e.target.checked } : x)); }} /> {t("Pflicht")}</label>
              <button className="btn ghost sm" onClick={() => { setDirty(true); setParams(params.filter((_, j) => j !== i)); }}>✕</button>
            </div>
          ))}
          <button className="btn ghost sm" style={{ marginTop: 6 }} onClick={() => { setDirty(true); setParams([...params, { name: "", label: "" }]); }}>+ {t("Parameter")}</button>
        </details>
      </div>

      <div className="flow-body">
        <aside className="flow-palette card">
          <h3 className="muted small" style={{ marginTop: 0 }}>{t("Bausteine")}</h3>
          {NODE_TYPES.map((d) => (
            <button key={d.type} className="flow-pal-btn" onClick={() => addNode(d.type)} title={t(d.help)}>
              <span>{d.icon}</span> {t(d.label)}
            </button>
          ))}
          <p className="muted small">{t("Klick fügt den Baustein unter dem ausgewählten an. Linien ziehen: vom grünen Punkt (ok) oder roten Punkt (Fehler) auf den nächsten Baustein. Entf löscht.")}</p>
        </aside>

        <div className="flow-canvas">
          <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} onNodesChange={onNodesChange} onEdgesChange={onEdgesChange} onConnect={onConnect}
            onNodeClick={(_, n) => setSelected(n.id)} onPaneClick={() => setSelected(null)} colorMode={theme} fitView fitViewOptions={{ padding: 0.3, maxZoom: 1 }}
            deleteKeyCode={["Delete", "Backspace"]} proOptions={{ hideAttribution: true }}>
            <Background gap={16} />
            <Controls showInteractive={false} />
            <MiniMap pannable zoomable />
          </ReactFlow>
        </div>

        <aside className="flow-props card">
          {!selNode ? (
            <p className="muted small">{t("Baustein anklicken, um ihn einzustellen.")}</p>
          ) : (() => {
            const def = NODE_TYPE[selNode.data.type];
            return (
              <>
                <h3 style={{ marginTop: 0 }}>{def.icon} {t(def.label)} <span className="muted mono small">{selNode.id}</span></h3>
                <p className="muted small">{t(def.help)}</p>
                <label className="flow-field"><span className="muted small">{t("Beschriftung")}</span>
                  <input value={selNode.data.label} placeholder={t(def.label)} onChange={(e) => updateSel({ label: e.target.value })} /></label>
                {def.fields.filter((f) => !f.show || f.show(selNode.data.config)).map((f) => fieldInput(f, selNode.data.config))}
                <label className="flow-field" title={t("Leer = immer ausführen. Beispiel: {{param.DB_SERVICE}} – der Baustein läuft nur, wenn der Parameter gesetzt ist; mit ! davor nur, wenn er leer ist. Übersprungene Bausteine gehen über „ok“ weiter.")}><span className="muted small">{t("Nur ausführen, wenn (optional)")}</span>
                  <input className="mono" placeholder="{{param.X}}" value={String(selNode.data.config.only_if ?? "")} onChange={(e) => updateSel({}, { only_if: e.target.value })} /></label>
                {selNode.data.type === "yaml" && <button className="btn primary sm" style={{ marginBottom: 10 }} onClick={() => setPlayground(true)}>{t("Ausprobieren …")}</button>}
                <p className="muted small">{t("Platzhalter")}: <span className="mono">{"{{param.NAME}}"}</span>, <span className="mono">{"{{steps.ID.value}}"}</span>, <span className="mono">{"{{steps.ID.out}}"}</span>, <span className="mono">{"{{steps.ID.json.pfad}}"}</span>, <span className="mono">{"{{agent.feld}}"}</span></p>
                <button className="btn ghost sm danger" onClick={removeSel}>{t("Baustein löschen")}</button>
              </>
            );
          })()}
        </aside>
      </div>
    </div>
  );
}
