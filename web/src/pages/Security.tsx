import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import { api, ApiError } from "../api";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { FimEventsTable } from "../components/FimEvents";
import { AuthEventsTable, IncidentsTable, INCIDENT_KIND } from "../components/SecurityEvents";
import { FleetCompliance } from "../components/Compliance";
import type { AuthEvent, ClientTree, Device, FIMEvent, IPBlock, SecurityIncident, SecuritySummary, IncidentKindInfo } from "../types";
import { BlockDurationSelect, IncidentActions, durationLabel } from "../components/IncidentActions";
import { CountryChips, Flag, countryFlag, countryName } from "../components/Country";

// Security ist die flottenweite Sicherheitsseite (Wazuh-Stil): Datei-Integrität,
// später Vorfälle, Anmeldungen, Compliance und Sperren.
type SecTab = "incidents" | "auth" | "fim" | "compliance" | "blocks";
const TABS: { key: SecTab; label: string }[] = [
  { key: "incidents", label: "Vorfälle" },
  { key: "auth", label: "Anmeldungen" },
  { key: "fim", label: "Datei-Integrität" },
  { key: "compliance", label: "Compliance" },
  { key: "blocks", label: "Sperren" },
];

export function Security() {
  const { t } = useI18n();
  const [params, setParams] = useSearchParams();
  const tab = (params.get("tab") as SecTab) || "incidents";
  const setTab = (k: SecTab) => { const p = new URLSearchParams(params); p.set("tab", k); setParams(p, { replace: true }); };
  return (
    <div className="page wide">
      <header className="page-head">
        <div>
          <h1>{t("Sicherheit")}</h1>
          <p className="muted">{t("Datei-Integrität, Anmeldungen und Compliance aller Geräte – konfiguriert über den Bereich „Sicherheitsüberwachung“ der Richtlinien.")}</p>
        </div>
      </header>
      <div className="tabs" style={{ marginTop: 0 }}>
        {TABS.map((tb) => <button key={tb.key} className={`tab ${tab === tb.key ? "tab-on" : ""}`} onClick={() => setTab(tb.key)}>{t(tb.label)}</button>)}
      </div>
      {tab === "incidents" && <IncidentsTab />}
      {tab === "auth" && <AuthTab />}
      {tab === "fim" && <FimTab />}
      {tab === "compliance" && <FleetCompliance />}
      {tab === "blocks" && <BlocksTab />}
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

function IncidentsTab() {
  const { t } = useI18n();
  const { hasPerm } = useAuth();
  const [params, setParams] = useSearchParams();
  const status = params.get("status") ?? "open";
  const kind = params.get("kind") || "";
  const severity = params.get("severity") || "";
  const days = params.get("days") || "30";
  const client = params.get("client") || "";
  const country = params.get("country") || "";
  const technique = params.get("technique") || "";
  const [q, setQ] = useState("");
  const setParam = (k: string, v: string) => { const p = new URLSearchParams(params); if (v) p.set(k, v); else p.delete(k); setParams(p, { replace: true }); };
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: summary } = useQuery({ queryKey: ["security-summary"], queryFn: () => api.get<SecuritySummary>("/security/summary"), refetchInterval: 30000 });
  const qs = new URLSearchParams({ days });
  if (status) qs.set("status", status);
  if (kind) qs.set("kind", kind);
  if (severity) qs.set("severity", severity);
  if (client) qs.set("client", client);
  if (country) qs.set("country", country);
  if (technique) qs.set("technique", technique);
  if (q.trim()) qs.set("q", q.trim());
  const key = qs.toString();
  const { data: kinds } = useQuery({ queryKey: ["incident-kinds"], queryFn: () => api.get<IncidentKindInfo[]>("/security/kinds"), staleTime: 600000 });
  const techniques = [...new Map((kinds ?? []).flatMap((k) => k.techniques).map((m) => [m.id, m])).values()].sort((a, b) => a.id.localeCompare(b.id));
  const { data: rows, isLoading } = useQuery({ queryKey: ["incidents", "fleet", key], queryFn: () => api.get<SecurityIncident[]>(`/security/incidents?${key}`), refetchInterval: 30000 });
  // Länder-Auswahl: alle Länder aus Zusammenfassung und geladenen Vorfällen, gesammelt über die Zeit,
  // damit die Liste beim Filtern nicht zusammenschrumpft.
  const { lang } = useI18n();
  const seenCountries = useRef(new Set<string>());
  for (const c of [...(summary?.top_countries ?? []), ...(summary?.blocked_countries ?? [])]) if (c.country) seenCountries.current.add(c.country);
  for (const r of rows ?? []) if (r.country) seenCountries.current.add(r.country);
  if (country && country !== "?") seenCountries.current.add(country);
  const countryOptions = [...seenCountries.current].map((cc) => ({ cc, name: countryName(cc, lang, cc) })).sort((a, b) => a.name.localeCompare(b.name));
  // Mehrfachauswahl: Quell-IPs mehrerer Vorfälle auf einmal sperren (je Gerät oder auf allen
  // überwachten Geräten) und die Vorfälle dabei bestätigen.
  const qc = useQueryClient();
  const [sel, setSel] = useState<Set<string>>(new Set());
  const [bulkMin, setBulkMin] = useState(0);
  const [bulkFleet, setBulkFleet] = useState(true);
  const [bulkMsg, setBulkMsg] = useState("");
  const canOperate = hasPerm("devices.operate");
  const bulkBlock = useMutation({
    mutationFn: async () => {
      const picked = (rows ?? []).filter((i) => sel.has(i.id) && i.source_ip);
      const seen = new Set<string>();
      let n = 0;
      for (const i of picked) {
        const k = `${bulkFleet ? "*" : i.device_id}|${i.source_ip}`;
        if (!seen.has(k)) {
          seen.add(k);
          await api.post(`/devices/${i.device_id}/block-ip`, { ip: i.source_ip, minutes: bulkMin, reason: i.title, fleet: bulkFleet });
          n++;
        }
        if (i.status === "open") await api.put(`/security/incidents/${i.id}`, { status: "acknowledged" });
      }
      return n;
    },
    onSuccess: (n) => { setBulkMsg(t("{n} Sperren eingereiht, Vorfälle bestätigt", { n })); setSel(new Set()); qc.invalidateQueries({ queryKey: ["ip-blocks"] }); qc.invalidateQueries({ queryKey: ["incidents"] }); qc.invalidateQueries({ queryKey: ["security-summary"] }); },
    onError: (e) => setBulkMsg(e instanceof ApiError ? e.message : t("Fehler")),
  });
  const toggleSel = (id: string, on: boolean) => setSel((prev) => { const n = new Set(prev); if (on) n.add(id); else n.delete(id); return n; });
  const toggleAll = (on: boolean) => setSel(on ? new Set((rows ?? []).filter((i) => i.source_ip).map((i) => i.id)) : new Set());
  return (
    <>
      {summary && (
        <div className="dash-grid" style={{ marginBottom: 16 }}>
          <Tile label={t("Offene Vorfälle")} value={summary.open_incidents} sub={t("{n} kritisch", { n: summary.critical_open })} tone={summary.critical_open > 0 ? "bad" : summary.open_incidents > 0 ? "warn" : "ok"} />
          <Tile label={t("Fehlgeschlagene Anmeldungen (24 h)")} value={summary.failed_logins_24h} tone={summary.failed_logins_24h > 50 ? "warn" : undefined} />
          <Tile label={t("Überwachte Geräte")} value={summary.devices_monitored} />
          <div className="dash-tile" style={{ gridColumn: "span 2" }}>
            <div className="dash-label">{t("Häufigste Quell-IPs (24 h)")}</div>
            <div className="small" style={{ marginTop: 6 }}>
              {summary.top_ips.length === 0 ? <span className="muted">—</span> : summary.top_ips.slice(0, 6).map((x) => (
                <span key={x.ip} className="badge badge-unknown" style={{ marginRight: 6, marginBottom: 4, cursor: "pointer" }} onClick={() => { const p = new URLSearchParams(params); p.set("tab", "auth"); p.set("ip", x.ip); setParams(p); }}><Flag cc={x.country} />{x.ip} · {x.count}</span>
              ))}
            </div>
          </div>
          <div className="dash-tile" style={{ gridColumn: "span 2" }}>
            <div className="dash-label">{t("Angriffsquellen nach Land (24 h)")} <span className="muted">· {t("Klick filtert die Vorfälle")}</span></div>
            <div className="small" style={{ marginTop: 6 }}><CountryChips items={summary.top_countries ?? []} onClick={(cc) => setParam("country", cc || "?")} /></div>
          </div>
          <div className="dash-tile" style={{ gridColumn: "span 2" }}>
            <div className="dash-label">{t("Gesperrte IPs nach Land (30 Tage)")}</div>
            <div className="small" style={{ marginTop: 6 }}><CountryChips items={summary.blocked_countries ?? []} /></div>
            <div className="muted small" style={{ marginTop: 4 }}>{t("Als Diagramm auf der Übersicht: „Diagramm hinzufügen“ → Kennzahl „Gesperrte IPs nach Land“.")}</div>
          </div>
        </div>
      )}
      <section className="card">
        <div className="page-head" style={{ marginBottom: 10 }}>
          <div className="head-actions" style={{ flexWrap: "wrap" }}>
            <input className="search" placeholder={t("IP, Benutzer, Host oder Titel suchen…")} value={q} onChange={(e) => setQ(e.target.value)} />
            <select value={status} onChange={(e) => setParam("status", e.target.value)}>
              <option value="open">{t("offen")}</option>
              <option value="acknowledged">{t("bestätigt")}</option>
              <option value="resolved">{t("erledigt")}</option>
              <option value="">{t("alle")}</option>
            </select>
            <select value={kind} onChange={(e) => setParam("kind", e.target.value)}>
              <option value="">{t("Alle Arten")}</option>
              {Object.entries(INCIDENT_KIND).map(([k, l]) => <option key={k} value={k}>{t(l)}</option>)}
            </select>
            <select value={severity} onChange={(e) => setParam("severity", e.target.value)}>
              <option value="">{t("Alle Schweregrade")}</option>
              <option value="critical">{t("kritisch")}</option>
              <option value="warning">{t("Warnung")}</option>
              <option value="info">{t("Info")}</option>
            </select>
            <select value={days} onChange={(e) => setParam("days", e.target.value)}>
              <option value="1">{t("24 Stunden")}</option>
              <option value="7">{t("7 Tage")}</option>
              <option value="30">{t("30 Tage")}</option>
              <option value="365">{t("1 Jahr")}</option>
            </select>
            <select value={client} onChange={(e) => setParam("client", e.target.value)}>
              <option value="">{t("Alle Clients")}</option>
              {(tree?.clients ?? []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
            <select value={technique} onChange={(e) => setParam("technique", e.target.value)} title={t("MITRE-ATT&CK-Technik")}>
              <option value="">{t("Alle Techniken")}</option>
              {techniques.map((m) => <option key={m.id} value={m.id}>{m.id} {m.name}</option>)}
            </select>
            <select value={country} onChange={(e) => setParam("country", e.target.value)} title={t("Land der Quell-IP")}>
              <option value="">{t("Alle Länder")}</option>
              {countryOptions.map((c) => <option key={c.cc} value={c.cc}>{countryFlag(c.cc)} {c.name}</option>)}
              <option value="?">🌐 {t("Unbekannt")}</option>
            </select>
            {country && <span className="filter-chip" onClick={() => setParam("country", "")}>{country === "?" ? t("Unbekannt") : `${countryFlag(country)} ${countryName(country, lang, country)}`} ✕</span>}
          </div>
          <span className="muted small">{t("{n} Vorfälle", { n: (rows ?? []).length })}</span>
        </div>
        <p className="muted small">{t("Vorfälle entstehen aus den Anmeldeereignissen: Brute-Force (Schwellwert in den Einstellungen), erfolgreiche Anmeldung nach Brute-Force, erste Anmeldung von einer neuen Quell-IP (nach der Lernphase), Angriffsmuster aus Log-Quellen, Kontoänderungen und Sperrungen. Automatische Sperren ohne Klicken: Einstellungen → Sicherheitsüberwachung.")}</p>
        {canOperate && (
          <div className="inline-form" style={{ marginBottom: 8 }}>
            <span className="muted small">{t("{n} ausgewählt", { n: sel.size })}</span>
            <BlockDurationSelect value={bulkMin} onChange={setBulkMin} />
            <label className="chip" title={t("Zusätzlich auf allen Geräten sperren, deren Richtlinie Anmeldungen überwacht oder Log-Quellen liest")}><input type="checkbox" checked={bulkFleet} onChange={(e) => setBulkFleet(e.target.checked)} /> {t("auf allen überwachten Geräten")}</label>
            <button className="btn sm" disabled={sel.size === 0 || bulkBlock.isPending} onClick={() => { if (confirm(t("Quell-IPs von {n} Vorfällen sperren ({d}) und die Vorfälle bestätigen?", { n: sel.size, d: durationLabel(bulkMin, t) }))) { setBulkMsg(""); bulkBlock.mutate(); } }}>🚫 {t("Ausgewählte IPs sperren")}</button>
            {bulkMsg && <span className="muted small">{bulkMsg}</span>}
          </div>
        )}
        {isLoading ? <p className="muted">{t("Lädt…")}</p> : <IncidentsTable incidents={rows ?? []} showDevice canOperate={canOperate} actions={canOperate ? (i) => <IncidentActions incident={i} /> : undefined}
          selected={canOperate ? sel : undefined} onSelect={canOperate ? toggleSel : undefined} onSelectAll={canOperate ? toggleAll : undefined} />}
      </section>
    </>
  );
}

// BlocksTab listet IP-Sperren aller Geräte (automatisch und manuell) mit Aufheben.
// CountryCell zeigt Flagge und Ländername (oder —).
function CountryCell({ cc }: { cc?: string }) {
  const { t, lang } = useI18n();
  if (!cc) return <span className="muted">—</span>;
  return <span><Flag cc={cc} />{countryName(cc, lang, t("Unbekannt"))}</span>;
}

function BlocksTab() {
  const { t } = useI18n();
  const { hasPerm } = useAuth();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const [q, setQ] = useState(params.get("q") ?? "");
  const [dq, setDq] = useState(q);
  useEffect(() => { const h = setTimeout(() => setDq(q.trim()), 300); return () => clearTimeout(h); }, [q]);
  const bStatus = params.get("bstatus") ?? "";
  const setBStatus = (v: string) => { const p = new URLSearchParams(params); if (v) p.set("bstatus", v); else p.delete("bstatus"); setParams(p, { replace: true }); };
  const bq = new URLSearchParams({ limit: "1000" });
  if (dq) bq.set("q", dq);
  if (bStatus) bq.set("status", bStatus);
  const { data: rows, isLoading } = useQuery({ queryKey: ["ip-blocks", bq.toString()], queryFn: () => api.get<IPBlock[]>(`/security/blocks?${bq.toString()}`), refetchInterval: 30000 });
  const unblock = useMutation({
    mutationFn: (b: IPBlock) => api.post(`/devices/${b.device_id}/unblock-ip`, { ip: b.ip }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ip-blocks"] }),
  });
  const unblockAll = useMutation({
    mutationFn: (ip: string) => api.post<{ devices: number }>(`/security/unblock-ip`, { ip }),
    onSuccess: (r) => { setBMsg(t("Auf {n} Geräten aufgehoben", { n: r.devices })); qc.invalidateQueries({ queryKey: ["ip-blocks"] }); },
  });
  const cls: Record<string, string> = { pending: "badge-unknown", active: "badge-offline", expired: "badge-unknown", released: "badge-online", failed: "badge-warn" };
  const label: Record<string, string> = { pending: "eingereiht", active: "aktiv", expired: "abgelaufen", released: "aufgehoben", failed: "fehlgeschlagen" };
  // Manuelle Sperre: Gerät, IP, Dauer (0 = dauerhaft), Grund.
  const canOperate = hasPerm("devices.operate");
  const { data: devices } = useQuery({ queryKey: ["devices"], queryFn: () => api.get<Device[]>("/devices"), enabled: canOperate });
  const [bDev, setBDev] = useState("");
  const [bIP, setBIP] = useState("");
  const [bMin, setBMin] = useState(0);
  const [bReason, setBReason] = useState("");
  const [bFleet, setBFleet] = useState(false);
  const [bMsg, setBMsg] = useState("");
  const addBlock = useMutation({
    mutationFn: () => api.post<IPBlock>(`/devices/${bDev}/block-ip`, { ip: bIP.trim(), minutes: bMin, reason: bReason.trim(), fleet: bFleet }),
    onSuccess: () => { setBMsg(t("Sperre eingereiht")); setBIP(""); setBReason(""); qc.invalidateQueries({ queryKey: ["ip-blocks"] }); },
    onError: (e) => setBMsg(e instanceof ApiError ? e.message : t("Fehler")),
  });
  return (
    <section className="card">
      <p className="muted small">{t("IP-Sperren, die Active Response oder ein Bediener angestoßen hat. Zeitlich begrenzte Sperren hebt der Agent selbst wieder auf; „aufheben“ entfernt sie sofort. Neben einzelnen IPs lassen sich ganze Netze (CIDR) sperren; die Sperre gilt auch für Verkehr zu Docker-Containern.")}</p>
      {canOperate && (
        <form className="row" style={{ display: "flex", flexWrap: "wrap", gap: 8, alignItems: "center", marginBottom: 12 }} onSubmit={(e) => { e.preventDefault(); if (bDev && bIP.trim()) addBlock.mutate(); }}>
          <select value={bDev} onChange={(e) => setBDev(e.target.value)} required>
            <option value="">{t("Gerät wählen…")}</option>
            {(devices ?? []).filter((d) => d.status !== "unmanaged").sort((a, b) => a.hostname.localeCompare(b.hostname)).map((d) => <option key={d.id} value={d.id}>{d.hostname}</option>)}
          </select>
          <input className="mono" placeholder={t("IP oder Netz (203.0.113.0/24)")} value={bIP} onChange={(e) => setBIP(e.target.value)} style={{ width: 200 }} required />
          <BlockDurationSelect value={bMin} onChange={setBMin} />
          <input placeholder={t("Grund")} value={bReason} onChange={(e) => setBReason(e.target.value)} style={{ width: 220 }} />
          <label className="chip" title={t("Zusätzlich auf allen Geräten sperren, deren Richtlinie Anmeldungen überwacht oder Log-Quellen liest")}><input type="checkbox" checked={bFleet} onChange={(e) => setBFleet(e.target.checked)} /> {t("auf allen überwachten Geräten")}</label>
          <button className="btn sm" type="submit" disabled={addBlock.isPending || !bDev || !bIP.trim()}>🚫 {t("IP sperren")}</button>
          {bMsg && <span className="muted small">{bMsg}</span>}
          <span className="muted small">{t("Eine bereits gesperrte IP wird nicht doppelt angelegt, sondern mit der neuen Dauer erneuert.")}</span>
        </form>
      )}
      <div className="page-head" style={{ marginBottom: 10 }}>
        <div className="head-actions" style={{ flexWrap: "wrap" }}>
          <input className="search" placeholder={t("IP, Netz, Gerät, Grund oder Ersteller suchen…")} value={q} onChange={(e) => setQ(e.target.value)} />
          <select value={bStatus} onChange={(e) => setBStatus(e.target.value)}>
            <option value="">{t("alle Status")}</option>
            <option value="active">{t("wirksam / eingereiht")}</option>
            <option value="expired">{t("abgelaufen")}</option>
            <option value="released">{t("aufgehoben")}</option>
            <option value="failed">{t("fehlgeschlagen")}</option>
          </select>
        </div>
        <span className="muted small">{t("{n} Sperren", { n: (rows ?? []).length })}{(rows ?? []).length >= 1000 ? " (" + t("gekürzt – Suche eingrenzen") + ")" : ""}</span>
      </div>
      {isLoading ? <p className="muted">{t("Lädt…")}</p> : (rows ?? []).length === 0 ? <p className="empty">{t("Keine Sperren.")}</p> : (
        <div className="scroll-list" style={{ maxHeight: "65vh" }}>
          <table className="table">
            <thead><tr><th>{t("Zeit")}</th><th>{t("Gerät")}</th><th>IP</th><th>{t("Land")}</th><th>{t("Grund")}</th><th>{t("Bis")}</th><th>{t("Status")}</th><th>{t("Von")}</th><th></th></tr></thead>
            <tbody>
              {(rows ?? []).map((b) => (
                <tr key={b.id} className="small">
                  <td className="mono muted">{new Date(b.created_at).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })}</td>
                  <td><Link to={`/devices/${b.device_id}`} className="link-strong">{b.hostname}</Link></td>
                  <td className="mono">{b.ip}</td>
                  <td><CountryCell cc={b.country} /></td>
                  <td className="muted">{b.reason || "—"}</td>
                  <td className="muted">{b.until ? new Date(b.until).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" }) : t("dauerhaft")}</td>
                  <td><span className={`badge ${cls[b.status] ?? "badge-unknown"}`} title={b.detail}>{t(label[b.status] ?? b.status)}</span>{b.status === "failed" && b.detail && <div className="muted small">{b.detail.slice(0, 80)}</div>}</td>
                  <td className="muted">{b.created_by}</td>
                  <td style={{ whiteSpace: "nowrap" }}>{hasPerm("devices.operate") && (b.status === "active" || b.status === "pending") && <>
                    <button className="btn ghost sm" disabled={unblock.isPending} onClick={() => unblock.mutate(b)}>{t("Aufheben")}</button>
                    <button className="btn ghost sm" disabled={unblockAll.isPending} title={t("Diese IP auf allen Geräten freigeben, auf denen sie gesperrt ist")} onClick={() => unblockAll.mutate(b.ip)}>{t("überall")}</button>
                  </>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function AuthTab() {
  const { t } = useI18n();
  const [params, setParams] = useSearchParams();
  const days = params.get("days") || "7";
  const kind = params.get("kind") || "";
  const ip = params.get("ip") || "";
  const client = params.get("client") || "";
  const [q, setQ] = useState("");
  const setParam = (k: string, v: string) => { const p = new URLSearchParams(params); if (v) p.set(k, v); else p.delete(k); setParams(p, { replace: true }); };
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const qs = new URLSearchParams({ days });
  if (kind) qs.set("kind", kind);
  if (ip) qs.set("ip", ip);
  if (client) qs.set("client", client);
  if (q.trim()) qs.set("q", q.trim());
  const key = qs.toString();
  const { data: rows, isLoading } = useQuery({ queryKey: ["auth-events", "fleet", key], queryFn: () => api.get<AuthEvent[]>(`/security/auth-events?${key}`), refetchInterval: 30000 });
  return (
    <section className="card">
      <div className="page-head" style={{ marginBottom: 10 }}>
        <div className="head-actions" style={{ flexWrap: "wrap" }}>
          <input className="search" placeholder={t("Benutzer, IP oder Host suchen…")} value={q} onChange={(e) => setQ(e.target.value)} />
          <select value={kind} onChange={(e) => setParam("kind", e.target.value)}>
            <option value="">{t("Alle Ereignisse")}</option>
            <option value="failed">{t("Fehlversuche")}</option>
            <option value="probe">{t("verdächtige Anfrage")}</option>
            <option value="login_ok">{t("Anmeldung")}</option>
            <option value="sudo">sudo</option>
            <option value="su">su</option>
            <option value="user_added">{t("Benutzer angelegt")}</option>
            <option value="user_removed">{t("Benutzer gelöscht")}</option>
            <option value="group_changed">{t("Gruppe geändert")}</option>
            <option value="password_changed">{t("Passwort geändert")}</option>
            <option value="lockout">{t("Kontosperre")}</option>
          </select>
          <select value={days} onChange={(e) => setParam("days", e.target.value)}>
            <option value="1">{t("24 Stunden")}</option>
            <option value="7">{t("7 Tage")}</option>
            <option value="30">{t("30 Tage")}</option>
            <option value="90">{t("90 Tage")}</option>
          </select>
          <select value={client} onChange={(e) => setParam("client", e.target.value)}>
            <option value="">{t("Alle Clients")}</option>
            {(tree?.clients ?? []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
          {ip && <span className="filter-chip" onClick={() => setParam("ip", "")}>IP {ip} ✕</span>}
        </div>
        <span className="muted small">{t("{n} Ereignisse", { n: (rows ?? []).length })}</span>
      </div>
      {isLoading ? <p className="muted">{t("Lädt…")}</p> : <AuthEventsTable events={rows ?? []} showDevice />}
    </section>
  );
}

function FimTab() {
  const { t } = useI18n();
  const { hasPerm } = useAuth();
  const qc = useQueryClient();
  const [params, setParams] = useSearchParams();
  const days = params.get("days") || "7";
  const change = params.get("change") || "";
  const client = params.get("client") || "";
  const openOnly = params.get("open") !== "0";
  const [q, setQ] = useState("");
  const setParam = (k: string, v: string) => { const p = new URLSearchParams(params); if (v) p.set(k, v); else p.delete(k); setParams(p, { replace: true }); };
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const qs = new URLSearchParams({ days });
  if (change) qs.set("change", change);
  if (client) qs.set("client", client);
  if (openOnly) qs.set("open", "1");
  if (q.trim()) qs.set("q", q.trim());
  const key = qs.toString();
  const { data: rows, isLoading } = useQuery({ queryKey: ["fim", "fleet", key], queryFn: () => api.get<FIMEvent[]>(`/security/fim?${key}`), refetchInterval: 30000 });
  void useMutation; void qc;
  return (
    <section className="card">
      <div className="page-head" style={{ marginBottom: 10 }}>
        <div className="head-actions" style={{ flexWrap: "wrap" }}>
          <input className="search" placeholder={t("Pfad oder Host suchen…")} value={q} onChange={(e) => setQ(e.target.value)} />
          <select value={days} onChange={(e) => setParam("days", e.target.value)}>
            <option value="1">{t("24 Stunden")}</option>
            <option value="7">{t("7 Tage")}</option>
            <option value="30">{t("30 Tage")}</option>
            <option value="90">{t("90 Tage")}</option>
          </select>
          <select value={change} onChange={(e) => setParam("change", e.target.value)}>
            <option value="">{t("Alle Änderungen")}</option>
            <option value="created">{t("neu")}</option>
            <option value="modified">{t("geändert")}</option>
            <option value="deleted">{t("gelöscht")}</option>
            <option value="attrs">{t("Rechte/Besitzer")}</option>
          </select>
          <select value={client} onChange={(e) => setParam("client", e.target.value)}>
            <option value="">{t("Alle Clients")}</option>
            {(tree?.clients ?? []).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
          <label className={`chip ${openOnly ? "chip-on" : ""}`}><input type="checkbox" checked={openOnly} onChange={(e) => setParam("open", e.target.checked ? "" : "0")} /> {t("nur offene")}</label>
        </div>
        <span className="muted small">{t("{n} Ereignisse", { n: (rows ?? []).length })}</span>
      </div>
      {isLoading ? <p className="muted">{t("Lädt…")}</p> : <FimEventsTable events={rows ?? []} showDevice canOperate={hasPerm("devices.operate")} />}
    </section>
  );
}
