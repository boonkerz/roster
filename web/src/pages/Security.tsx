import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { api } from "../api";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";
import { FimEventsTable } from "../components/FimEvents";
import type { ClientTree, FIMEvent } from "../types";

// Security ist die flottenweite Sicherheitsseite (Wazuh-Stil): Datei-Integrität,
// später Vorfälle, Anmeldungen, Compliance und Sperren.
type SecTab = "fim";
const TABS: { key: SecTab; label: string }[] = [
  { key: "fim", label: "Datei-Integrität" },
];

export function Security() {
  const { t } = useI18n();
  const [params, setParams] = useSearchParams();
  const tab = (params.get("tab") as SecTab) || "fim";
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
      {tab === "fim" && <FimTab />}
    </div>
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
