import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { ComplianceDevice, ComplianceResult, ComplianceRule, ComplianceRuleStat } from "../types";

export const CATEGORY_LABEL: Record<string, string> = { auth: "Konten & Anmeldung", network: "Netzwerk", fs: "Dateisystem", updates: "Updates", hardening: "Härtung", crypto: "Verschlüsselung" };
export const SEVERITY_LABEL: Record<string, string> = { high: "hoch", medium: "mittel", low: "niedrig" };
const sevCls = (s: string) => (s === "high" ? "badge-offline" : s === "medium" ? "badge-warn" : "badge-unknown");

export function useComplianceRules() {
  const { data } = useQuery({ queryKey: ["compliance-rules"], queryFn: () => api.get<ComplianceRule[]>("/compliance/rules"), staleTime: 10 * 60 * 1000 });
  return data ?? [];
}

export function ruleTitle(r: ComplianceRule | undefined, lang: string, id: string) {
  if (!r) return id;
  return lang === "en" && r.title_en ? r.title_en : r.title;
}

// ScoreBar zeigt einen Score 0–100 als Balken mit Farbe.
export function ScoreBar({ score, width = 120 }: { score: number; width?: number }) {
  const color = score >= 90 ? "var(--green, #16a34a)" : score >= 70 ? "var(--orange, #d97706)" : "var(--red)";
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
      <span style={{ display: "inline-block", width, height: 8, background: "var(--surface-2)", borderRadius: 4, overflow: "hidden" }}>
        <span style={{ display: "block", width: `${Math.max(0, Math.min(100, score))}%`, height: "100%", background: color }} />
      </span>
      <strong style={{ color }}>{score}%</strong>
    </span>
  );
}

export function StatusPill({ status }: { status: string }) {
  const { t } = useI18n();
  const map: Record<string, [string, string]> = { pass: ["badge-online", "bestanden"], fail: ["badge-offline", "fehlgeschlagen"], na: ["badge-unknown", "n/a"], error: ["badge-warn", "Fehler"] };
  const [cls, label] = map[status] ?? ["badge-unknown", status];
  return <span className={`badge ${cls}`}>{t(label)}</span>;
}

// DeviceCompliance: Regelliste eines Geräts mit Evidenz und Empfehlung.
export function DeviceCompliance({ deviceId, canOperate, score, checkedAt }: { deviceId: string; canOperate: boolean; score?: number | null; checkedAt?: string }) {
  const { t, lang } = useI18n();
  const qc = useQueryClient();
  const rules = useComplianceRules();
  const byId = useMemo(() => new Map(rules.map((r) => [r.id, r])), [rules]);
  const [onlyFail, setOnlyFail] = useState(false);
  const [msg, setMsg] = useState("");
  const { data, isLoading } = useQuery({ queryKey: ["compliance", deviceId], queryFn: () => api.get<ComplianceResult[]>(`/devices/${deviceId}/compliance`), refetchInterval: 30000 });
  const scan = useMutation({ mutationFn: () => api.post(`/devices/${deviceId}/compliance-scan`), onSuccess: () => { setMsg(t("Prüfung eingereiht – Ergebnis erscheint nach dem nächsten Checkin.")); setTimeout(() => qc.invalidateQueries({ queryKey: ["compliance", deviceId] }), 8000); } });
  const results = (data ?? []).filter((r) => !onlyFail || r.status === "fail");
  const order: Record<string, number> = { fail: 0, error: 1, pass: 2, na: 3 };
  results.sort((a, b) => (order[a.status] ?? 9) - (order[b.status] ?? 9) || a.rule_id.localeCompare(b.rule_id));
  return (
    <section className="card">
      <div className="page-head" style={{ marginBottom: 10 }}>
        <h2 style={{ margin: 0, display: "flex", alignItems: "center", gap: 12 }}>{t("Compliance")} {score !== undefined && score !== null && <ScoreBar score={score} />}</h2>
        <div className="actions">
          <label className={`chip ${onlyFail ? "chip-on" : ""}`}><input type="checkbox" checked={onlyFail} onChange={(e) => setOnlyFail(e.target.checked)} /> {t("nur Fehlschläge")}</label>
          {canOperate && <button className="btn ghost sm" disabled={scan.isPending} onClick={() => { setMsg(""); scan.mutate(); }}>↻ {t("Jetzt prüfen")}</button>}
        </div>
      </div>
      <p className="muted small">{t("Baseline-Regeln (CIS-artig) laut Richtlinie. Score = bestandene / (bestandene + fehlgeschlagene) Regeln; „n/a“ zählt nicht.")}{checkedAt ? ` · ${t("Zuletzt geprüft")} ${new Date(checkedAt).toLocaleString()}` : ""}</p>
      {msg && <p className="form-ok">{msg}</p>}
      {isLoading ? <p className="muted">{t("Lädt…")}</p> : (data ?? []).length === 0 ? (
        <p className="muted">{t("Noch keine Prüfung – in der Richtlinie unter „Sicherheitsüberwachung“ die Compliance-Prüfung aktivieren.")}</p>
      ) : (
        <div className="scroll-list" style={{ maxHeight: 520 }}>
          <table className="table">
            <thead><tr><th>{t("Status")}</th><th>{t("Regel")}</th><th>{t("Schwere")}</th><th>{t("Befund")}</th><th>{t("Empfehlung")}</th></tr></thead>
            <tbody>
              {results.map((r) => {
                const rule = byId.get(r.rule_id);
                return (
                  <tr key={r.rule_id} className="small">
                    <td><StatusPill status={r.status} /></td>
                    <td><strong>{ruleTitle(rule, lang, r.rule_id)}</strong>{rule && <div className="muted">{t(CATEGORY_LABEL[rule.category] ?? rule.category)} · <span className="mono">{r.rule_id}</span></div>}</td>
                    <td>{rule ? <span className={`badge ${sevCls(rule.severity)}`}>{t(SEVERITY_LABEL[rule.severity])}</span> : "—"}</td>
                    <td className="mono muted" style={{ wordBreak: "break-word" }}>{r.evidence || "—"}</td>
                    <td className="muted">{r.status === "fail" && rule ? rule.remediation : ""}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

// FleetCompliance: Geräte-Scores und Pass-Quote je Regel.
export function FleetCompliance() {
  const { t, lang } = useI18n();
  const rules = useComplianceRules();
  const byId = useMemo(() => new Map(rules.map((r) => [r.id, r])), [rules]);
  const [view, setView] = useState<"devices" | "rules">("devices");
  const [openRule, setOpenRule] = useState<string | null>(null);
  const { data: devices, isLoading } = useQuery({ queryKey: ["compliance-fleet"], queryFn: () => api.get<ComplianceDevice[]>("/compliance"), refetchInterval: 60000 });
  const { data: stats } = useQuery({ queryKey: ["compliance-by-rule"], queryFn: () => api.get<ComplianceRuleStat[]>("/compliance/by-rule"), refetchInterval: 60000, enabled: view === "rules" });
  const { data: ruleDevices } = useQuery({ queryKey: ["compliance-rule-devices", openRule], queryFn: () => api.get<ComplianceResult[]>(`/compliance/rules/${openRule}/devices?status=fail`), enabled: !!openRule });
  const avg = devices && devices.length ? Math.round(devices.reduce((a, d) => a + d.score, 0) / devices.length) : null;
  return (
    <section className="card">
      <div className="page-head" style={{ marginBottom: 10 }}>
        <div className="head-actions">
          <div className="tabs" style={{ margin: 0, border: 0 }}>
            <button className={`tab ${view === "devices" ? "tab-on" : ""}`} onClick={() => setView("devices")}>{t("nach Gerät")}</button>
            <button className={`tab ${view === "rules" ? "tab-on" : ""}`} onClick={() => setView("rules")}>{t("nach Regel")}</button>
          </div>
        </div>
        {avg !== null && <span className="small">{t("Ø Score")}: <ScoreBar score={avg} /></span>}
      </div>
      {isLoading ? <p className="muted">{t("Lädt…")}</p> : (devices ?? []).length === 0 ? (
        <p className="empty">{t("Noch keine geprüften Geräte – Compliance in einer Richtlinie unter „Sicherheitsüberwachung“ aktivieren.")}</p>
      ) : view === "devices" ? (
        <div className="scroll-list" style={{ maxHeight: "65vh" }}>
          <table className="table">
            <thead><tr><th>{t("Gerät")}</th><th>{t("Firma / Standort")}</th><th>{t("Profil")}</th><th>{t("Score")}</th><th>{t("Bestanden")}</th><th>{t("Fehlgeschlagen")}</th><th>{t("Geprüft")}</th></tr></thead>
            <tbody>
              {(devices ?? []).map((d) => (
                <tr key={d.device_id} className="small">
                  <td><Link to={`/devices/${d.device_id}`} className="link-strong">{d.hostname}</Link> <span className="muted">{d.os}</span></td>
                  <td className="muted">{d.client_name ? `${d.client_name} › ${d.site_name}` : "—"}</td>
                  <td className="muted">{d.profile === "workstation" ? t("Arbeitsplatz") : t("Server")}</td>
                  <td><ScoreBar score={d.score} /></td>
                  <td>{d.passed}</td>
                  <td>{d.failed > 0 ? <span className="badge badge-offline">{d.failed}</span> : <span className="muted">0</span>}</td>
                  <td className="muted">{new Date(d.checked_at).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="scroll-list" style={{ maxHeight: "65vh" }}>
          <table className="table">
            <thead><tr><th>{t("Regel")}</th><th>{t("Schwere")}</th><th>{t("Erfüllt")}</th><th>{t("Fehlgeschlagen")}</th><th>{t("n/a")}</th></tr></thead>
            <tbody>
              {(stats ?? []).map((s) => s).sort((a, b) => b.failed - a.failed || a.rule_id.localeCompare(b.rule_id)).map((s) => {
                const rule = byId.get(s.rule_id);
                const total = s.passed + s.failed;
                const pct = total ? Math.round((s.passed / total) * 100) : 100;
                const isOpen = openRule === s.rule_id;
                return [
                  <tr key={s.rule_id} className="small" style={{ cursor: s.failed > 0 ? "pointer" : undefined }} onClick={() => s.failed > 0 && setOpenRule(isOpen ? null : s.rule_id)}>
                    <td>{s.failed > 0 && <span className="muted">{isOpen ? "▾ " : "▸ "}</span>}<strong>{ruleTitle(rule, lang, s.rule_id)}</strong>{rule && <div className="muted">{t(CATEGORY_LABEL[rule.category] ?? rule.category)} · {(rule.platforms ?? []).join(", ")}</div>}</td>
                    <td>{rule ? <span className={`badge ${sevCls(rule.severity)}`}>{t(SEVERITY_LABEL[rule.severity])}</span> : "—"}</td>
                    <td><ScoreBar score={pct} width={90} /> <span className="muted">{s.passed}/{total}</span></td>
                    <td>{s.failed > 0 ? <span className="badge badge-offline">{s.failed}</span> : <span className="muted">0</span>}</td>
                    <td className="muted">{s.skipped}</td>
                  </tr>,
                  ...(isOpen ? [(
                    <tr key={s.rule_id + ":d"}><td colSpan={5} style={{ paddingLeft: 28 }}>
                      {rule && <p className="muted small" style={{ marginTop: 0 }}>{rule.description} <strong>{t("Empfehlung")}:</strong> {rule.remediation}</p>}
                      {(ruleDevices ?? []).map((r) => (
                        <div key={r.device_id} className="small"><Link to={`/devices/${r.device_id}`} className="link-strong">{r.hostname}</Link> <span className="mono muted">{r.evidence}</span></div>
                      ))}
                    </td></tr>
                  )] : []),
                ];
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
