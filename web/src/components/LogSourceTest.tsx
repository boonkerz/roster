import { useState } from "react";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import { pollCmd } from "../pollCmd";
import type { Device, LogSourceSpec, LogSourceTestResult } from "../types";
import { AUTH_KIND } from "./SecurityEvents";

// LogSourceTest lässt den Agent eines Geräts eine Log-Quelle probeweise lesen (letzte
// Zeilen parsen, Treffer zählen) – aus dem Richtlinien-Editor (Gerät wählbar) oder vom
// Gerät aus (deviceId fest).
export function LogSourceTest({ source, devices, deviceId, onClose }: { source: LogSourceSpec; devices?: Device[]; deviceId?: string; onClose: () => void }) {
  const { t } = useI18n();
  const candidates = (devices ?? []).filter((d) => d.status !== "unmanaged").sort((a, b) => a.hostname.localeCompare(b.hostname));
  const [dev, setDev] = useState(deviceId ?? candidates.find((d) => d.os === "linux")?.id ?? candidates[0]?.id ?? "");
  const [lines, setLines] = useState(300);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [res, setRes] = useState<LogSourceTestResult | null>(null);
  const run = async () => {
    if (!dev) return;
    setBusy(true); setErr(""); setRes(null);
    try {
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${dev}/test-log-source`, { ...source, lines });
      const cmd = await pollCmd(command_id);
      try { setRes(JSON.parse(cmd.output ?? "{}") as LogSourceTestResult); } catch { setErr(cmd.output || t("Keine Antwort vom Agent")); }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : e instanceof Error ? e.message : String(e));
    } finally { setBusy(false); }
  };
  const targets = res?.targets ?? [];
  return (
    <div className="card" style={{ marginTop: 10, padding: 12, background: "var(--bg-2, transparent)" }}>
      <div style={{ display: "flex", alignItems: "flex-start", gap: 8, marginBottom: 6 }}>
        <strong style={{ flex: 1, overflowWrap: "anywhere" }}>{t("Log-Quelle testen")}: <span className="mono">{source.type === "docker" ? "🐳 " : ""}{source.target}</span></strong>
        <button className="btn ghost sm" type="button" onClick={onClose}>✕</button>
      </div>
      <div className="inline-form">
        {!deviceId && (
          <select value={dev} onChange={(e) => setDev(e.target.value)}>
            <option value="">{t("Gerät wählen…")}</option>
            {candidates.map((d) => <option key={d.id} value={d.id}>{d.hostname}{d.os && d.os !== "linux" ? ` (${d.os})` : ""}</option>)}
          </select>
        )}
        <label className="num">{t("letzte Zeilen")} <input type="number" min={10} max={5000} value={lines} onChange={(e) => setLines(Number(e.target.value) || 300)} /></label>
        <button className="btn primary sm" type="button" disabled={busy || !dev} onClick={run}>{busy ? t("Läuft…") : t("Testen")}</button>
        <span className="muted small">{t("Der Agent liest die letzten Zeilen, ohne den Lesezeiger zu verändern; Treffer werden nicht als Ereignisse gespeichert.")}</span>
      </div>
      {err && <p className="error" style={{ marginTop: 6 }}>{err}</p>}
      {res && (
        <div style={{ marginTop: 8 }}>
          {res.error && <p className="error">{res.error}</p>}
          {res.hints && res.hints.length > 0 && <p className="muted small">{t("Laufende Container")}: <span className="mono">{res.hints.join(", ")}</span></p>}
          {targets.length > 0 && (
            <>
              <p className="small" style={{ margin: "4px 0" }}>
                {res.matched > 0
                  ? <span className="badge badge-online">✓ {t("{m} Treffer in {n} Zeilen", { m: res.matched, n: res.lines })}</span>
                  : <span className="badge badge-warn">{t("{m} Treffer in {n} Zeilen", { m: 0, n: res.lines })}</span>}
                {res.matched === 0 && res.lines > 0 && <span className="muted"> · {t("Quelle lesbar, aber kein Treffer: entweder passt das Format nicht zu den Zeilen oder es gab zuletzt keine Fehlversuche/verdächtigen Anfragen.")}</span>}
                {res.matched === 0 && res.lines === 0 && <span className="muted"> · {t("Quelle gefunden, aber leer.")}</span>}
              </p>
              <table className="table small">
                <thead><tr><th>{t("Ziel")}</th><th>{t("Zeilen")}</th><th>{t("Treffer")}</th><th></th></tr></thead>
                <tbody>{targets.map((x) => <tr key={x.target}><td className="mono">{x.target}</td><td>{x.lines}</td><td>{x.matched}</td><td className="error small">{x.error}</td></tr>)}</tbody>
              </table>
            </>
          )}
          {(res.events ?? []).length > 0 && (
            <div className="scroll-list" style={{ maxHeight: 260, marginTop: 6 }}>
              <table className="table small">
                <thead><tr><th>{t("Zeit")}</th><th>{t("Ereignis")}</th><th>{t("Quelle")}</th><th>{t("Benutzer")}</th><th>{t("Details")}</th></tr></thead>
                <tbody>
                  {res.events.map((e, i) => {
                    const m = AUTH_KIND[e.kind] ?? { label: e.kind, cls: "badge-unknown" };
                    return (
                      <tr key={i}>
                        <td className="mono muted">{e.time ? new Date(e.time).toLocaleString(undefined, { dateStyle: "short", timeStyle: "medium" }) : "—"}</td>
                        <td><span className={`badge ${m.cls}`}>{t(m.label)}</span></td>
                        <td className="mono">{e.source_ip || "—"}</td>
                        <td>{e.user || "—"}</td>
                        <td className="muted" title={e.detail}>{e.detail ? (e.detail.length > 90 ? e.detail.slice(0, 90) + "…" : e.detail) : ""}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
