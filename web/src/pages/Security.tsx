import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { api } from "../api";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { FimEventsTable } from "../components/FimEvents";
import { AuthEventsTable, IncidentsTable, INCIDENT_KIND } from "../components/SecurityEvents";
import type { AuthEvent, ClientTree, FIMEvent, SecurityIncident, SecuritySummary } from "../types";

// Security ist die flottenweite Sicherheitsseite (Wazuh-Stil): Datei-Integrität,
// später Vorfälle, Anmeldungen, Compliance und Sperren.
type SecTab = "incidents" | "auth" | "fim";
const TABS: { key: SecTab; label: string }[] = [
  { key: "incidents", label: "Vorfälle" },
  { key: "auth", label: "Anmeldungen" },
  { key: "fim", label: "Datei-Integrität" },
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
  const [q, setQ] = useState("");
  const setParam = (k: string, v: string) => { const p = new URLSearchParams(params); if (v) p.set(k, v); else p.delete(k); setParams(p, { replace: true }); };
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: summary } = useQuery({ queryKey: ["security-summary"], queryFn: () => api.get<SecuritySummary>("/security/summary"), refetchInterval: 30000 });
  const qs = new URLSearchParams({ days });
  if (status) qs.set("status", status);
  if (kind) qs.set("kind", kind);
  if (severity) qs.set("severity", severity);
  if (client) qs.set("client", client);
  if (q.trim()) qs.set("q", q.trim());
  const key = qs.toString();
  const { data: rows, isLoading } = useQuery({ queryKey: ["incidents", "fleet", key], queryFn: () => api.get<SecurityIncident[]>(`/security/incidents?${key}`), refetchInterval: 30000 });
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
                <span key={x.ip} className="badge badge-unknown" style={{ marginRight: 6, marginBottom: 4, cursor: "pointer" }} onClick={() => { const p = new URLSearchParams(params); p.set("tab", "auth"); p.set("ip", x.ip); setParams(p); }}>{x.ip} · {x.count}</span>
              ))}
            </div>
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
          </div>
          <span className="muted small">{t("{n} Vorfälle", { n: (rows ?? []).length })}</span>
        </div>
        <p className="muted small">{t("Vorfälle entstehen aus den Anmeldeereignissen: Brute-Force (Schwellwert in den Einstellungen), erfolgreiche Anmeldung nach Brute-Force, erste Anmeldung von einer neuen Quell-IP (nach der Lernphase), Kontoänderungen und Sperrungen.")}</p>
        {isLoading ? <p className="muted">{t("Lädt…")}</p> : <IncidentsTable incidents={rows ?? []} showDevice canOperate={hasPerm("devices.operate")} />}
      </section>
    </>
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
