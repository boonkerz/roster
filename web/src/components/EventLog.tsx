import { useState } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import { pollCmd } from "../pollCmd";

interface Evt { time: string; id?: number; level: string; source?: string; message: string; }

// EventLog zeigt die letzten Ereignisse (Windows-Eventlog bzw. journald) on-demand.
export function EventLog({ deviceId, os }: { deviceId: string; os: string }) {
  const { t } = useI18n();
  const isWin = /win/i.test(os);
  const [log, setLog] = useState(isWin ? "System" : "all");
  const [events, setEvents] = useState<Evt[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");

  const load = async () => {
    setLoading(true); setError("");
    try {
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${deviceId}/event-log`, { log, count: 100 });
      const cmd = await pollCmd(command_id);
      const data = JSON.parse(cmd.output || "{}");
      if (data.error) setError(data.error);
      else if (data.info && (data.events ?? []).length === 0) setError(data.info);
      else setEvents(data.events ?? []);
    } catch (e) { setError((e as Error).message); } finally { setLoading(false); }
  };

  const levelCls = (l: string) => /error|fehler|critical|kritisch/i.test(l) ? "badge-offline"
    : /warn/i.test(l) ? "badge-warn" : "badge-unknown";

  const shown = (events ?? []).filter((e) =>
    e.message.toLowerCase().includes(filter.toLowerCase()) || (e.source ?? "").toLowerCase().includes(filter.toLowerCase()));

  return (
    <section className="card">
      <div className="page-head" style={{ marginBottom: 10 }}>
        <div className="inline-form">
          <select value={log} onChange={(e) => setLog(e.target.value)}>
            {isWin
              ? <>
                  <option value="System">{t("System-Log")}</option>
                  <option value="Application">{t("Anwendung")}</option>
                  <option value="Security">{t("Sicherheit")}</option>
                </>
              : <>
                  <option value="all">{t("Alle (journald)")}</option>
                  <option value="errors">{t("Nur Warnungen/Fehler")}</option>
                </>}
          </select>
          <button className="btn primary sm" disabled={loading} onClick={load}>{loading ? "…" : t("Laden")}</button>
        </div>
        {events && <input className="search" placeholder={t("Filtern…")} value={filter} onChange={(e) => setFilter(e.target.value)} />}
      </div>

      {loading ? <p className="muted">{t("Wird abgefragt… (Agent muss online sein)")}</p>
        : error ? <p className="muted">{error}</p>
        : !events ? <p className="muted">{t("„Laden\u201c zeigt die letzten 100 Einträge.")}</p>
        : (
          <div className="scroll-list">
            <table className="table">
              <thead><tr><th>{t("Zeit")}</th><th>{t("Stufe")}</th><th>{t("Quelle")}</th><th>{t("Meldung")}</th></tr></thead>
              <tbody>
                {shown.map((e, i) => (
                  <tr key={i}>
                    <td className="muted small" style={{ whiteSpace: "nowrap" }}>{e.time ? new Date(e.time).toLocaleString() : "—"}</td>
                    <td><span className={`badge ${levelCls(e.level)}`}>{e.level || "—"}</span></td>
                    <td className="muted small">{e.source || "—"}{e.id ? ` (${e.id})` : ""}</td>
                    <td className="small">{e.message}</td>
                  </tr>
                ))}
                {shown.length === 0 && <tr><td colSpan={4} className="empty">{t("Keine Einträge.")}</td></tr>}
              </tbody>
            </table>
          </div>
        )}
    </section>
  );
}
