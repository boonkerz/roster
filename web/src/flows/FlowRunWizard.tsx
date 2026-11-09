import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import type { ClientTree, Device, Flow, FlowRun, FlowRunDevice, Group } from "../types";
import { FlowResultView, FlowStatusBadge } from "./FlowResultView";
import { ConfirmButton } from "../components/ConfirmButton";

type Target = "devices" | "client" | "site" | "group" | "all";

// FlowRunWizard führt einen Ablauf aus: Parameter → Vorschau auf einem Server → Testlauf auf
// diesem Server → Rollout auf die übrigen Ziele.
export function FlowRunWizard({ flow, initialParams, initialDevice, initialTargets, initialRun }: {
  flow: Flow; initialParams: Record<string, string>; initialDevice?: string; initialTargets?: string[]; initialRun?: string;
}) {
  const { t } = useI18n();
  const [params, setParams] = useState<Record<string, string>>(() => {
    const p: Record<string, string> = {};
    for (const d of flow.params) p[d.name] = initialParams[d.name] ?? d.default ?? "";
    return p;
  });
  const [deviceId, setDeviceId] = useState(initialDevice ?? "");
  const [devFilter, setDevFilter] = useState("");
  const [runId, setRunId] = useState(initialRun ?? "");
  const [err, setErr] = useState("");
  const [tType, setTType] = useState<Target>(initialTargets && initialTargets.length > 0 ? "devices" : "client");
  const [target, setTarget] = useState("");
  const [targetIds, setTargetIds] = useState<string[]>(initialTargets ?? []);
  const [openRow, setOpenRow] = useState<string | null>(null);

  const { data: devices } = useQuery({ queryKey: ["devices"], queryFn: () => api.get<Device[]>("/devices") });
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: groups } = useQuery({ queryKey: ["groups"], queryFn: () => api.get<Group[]>("/groups") });
  const { data: run } = useQuery({
    queryKey: ["flow-run", runId], enabled: !!runId,
    queryFn: () => api.get<FlowRun>(`/flow-runs/${runId}`),
    refetchInterval: (q) => ((q.state.data?.devices ?? []).some((d) => d.status === "queued") ? 2000 : false),
  });
  // Gespeicherten Lauf öffnen: Parameter und Server dieses Laufs anzeigen (nicht die Standardwerte).
  const [synced, setSynced] = useState(false);
  useEffect(() => {
    if (!run || synced) return;
    setSynced(true);
    setParams((cur) => ({ ...cur, ...(run.params ?? {}) }));
    if (run.test_device_id) setDeviceId(run.test_device_id);
  }, [run, synced]);
  const host = (id: string) => devices?.find((d) => d.id === id)?.hostname ?? run?.devices.find((d) => d.device_id === id)?.hostname ?? id;
  const preview = run?.devices.find((d) => d.stage === "preview");
  const test = run?.devices.find((d) => d.stage === "test");
  const rollout = (run?.devices ?? []).filter((d) => d.stage === "rollout");
  const testDevice = run?.test_device_id || deviceId;

  const onErr = (e: unknown) => setErr(e instanceof ApiError ? e.message : t("Fehler"));
  const start = useMutation({
    mutationFn: () => api.post<{ id: string }>(`/flows/${flow.id}/runs`, { params, device_id: deviceId }),
    onSuccess: (d) => { setErr(""); setRunId(d.id); },
    onError: onErr,
  });
  const runTest = useMutation({ mutationFn: () => api.post(`/flow-runs/${runId}/test`, {}), onSuccess: () => setErr(""), onError: onErr });
  const runRollout = useMutation({
    mutationFn: () => api.post<{ queued: number }>(`/flow-runs/${runId}/rollout`, { target_type: tType, target_id: tType === "all" || tType === "devices" ? "" : target, device_ids: tType === "devices" ? targetIds : [] }),
    onSuccess: () => setErr(""), onError: onErr,
  });

  const devList = useMemo(() => (devices ?? []).filter((d) => !devFilter || d.hostname.toLowerCase().includes(devFilter.toLowerCase())).slice(0, 200), [devices, devFilter]);
  const targets = tType === "client" ? (tree?.clients ?? []).map((c) => ({ id: c.id, name: c.name }))
    : tType === "site" ? (tree?.clients ?? []).flatMap((c) => (c.sites ?? []).map((s) => ({ id: s.id, name: `${c.name} › ${s.name}` })))
    : tType === "group" ? (groups ?? []).map((g) => ({ id: g.id, name: g.name })) : [];
  const done = (d?: FlowRunDevice) => !!d && d.status !== "queued";
  const counts = rollout.reduce((m, d) => { m[d.status] = (m[d.status] ?? 0) + 1; return m; }, {} as Record<string, number>);

  return (
    <div className="flow-wizard">
      {/* 1. Parameter + Vorschau-Server */}
      <section className="card">
        <h2>1 · {t("Parameter und Vorschau-Server")}</h2>
        {flow.description && <p className="muted small">{flow.description}</p>}
        {flow.params.map((p) => (
          <label className="flow-field" key={p.name}><span className="muted small">{p.label ? t(p.label) : p.name}{p.required ? " *" : ""} <span className="mono">{`{{param.${p.name}}}`}</span></span>
            <input className="mono" value={params[p.name] ?? ""} disabled={!!runId} onChange={(e) => setParams({ ...params, [p.name]: e.target.value })} /></label>
        ))}
        <label className="flow-field"><span className="muted small">{t("Vorschau- und Testserver")}</span>
          <div className="inline-form" style={{ gap: 6 }}>
            <input placeholder={t("Gerät suchen …")} value={devFilter} disabled={!!runId} onChange={(e) => setDevFilter(e.target.value)} style={{ width: 200 }} />
            <select value={deviceId} disabled={!!runId} onChange={(e) => setDeviceId(e.target.value)} style={{ minWidth: 260 }}>
              <option value="">{t("— Server wählen —")}</option>
              {deviceId && !devList.some((d) => d.id === deviceId) && <option value={deviceId}>{host(deviceId)}</option>}
              {devList.map((d) => <option key={d.id} value={d.id}>{d.hostname}</option>)}
            </select>
          </div>
        </label>
        {!runId && <button className="btn primary" disabled={!deviceId || start.isPending} onClick={() => start.mutate()}>{t("Vorschau starten")}</button>}
        {runId && !preview && <p className="muted small">{t("Lauf geladen.")}</p>}
      </section>

      {/* 2. Vorschau */}
      {preview && (
        <section className="card">
          <h2>2 · {t("Vorschau auf {host}", { host: preview.hostname || host(preview.device_id) })} <FlowStatusBadge status={preview.status} /></h2>
          {preview.status === "queued" ? <p className="muted">{t("Warte auf den Agent … (Vorschau ändert nichts)")}</p>
            : preview.result && <FlowResultView result={preview.result} hideStatus />}
        </section>
      )}

      {/* 3. Testlauf */}
      {done(preview) && (
        <section className="card">
          <h2>3 · {t("Ausführen auf {host}", { host: host(testDevice) })} {test && <FlowStatusBadge status={test.status} />}</h2>
          {!test && (
            <>
              <p className="muted small">{t("Führt den Ablauf echt auf diesem Server aus: Dateien werden geändert (mit Backup), Befehle laufen.")}</p>
              <ConfirmButton className="btn primary" disabled={runTest.isPending} confirmLabel={t("Echt auf {host} ausführen? Nochmal klicken", { host: host(testDevice) })} onConfirm={() => runTest.mutate()}>{t("Auf Testserver ausführen")}</ConfirmButton>
            </>
          )}
          {test?.status === "queued" && <p className="muted">{t("Läuft … (pull/up kann einige Minuten dauern)")}</p>}
          {test?.result && <FlowResultView result={test.result} hideStatus />}
        </section>
      )}

      {/* 4. Rollout */}
      {done(test) && (
        <section className="card">
          <h2>4 · {t("Auf weitere Server ausrollen")}</h2>
          {test?.status !== "ok" && <p className="error small">{t("Der Testlauf war nicht erfolgreich – erst die Ursache prüfen.")}</p>}
          <div className="inline-form" style={{ gap: 8, flexWrap: "wrap" }}>
            <select value={tType} onChange={(e) => { setTType(e.target.value as Target); setTarget(""); }}>
              {initialTargets && initialTargets.length > 0 && <option value="devices">{t("Betroffene Geräte ({n})", { n: initialTargets.length })}</option>}
              <option value="client">{t("Firma")}</option>
              <option value="site">{t("Standort")}</option>
              <option value="group">{t("Tag / Gruppe")}</option>
              <option value="all">{t("Alle Geräte")}</option>
            </select>
            {tType !== "all" && tType !== "devices" && (
              <select value={target} onChange={(e) => setTarget(e.target.value)} style={{ minWidth: 220 }}>
                <option value="">{t("— Ziel wählen —")}</option>
                {targets.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
              </select>
            )}
            {tType === "devices" && <span className="muted small">{targetIds.filter((id) => id !== testDevice).map(host).join(", ")}</span>}
            <ConfirmButton className="btn primary" disabled={runRollout.isPending || (tType !== "all" && tType !== "devices" && !target)}
              confirmLabel={t("Auf allen Geräten des Ziels ausführen? Nochmal klicken")} onConfirm={() => runRollout.mutate()}>{t("Ausrollen")}</ConfirmButton>
            {tType === "devices" && <button className="btn ghost sm" onClick={() => setTargetIds([])}>{t("Liste leeren")}</button>}
          </div>
          {runRollout.data && <p className="form-ok small">{t("Auf {n} Gerät(en) eingereiht.", { n: runRollout.data.queued })}</p>}
          {rollout.length > 0 && (
            <>
              <div className="inline-form" style={{ gap: 6, margin: "10px 0" }}>
                {Object.entries(counts).map(([k, n]) => <span key={k}><FlowStatusBadge status={k} /> {n}</span>)}
              </div>
              <table className="table">
                <thead><tr><th>{t("Gerät")}</th><th>{t("Status")}</th><th>{t("Meldung")}</th></tr></thead>
                <tbody>
                  {rollout.map((d) => [
                    <tr key={d.device_id} className="clickable" style={{ cursor: "pointer" }} onClick={() => setOpenRow(openRow === d.device_id ? null : d.device_id)}>
                      <td><Link to={`/devices/${d.device_id}`} onClick={(e) => e.stopPropagation()} className="link-strong">{d.hostname || host(d.device_id)}</Link></td>
                      <td><FlowStatusBadge status={d.status} /></td>
                      <td className="muted small">{d.result?.error ?? ""}</td>
                    </tr>,
                    openRow === d.device_id && d.result && <tr key={d.device_id + "-r"}><td colSpan={3}><FlowResultView result={d.result} /></td></tr>,
                  ])}
                </tbody>
              </table>
            </>
          )}
        </section>
      )}
      {err && <p className="error">{err}</p>}
    </div>
  );
}
