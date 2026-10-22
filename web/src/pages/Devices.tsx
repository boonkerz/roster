import { Fragment, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type { Device, Policy, ClientTree as Tree, CustomField } from "../types";
import { ClientTree, OrgFilter } from "../components/ClientTree";
import { AddComputerDialog } from "../components/AddComputerDialog";
import { DevicePanel } from "../components/DevicePanel";
import { DeviceFilter, evalFilter, DFilter, CheckOption } from "../components/DeviceFilter";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { ExportDialog } from "../components/ExportDialog";
import { ColumnsDialog } from "../components/ColumnsDialog";
import type { EntityFieldValue } from "../export";
import { usePref } from "../prefs";
import { baseDeviceColumns, fieldDeviceColumns, resolveColumns, ColumnCtx, CF_PREFIX, DEFAULT_COLUMNS } from "../deviceColumns";

export { primaryIPv4, formatBytes } from "../devutil";

function matchesOrg(d: Device, f: OrgFilter): boolean {
  switch (f.kind) {
    case "all": return true;
    case "unassigned": return !d.site_id;
    case "client": return d.client_id === f.id;
    case "site": return d.site_id === f.id;
  }
}

export function Devices() {
  const { t } = useI18n();
  const [q, setQ] = useState("");
  const [showAdd, setShowAdd] = useState(false);
  // Filterauswahl über Navigation hinweg merken (z.B. Gerätedetail -> zurück).
  const [org, setOrg] = useState<OrgFilter>(() => {
    try {
      const s = sessionStorage.getItem("roster-org");
      if (s) return JSON.parse(s) as OrgFilter;
    } catch { /* ignore */ }
    return { kind: "all" };
  });
  useEffect(() => {
    sessionStorage.setItem("roster-org", JSON.stringify(org));
  }, [org]);
  // Ausgewähltes Gerät (für das untere Detail-Panel) ebenfalls merken.
  const [selectedId, setSelectedId] = useState<string | null>(() => sessionStorage.getItem("roster-selected") || null);
  useEffect(() => {
    if (selectedId) sessionStorage.setItem("roster-selected", selectedId);
    else sessionStorage.removeItem("roster-selected");
  }, [selectedId]);
  // Direktsprung in einen Panel-Tab (z.B. Klick auf Checks/Tasks in der Liste).
  const [jump, setJump] = useState<{ tab: string; n: number }>({ tab: "", n: 0 });
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  // Höhe des oberen (Listen-)Panels – per Trenner ziehbar, in localStorage gemerkt.
  const [listH, setListH] = useState<number>(() => {
    const v = Number(localStorage.getItem("roster-devices-list-h"));
    return v >= 150 ? v : 380;
  });
  const startResize = (e: React.MouseEvent) => {
    e.preventDefault();
    const startY = e.clientY;
    const startH = listH;
    const onMove = (ev: MouseEvent) => {
      const max = Math.max(200, window.innerHeight - 220);
      const h = Math.min(max, Math.max(150, startH + (ev.clientY - startY)));
      setListH(h);
    };
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      document.body.style.userSelect = "";
      setListH((h) => { localStorage.setItem("roster-devices-list-h", String(h)); return h; });
    };
    document.body.style.userSelect = "none";
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  };

  // Suche serverseitig (debounced) – deckt Hostname, IP/MAC, OS, Seriennr.,
  // installierte Software und Custom-Field-Werte ab.
  const [dq, setDq] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDq(q.trim()), 300);
    return () => clearTimeout(t);
  }, [q]);

  const { data, isLoading, error } = useQuery({
    queryKey: ["devices", dq],
    queryFn: () => api.get<Device[]>(`/devices${dq ? `?q=${encodeURIComponent(dq)}` : ""}`),
    refetchInterval: 15000,
  });
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<Tree>("/clients") });
  // Alle Policy-Checks (für die Check-Bedingung im Filter); ohne Rechte auf die
  // Richtlinien bleiben die gerade fehlschlagenden Checks aus der Liste als Auswahl.
  const { data: policies } = useQuery({ queryKey: ["policies"], queryFn: () => api.get<Policy[]>("/policies"), retry: false });

  // Fehlschlagende Checks über alle geladenen Geräte: Name + Anzahl betroffener Geräte.
  const failingChecks = useMemo(() => {
    const m = new Map<string, { id: string; name: string; count: number }>();
    for (const d of data ?? []) for (const fc of d.failing_checks ?? []) {
      const e = m.get(fc.id) ?? { id: fc.id, name: fc.name, count: 0 };
      e.count++; m.set(fc.id, e);
    }
    return [...m.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [data]);
  const checkOptions = useMemo<CheckOption[]>(() => {
    const m = new Map<string, CheckOption>();
    for (const p of policies ?? []) for (const c of p.checks ?? []) m.set(c.id, { id: c.id, name: `${c.name} (${p.name})` });
    for (const fc of failingChecks) if (!m.has(fc.id)) m.set(fc.id, { id: fc.id, name: fc.name });
    return [...m.values()].sort((a, b) => a.name.localeCompare(b.name));
  }, [policies, failingChecks]);

  // Zustands-Filter über die URL (z. B. Klick auf eine Dashboard-Kachel oder
  // „Fehlerhafter Check…“: filter=check:<id>).
  const [params, setParams] = useSearchParams();
  const health = params.get("filter") || "";
  const healthCheck = health.startsWith("check:") ? health.slice(6) : "";
  const matchesHealth = (d: Device) => {
    if (healthCheck) return (d.failing_checks ?? []).some((fc) => fc.id === healthCheck);
    switch (health) {
      case "failing-checks": return (d.checks_failing ?? 0) > 0;
      case "failing-tasks": return (d.tasks_failing ?? 0) > 0;
      case "vulns": return (d.vuln_count ?? 0) > 0;
      default: return true;
    }
  };
  const healthLabel: Record<string, string> = {
    "failing-checks": t("Nur Geräte mit fehlerhaften Checks"),
    "failing-tasks": t("Nur Geräte mit fehlerhaften Tasks"),
    "vulns": t("Nur Geräte mit Schwachstellen"),
  };
  if (healthCheck) {
    const name = failingChecks.find((fc) => fc.id === healthCheck)?.name ?? checkOptions.find((c) => c.id === healthCheck)?.name ?? healthCheck;
    healthLabel[health] = t("Check fehlerhaft: {name}", { name });
  }
  const setHealth = (v: string) => { const p = new URLSearchParams(params); if (v) p.set("filter", v); else p.delete("filter"); setParams(p, { replace: true }); };
  const clearHealth = () => setHealth("");

  // Eigener, benannter Filter (clientseitig, Bedingungen mit UND/ODER).
  const [filter, setFilter] = useState<DFilter>({ match: "all", conditions: [] });
  const [showFilter, setShowFilter] = useState(false);
  const filterActive = filter.conditions.length > 0;

  const devices = (data ?? []).filter((d) => matchesOrg(d, org) && matchesHealth(d) && evalFilter(d, filter));

  const online = devices.filter((d) => d.status === "online").length;

  // Mehrfachauswahl (Checkbox je Zeile) für Sammelaktionen auf genau diesen Geräten –
  // typisch: nach „Neustart ausstehend“ filtern, alle markieren, neu starten.
  const [sel, setSel] = useState<Set<string>>(new Set());
  const selectable = (d: Device) => d.managed !== false && !d.revoked && d.status !== "unmanaged";
  const visibleIds = devices.filter(selectable).map((d) => d.id);
  const allVisible = visibleIds.length > 0 && visibleIds.every((id) => sel.has(id));
  const toggleOne = (id: string) => setSel((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n; });
  const toggleAll = () => setSel((s) => {
    const n = new Set(s);
    if (allVisible) visibleIds.forEach((id) => n.delete(id)); else visibleIds.forEach((id) => n.add(id));
    return n;
  });
  // Geräte, die aus der Liste verschwinden (gelöscht, widerrufen), fallen aus der Auswahl.
  useEffect(() => {
    if (!data) return;
    const known = new Set(data.map((d) => d.id));
    setSel((s) => { const n = new Set([...s].filter((id) => known.has(id))); return n.size === s.size ? s : n; });
  }, [data]);

  const qc = useQueryClient();
  const [bulkMsg, setBulkMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const bulk = useMutation({
    mutationFn: (action: "reboot" | "scan-updates" | "install-updates") =>
      api.post<{ queued: number }>(`/bulk/${action}`, { target_type: "devices", device_ids: [...sel] }),
    onSuccess: (r) => {
      setBulkMsg({ ok: true, text: t("Auf {n} Gerät(en) eingereiht. Offline-Geräte holen den Befehl beim nächsten Checkin ab.", { n: r.queued }) });
      setSel(new Set());
      qc.invalidateQueries({ queryKey: ["devices"] });
    },
    onError: (e) => setBulkMsg({ ok: false, text: e instanceof ApiError ? e.message : t("Fehler") }),
  });
  // Export der markierten Geräte als CSV: Dialog mit Spaltenauswahl.
  const [exportOpen, setExportOpen] = useState(false);

  // Sichtbare Spalten (Benutzereinstellung, serverseitig). Benutzerdefinierte Felder
  // werden als Spalten angeboten; ihre Werte kommen aus dem Sammelabruf, der nur
  // läuft, wenn mindestens eine Feldspalte eingeblendet ist.
  const [colKeys, setColKeys] = usePref<string[]>("devices.columns", DEFAULT_COLUMNS);
  const [colsOpen, setColsOpen] = useState(false);
  const { data: fieldDefs } = useQuery({
    queryKey: ["custom-fields", "device"],
    queryFn: () => api.get<CustomField[]>("/custom-fields?model=device"),
    retry: false,
    staleTime: 60_000,
  });
  const anyFieldCol = colKeys.some((k) => k.startsWith(CF_PREFIX));
  const { data: fieldVals } = useQuery({
    queryKey: ["custom-field-values", "device", "all"],
    queryFn: () => api.get<EntityFieldValue[]>("/custom-field-values?model=device"),
    enabled: anyFieldCol,
    retry: false,
    refetchInterval: 15000,
  });
  const fieldMap = useMemo(() => {
    const m = new Map<string, string>();
    for (const v of fieldVals ?? []) m.set(v.entity_id + "\0" + v.field.id, v.value);
    return m;
  }, [fieldVals]);
  const allColumns = useMemo(() => [...baseDeviceColumns(t), ...fieldDeviceColumns(fieldDefs ?? [], t)], [t, fieldDefs]);
  const columns = useMemo(() => resolveColumns(colKeys, allColumns), [colKeys, allColumns]);
  const colCtx: ColumnCtx = {
    t,
    jump: (d, tab) => { setSelectedId(d.id); setJump({ tab, n: jump.n + 1 }); },
    fieldValue: (devID, fieldID) => fieldMap.get(devID + "\0" + fieldID),
  };
  const runBulk = (action: "reboot" | "scan-updates" | "install-updates") => {
    setBulkMsg(null);
    const n = sel.size;
    if (action === "reboot" && !window.confirm(t("{n} ausgewählte Geräte jetzt neu starten?", { n }))) return;
    if (action === "install-updates" && !window.confirm(t("Updates auf {n} ausgewählten Geräten installieren? Das kann Neustarts auslösen.", { n }))) return;
    bulk.mutate(action);
  };

  return (
    <div className="page devices-layout">
      <aside>
        {tree && (
          <ClientTree tree={tree} total={data?.length ?? 0} selected={org} onSelect={setOrg} isAdmin={isAdmin} />
        )}
      </aside>

      <div className="devices-main">
        <header className="page-head">
          <div>
            <h1>{t("Geräte")}</h1>
            <p className="muted">
              {t("{n} angezeigt · {online} online", { n: devices.length, online })}
              {health && healthLabel[health] && (
                <button className="filter-chip" onClick={clearHealth} title={t("Filter entfernen")}>
                  {healthLabel[health]} ✕
                </button>
              )}
              {filterActive && (
                <button className="filter-chip" onClick={() => setFilter({ match: "all", conditions: [] })} title={t("Filter entfernen")}>
                  {t("{n} Filterbedingung(en)", { n: filter.conditions.length })} ✕
                </button>
              )}
            </p>
          </div>
          <div className="head-actions">
            <input className="search" placeholder={t("Suche: Hostname, IP, OS, Software, Custom Fields…")} value={q} onChange={(e) => setQ(e.target.value)} style={{ minWidth: 280 }} />
            {failingChecks.length > 0 && (
              <select value={healthCheck ? health : ""} onChange={(e) => setHealth(e.target.value)} title={t("Nur Geräte anzeigen, bei denen dieser Check fehlschlägt")}>
                <option value="">{t("Fehlerhafter Check…")}</option>
                {failingChecks.map((fc) => <option key={fc.id} value={`check:${fc.id}`}>{fc.name} ({fc.count})</option>)}
              </select>
            )}
            <button className={`btn ghost${filterActive ? " active" : ""}`} onClick={() => setShowFilter((v) => !v)}>⛃ {t("Filter")}</button>
            <button className="btn ghost" onClick={() => setColsOpen(true)} title={t("Spalten der Liste wählen")}>☷ {t("Spalten")}</button>
            {isAdmin && <button className="btn primary" onClick={() => setShowAdd(true)}>{t("+ Neuer Computer")}</button>}
          </div>
        </header>

        {showFilter && <DeviceFilter value={filter} onChange={setFilter} checks={checkOptions} />}

        {(sel.size > 0 || bulkMsg) && (
          <div className="bulk-bar">
            {sel.size > 0 && <strong>{t("{n} ausgewählt", { n: sel.size })}</strong>}
            {sel.size > 0 && (
              <>
                <button className="btn sm" disabled={bulk.isPending} onClick={() => runBulk("reboot")}>⟳ {t("Neustart")}</button>
                <button className="btn sm ghost" disabled={bulk.isPending} onClick={() => runBulk("scan-updates")}>{t("Updates prüfen")}</button>
                <button className="btn sm ghost" disabled={bulk.isPending} onClick={() => runBulk("install-updates")}>{t("Updates durchführen")}</button>
                <button className="btn sm ghost" onClick={() => { setBulkMsg(null); setExportOpen(true); }} title={t("Markierte Geräte als CSV-Datei herunterladen (Excel-kompatibel, inkl. benutzerdefinierter Felder)")}>⤓ {t("Export (CSV)")}</button>
                <button className="btn sm ghost" onClick={() => { setSel(new Set()); setBulkMsg(null); }}>{t("Auswahl aufheben")}</button>
              </>
            )}
            {bulkMsg && <span className={bulkMsg.ok ? "form-ok" : "form-err"}>{bulkMsg.text}</span>}
            {bulkMsg && sel.size === 0 && <button className="btn sm ghost" onClick={() => setBulkMsg(null)}>✕</button>}
          </div>
        )}

        {showAdd && <AddComputerDialog onClose={() => setShowAdd(false)} />}
        {colsOpen && <ColumnsDialog all={allColumns} value={columns.map((c) => c.key)} onChange={setColKeys} onClose={() => setColsOpen(false)} />}
        {exportOpen && (
          <ExportDialog devices={devices.filter((d) => sel.has(d.id))} onClose={() => setExportOpen(false)}
            onDone={(n) => { setExportOpen(false); setBulkMsg({ ok: true, text: t("{n} Gerät(e) als CSV exportiert.", { n }) }); }} />
        )}
        {isLoading && <div className="muted">{t("Lädt…")}</div>}
        {error && <div className="form-error">{t("Fehler beim Laden.")}</div>}

        {data && (
          <div className="devices-split">
            <div className="devices-table-wrap" style={{ height: listH, maxHeight: "none" }}>
              <table className="table selectable">
                <thead>
                  <tr>
                    <th className="sel">
                      <input type="checkbox" checked={allVisible} disabled={visibleIds.length === 0} title={t("Alle sichtbaren auswählen")}
                        ref={(el) => { if (el) el.indeterminate = !allVisible && visibleIds.some((id) => sel.has(id)); }}
                        onChange={toggleAll} />
                    </th>
                    {columns.map((c) => <th key={c.key}>{c.label}</th>)}
                  </tr>
                </thead>
                <tbody>
                  {devices.map((d) => (
                    <tr key={d.id} className={selectedId === d.id ? "row-selected" : ""} onClick={() => setSelectedId(d.id)}>
                      <td className="sel" onClick={(e) => e.stopPropagation()}>
                        {selectable(d) && <input type="checkbox" checked={sel.has(d.id)} onChange={() => toggleOne(d.id)} />}
                      </td>
                      {columns.map((c) => <Fragment key={c.key}>{c.render(d, colCtx)}</Fragment>)}
                    </tr>
                  ))}
                  {devices.length === 0 && (
                    <tr><td colSpan={columns.length + 1} className="empty">{t("Keine Geräte gefunden.")}</td></tr>
                  )}
                </tbody>
              </table>
            </div>

            <div className="devices-resizer" onMouseDown={startResize} title={t("Ziehen zum Anpassen")}>
              <span className="devices-resizer-grip" />
            </div>

            {selectedId && data.some((d) => d.id === selectedId) ? (
              <div className="devices-detail-wrap">
                <DevicePanel id={selectedId} focusTab={jump.tab} focusKey={jump.n} />
              </div>
            ) : (
              <div className="devices-detail-wrap empty-panel muted">{t("Gerät auswählen, um Details zu sehen.")}</div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
