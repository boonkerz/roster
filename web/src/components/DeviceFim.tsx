import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import { FimEventsTable } from "./FimEvents";
import type { FIMEvent } from "../types";

// DeviceFim zeigt die Datei-Änderungen eines Geräts und erlaubt Neuprüfung/Baseline-Reset.
export function DeviceFim({ deviceId, canOperate }: { deviceId: string; canOperate: boolean }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [days, setDays] = useState("30");
  const [msg, setMsg] = useState("");
  const { data, isLoading } = useQuery({
    queryKey: ["fim", deviceId, days],
    queryFn: () => api.get<FIMEvent[]>(`/devices/${deviceId}/fim?days=${days}&limit=1000`),
    refetchInterval: 30000,
  });
  const rescan = useMutation({ mutationFn: () => api.post(`/devices/${deviceId}/fim/rescan`), onSuccess: () => setMsg(t("Prüfung eingereiht – Ergebnis erscheint nach dem nächsten Checkin.")) });
  const reset = useMutation({ mutationFn: () => api.post(`/devices/${deviceId}/fim/reset`), onSuccess: () => setMsg(t("Baseline wird neu aufgebaut – bisherige Vergleichswerte verfallen.")) });
  const events = data ?? [];
  const hasBaseline = events.some((e) => e.change === "baseline") || events.length > 0;
  return (
    <section className="card">
      <div className="page-head" style={{ marginBottom: 10 }}>
        <h2 style={{ margin: 0 }}>{t("Datei-Integrität")}</h2>
        <div className="actions">
          <select value={days} onChange={(e) => setDays(e.target.value)}>
            <option value="7">{t("7 Tage")}</option>
            <option value="30">{t("30 Tage")}</option>
            <option value="90">{t("90 Tage")}</option>
          </select>
          {canOperate && <button className="btn ghost sm" disabled={rescan.isPending} onClick={() => { setMsg(""); rescan.mutate(); }}>↻ {t("Jetzt prüfen")}</button>}
          {canOperate && <button className="btn ghost sm" disabled={reset.isPending} onClick={() => { if (confirm(t("Baseline neu setzen? Alle aktuellen Dateistände gelten danach als Soll."))) { setMsg(""); reset.mutate(); } }}>{t("Baseline neu setzen")}</button>}
        </div>
      </div>
      <p className="muted small">{t("Überwachte Pfade kommen aus dem Bereich „Sicherheitsüberwachung“ der zugewiesenen Richtlinien. Der erste Lauf legt die Baseline an; danach wird jede Abweichung gemeldet.")}</p>
      {msg && <p className="form-ok">{msg}</p>}
      {isLoading ? <p className="muted">{t("Lädt…")}</p>
        : !hasBaseline ? <p className="muted">{t("Noch keine Baseline – entweder sind keine Pfade konfiguriert oder der Agent hat noch nicht geprüft.")}</p>
        : <FimEventsTable events={events} canOperate={canOperate} onChanged={() => qc.invalidateQueries({ queryKey: ["fim", deviceId] })} maxHeight={480} />}
    </section>
  );
}
