import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { api, ApiError } from "../api";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { SeverityBadge } from "../components/StatusBadge";
import type { ClientTree, PatchSummary, PendingPatch } from "../types";

// Patches ist die flottenweite Patch-Seite (PatchMon-Stil): Kopfzahlen, ausstehende
// Updates aller Geräte (Sicherheits-Updates hervorgehoben), Filter, Mehrfachauswahl
// mit Genehmigen/Installieren. Weitere Tabs (Verlauf, Pakete, Repositories,
// Lebenszyklus) hängen sich hier ein.
type PatchTab = "pending";
const TABS: { key: PatchTab; label: string }[] = [
  { key: "pending", label: "Ausstehend" },
];

const rowKey = (p: PendingPatch) => `${p.device_id}\u0000${p.name}`;

export function Patches() {
  const { t } = useI18n();
  const [params, setParams] = useSearchParams();
  const tab = (params.get("tab") as PatchTab) || "pending";
  const setTab = (k: PatchTab) => { const p = new URLSearchParams(params); p.set("tab", k); setParams(p, { replace: true }); };

  return (
    <div className="page wide">
      <header className="page-head">
        <div>
          <h1>{t("Patches")}</h1>
          <p className="muted">{t("Ausstehende Updates aller Geräte – Sicherheits-Updates zuerst. Genehmigen und installieren direkt aus der Liste.")}</p>
        </div>
      </header>
      <div className="tabs" style={{ marginTop: 0 }}>
        {TABS.map((tb) => (
          <button key={tb.key} className={`tab ${tab === tb.key ? "tab-on" : ""}`} onClick={() => setTab(tb.key)}>{t(tb.label)}</button>
        ))}
      </div>
      {tab === "pending" && <PendingTab />}
    </div>
  );
}

function Tile({ label, value, sub, tone }: { label: string; value: number | string; sub?: string; tone?: "ok" | "warn" | "bad" }) {
  return (
    <div className={`dash-tile${tone ? ` dash-${tone}` : ""}`}>
      <div className="dash-value">{value}</div>
      <div className="dash-label">{label}</div>
      {sub && <div className="muted small">{sub}</div>}
    </div>
  );
}

function PendingTab() {
  const { t } = useI18n();
  const { hasPerm } = useAuth();
  const canOperate = hasPerm("devices.operate");
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const securityOnly = params.get("security") === "1";
  const approvedOnly = params.get("approved") === "1";
  const client = params.get("client") || "";
  const site = params.get("site") || "";
  const [q, setQ] = useState(params.get("q") || "");
  const setParam = (k: string, v: string) => {
    const p = new URLSearchParams(params);
    if (v) p.set(k, v); else p.delete(k);
    setParams(p, { replace: true });
  };

  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: summary } = useQuery({ queryKey: ["patch-summary"], queryFn: () => api.get<PatchSummary>("/patches/summary"), refetchInterval: 30000 });
  const listParams = new URLSearchParams();
  if (securityOnly) listParams.set("security", "1");
  if (approvedOnly) listParams.set("approved", "1");
  if (client) listParams.set("client", client);
  if (site) listParams.set("site", site);
  const listKey = listParams.toString();
  const { data: rows, isLoading } = useQuery({
    queryKey: ["patches-pending", listKey],
    queryFn: () => api.get<PendingPatch[]>(`/patches/pending${listKey ? "?" + listKey : ""}`),
    refetchInterval: 30000,
  });

  // Textfilter clientseitig (Paketname, Hostname, Quelle) – die Liste ist bereits gefiltert geladen.
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const all = rows ?? [];
    if (!needle) return all;
    return all.filter((p) => p.name.toLowerCase().includes(needle) || p.hostname.toLowerCase().includes(needle) || (p.source ?? "").toLowerCase().includes(needle));
  }, [rows, q]);

  // Gruppierung je Gerät (Reihenfolge: Geräte mit Sicherheits-Updates zuerst, dann Hostname).
  const groups = useMemo(() => {
    const m = new Map<string, { device: PendingPatch; items: PendingPatch[]; security: number; approved: number }>();
    for (const p of filtered) {
      let g = m.get(p.device_id);
      if (!g) { g = { device: p, items: [], security: 0, approved: 0 }; m.set(p.device_id, g); }
      g.items.push(p);
      if (p.security) g.security++;
      if (p.approved) g.approved++;
    }
    return [...m.values()].sort((a, b) => (b.security > 0 ? 1 : 0) - (a.security > 0 ? 1 : 0) || a.device.hostname.localeCompare(b.device.hostname));
  }, [filtered]);

  const [open, setOpen] = useState<Set<string>>(new Set());
  const [allOpen, setAllOpen] = useState(false);
  const isOpen = (id: string) => allOpen ? !open.has(id) : open.has(id);
  const toggleOpen = (id: string) => setOpen((s) => { const n = new Set(s); if (n.has(id)) n.delete(id); else n.add(id); return n; });

  const [sel, setSel] = useState<Set<string>>(new Set());
  const visibleKeys = useMemo(() => filtered.map(rowKey), [filtered]);
  const allSelected = visibleKeys.length > 0 && visibleKeys.every((k) => sel.has(k));
  const toggleAll = () => setSel(allSelected ? new Set() : new Set(visibleKeys));
  const toggleDevice = (items: PendingPatch[]) => {
    const keys = items.map(rowKey);
    const every = keys.every((k) => sel.has(k));
    setSel((s) => { const n = new Set(s); for (const k of keys) { if (every) n.delete(k); else n.add(k); } return n; });
  };
  const toggleRow = (k: string) => setSel((s) => { const n = new Set(s); if (n.has(k)) n.delete(k); else n.add(k); return n; });
  const selectedRows = useMemo(() => filtered.filter((p) => sel.has(rowKey(p))), [filtered, sel]);
  const selectedDevices = useMemo(() => [...new Set(selectedRows.map((p) => p.device_id))], [selectedRows]);

  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const invalidate = () => { qc.invalidateQueries({ queryKey: ["patches-pending"] }); qc.invalidateQueries({ queryKey: ["patch-summary"] }); qc.invalidateQueries({ queryKey: ["devices"] }); };
  const approve = useMutation({
    mutationFn: (approved: boolean) => api.put<{ changed: number; skipped: number }>("/patches/approve", {
      items: selectedRows.map((p) => ({ device_id: p.device_id, name: p.name })), approved,
    }),
    onSuccess: (r, approved) => { setMsg({ ok: true, text: approved ? t("{n} Patch(es) genehmigt.", { n: r.changed }) : t("Genehmigung für {n} Patch(es) aufgehoben.", { n: r.changed }) }); invalidate(); },
    onError: (e) => setMsg({ ok: false, text: e instanceof ApiError ? e.message : t("Fehler") }),
  });
  const [installMode, setInstallMode] = useState<"approved" | "security" | "approved_security" | "all">("approved");
  const [aptMode, setAptMode] = useState<"full" | "safe">("full");
  const install = useMutation({
    mutationFn: (deviceIds: string[]) => api.post<{ queued: number; skipped: number }>("/bulk/install-updates", {
      target_type: "devices", device_ids: deviceIds,
      approved: installMode === "approved" || installMode === "approved_security",
      security_only: installMode === "security" || installMode === "approved_security",
      apt_mode: aptMode,
    }),
    onSuccess: (r) => { setMsg({ ok: true, text: t("Installation auf {n} Gerät(en) eingereiht, {s} übersprungen (keine passenden Updates).", { n: r.queued, s: r.skipped }) }); setSel(new Set()); invalidate(); },
    onError: (e) => setMsg({ ok: false, text: e instanceof ApiError ? e.message : t("Fehler") }),
  });
  const runInstall = () => {
    setMsg(null);
    const ids = selectedDevices.length > 0 ? selectedDevices : groups.map((g) => g.device.device_id);
    if (ids.length === 0) return;
    const scope = selectedDevices.length > 0 ? t("{n} ausgewählten Gerät(en)", { n: ids.length }) : t("allen {n} angezeigten Geräten", { n: ids.length });
    const what = installMode === "all" ? t("ALLE ausstehenden Updates") : installMode === "security" ? t("die Sicherheits-Updates")
      : installMode === "approved_security" ? t("die genehmigten Sicherheits-Updates") : t("die genehmigten Updates");
    if (!window.confirm(t("Auf {scope} {what} installieren? Das kann Neustarts auslösen.", { scope, what }))) return;
    install.mutate(ids);
  };
  const scan = useMutation({
    mutationFn: () => api.post<{ queued: number }>("/bulk/scan-updates", { target_type: "all", target_id: "" }),
    onSuccess: (r) => setMsg({ ok: true, text: t("Update-Scan auf {n} Gerät(en) eingereiht.", { n: r.queued }) }),
    onError: (e) => setMsg({ ok: false, text: e instanceof ApiError ? e.message : t("Fehler") }),
  });

  const clients = tree?.clients ?? [];
  const sites = (client ? clients.filter((c) => c.id === client) : clients).flatMap((c) => (c.sites ?? []).map((s) => ({ id: s.id, name: client ? s.name : `${c.name} › ${s.name}` })));

  return (
    <>
      {summary && (
        <div className="dash-grid" style={{ marginBottom: 16 }}>
          <Tile label={t("Geräte mit Sicherheits-Updates")} value={summary.devices_security} sub={t("von {n} geprüften", { n: summary.devices_checked })} tone={summary.devices_security > 0 ? "bad" : "ok"} />
          <Tile label={t("Sicherheits-Updates")} value={summary.security} tone={summary.security > 0 ? "bad" : "ok"} />
          <Tile label={t("Ausstehende Updates")} value={summary.pending} sub={t("{n} Gerät(e)", { n: summary.devices_pending })} tone={summary.pending > 0 ? "warn" : "ok"} />
          <Tile label={t("Genehmigt, nicht installiert")} value={summary.approved} tone={summary.approved > 0 ? "warn" : undefined} />
          <Tile label={t("Ohne Sicherheits-Updates")} value={`${summary.compliant_pct}%`} sub={t("der geprüften Geräte")} tone={summary.compliant_pct >= 90 ? "ok" : summary.compliant_pct >= 60 ? "warn" : "bad"} />
          <Tile label={t("Noch nie geprüft")} value={summary.devices_total - summary.devices_checked} tone={summary.devices_total - summary.devices_checked > 0 ? "warn" : undefined} />
        </div>
      )}

      <section className="card">
        <div className="page-head" style={{ marginBottom: 10 }}>
          <div className="head-actions" style={{ flexWrap: "wrap" }}>
            <input className="search" placeholder={t("Paket, Host oder Quelle suchen…")} value={q} onChange={(e) => { setQ(e.target.value); setParam("q", e.target.value); }} />
            <select value={client} onChange={(e) => { setParam("client", e.target.value); setParam("site", ""); }}>
              <option value="">{t("Alle Clients")}</option>
              {clients.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
            <select value={site} onChange={(e) => setParam("site", e.target.value)}>
              <option value="">{t("Alle Standorte")}</option>
              {sites.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
            <label className={`chip ${securityOnly ? "chip-on" : ""}`}><input type="checkbox" checked={securityOnly} onChange={(e) => setParam("security", e.target.checked ? "1" : "")} /> 🛡 {t("nur Sicherheit")}</label>
            <label className={`chip ${approvedOnly ? "chip-on" : ""}`}><input type="checkbox" checked={approvedOnly} onChange={(e) => setParam("approved", e.target.checked ? "1" : "")} /> {t("nur genehmigte")}</label>
            <button className="btn ghost sm" onClick={() => setAllOpen((v) => { setOpen(new Set()); return !v; })}>{allOpen ? t("Alle zuklappen") : t("Alle aufklappen")}</button>
          </div>
          {canOperate && (
            <div className="actions">
              <button className="btn ghost sm" disabled={scan.isPending} onClick={() => scan.mutate()} title={t("Update-Scan auf allen Geräten anstoßen")}>↻ {t("Alle prüfen")}</button>
            </div>
          )}
        </div>

        {canOperate && (
          <div className="bulk-bar">
            <strong>{sel.size > 0 ? t("{n} ausgewählt auf {d} Gerät(en)", { n: sel.size, d: selectedDevices.length }) : t("Keine Auswahl – Installation gilt für alle angezeigten Geräte")}</strong>
            <button className="btn sm" disabled={sel.size === 0 || approve.isPending} onClick={() => { setMsg(null); approve.mutate(true); }}>✓ {t("Genehmigen")}</button>
            <button className="btn sm ghost" disabled={sel.size === 0 || approve.isPending} onClick={() => { setMsg(null); approve.mutate(false); }}>{t("Genehmigung aufheben")}</button>
            <span className="muted">|</span>
            <select value={installMode} onChange={(e) => setInstallMode(e.target.value as typeof installMode)}>
              <option value="approved">{t("Genehmigte installieren")}</option>
              <option value="security">{t("Sicherheits-Updates installieren")}</option>
              <option value="approved_security">{t("Genehmigte Sicherheits-Updates installieren")}</option>
              <option value="all">{t("Alle Updates installieren")}</option>
            </select>
            <select value={aptMode} onChange={(e) => setAptMode(e.target.value as "full" | "safe")} title={t("apt-Strategie (nur Debian/Ubuntu)")}>
              <option value="full">{t("Voll-Upgrade (dist-upgrade)")}</option>
              <option value="safe">{t("Sicheres Upgrade (upgrade)")}</option>
            </select>
            <button className="btn sm primary" disabled={install.isPending || groups.length === 0} onClick={runInstall}>⬇ {t("Installieren")}</button>
            {sel.size > 0 && <button className="btn sm ghost" onClick={() => setSel(new Set())}>{t("Auswahl aufheben")}</button>}
          </div>
        )}
        {msg && <p className={msg.ok ? "form-ok" : "form-err"}>{msg.text}</p>}

        {isLoading ? <p className="muted">{t("Lädt…")}</p> : groups.length === 0 ? (
          <p className="empty">{securityOnly ? t("Keine ausstehenden Sicherheits-Updates.") : t("Keine ausstehenden Updates – alle geprüften Geräte sind aktuell.")}</p>
        ) : (
          <table className="table">
            <thead>
              <tr>
                {canOperate && <th style={{ width: 28 }}><input type="checkbox" checked={allSelected} onChange={toggleAll} title={t("Alle sichtbaren auswählen")} /></th>}
                <th>{t("Gerät")}</th>
                <th>{t("Firma / Standort")}</th>
                <th>{t("Updates")}</th>
                <th>{t("Sicherheit")}</th>
                <th>{t("Genehmigt")}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => {
                const id = g.device.device_id;
                const keys = g.items.map(rowKey);
                const devSel = keys.every((k) => sel.has(k));
                const devPart = !devSel && keys.some((k) => sel.has(k));
                return [
                  <tr key={id} className={isOpen(id) ? "row-selected" : undefined} style={{ cursor: "pointer" }} onClick={() => toggleOpen(id)}>
                    {canOperate && <td onClick={(e) => e.stopPropagation()}><input type="checkbox" checked={devSel} ref={(el) => { if (el) el.indeterminate = devPart; }} onChange={() => toggleDevice(g.items)} /></td>}
                    <td><span className="muted small">{isOpen(id) ? "▾" : "▸"}</span> <Link to={`/devices/${id}`} className="link-strong" onClick={(e) => e.stopPropagation()}>{g.device.hostname}</Link> <span className="muted small">{g.device.os}</span></td>
                    <td className="muted">{g.device.client_name ? `${g.device.client_name} › ${g.device.site_name}` : "—"}</td>
                    <td><span className="badge badge-warn">{g.items.length}</span></td>
                    <td>{g.security > 0 ? <span className="badge badge-offline">🛡 {g.security}</span> : <span className="muted">—</span>}</td>
                    <td className="muted">{g.approved}/{g.items.length}</td>
                    <td></td>
                  </tr>,
                  ...(isOpen(id) ? g.items.map((p) => {
                    const k = rowKey(p);
                    return (
                      <tr key={k} className="small">
                        {canOperate && <td><input type="checkbox" checked={sel.has(k)} onChange={() => toggleRow(k)} /></td>}
                        <td style={{ paddingLeft: 28 }}>{p.name}{p.kind === "driver" && <span className="muted"> · {t("Treiber")}</span>}{p.kind === "definition" && <span className="muted"> · {t("Definitionen")}</span>}</td>
                        <td className="mono muted">{p.current_version ? <>{p.current_version} → </> : null}{p.version || "—"}</td>
                        <td className="muted">{p.source || "—"}</td>
                        <td><SeverityBadge severity={p.severity} security={p.security} /></td>
                        <td>{p.approved ? <span className="badge badge-online">✓</span> : <span className="muted">—</span>}</td>
                        <td>{p.url ? <a className="link-strong" href={p.url} target="_blank" rel="noreferrer">↗</a> : null}</td>
                      </tr>
                    );
                  }) : []),
                ];
              })}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}
