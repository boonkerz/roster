import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import { AuthEventsTable, IncidentsTable } from "./SecurityEvents";
import { IncidentActions } from "./IncidentActions";
import type { AuthEvent, IPBlock, SecurityIncident } from "../types";
import { useMutation, useQueryClient } from "@tanstack/react-query";

// DeviceAuth zeigt Vorfälle und Anmeldeereignisse eines Geräts.
export function DeviceAuth({ deviceId, canOperate }: { deviceId: string; canOperate: boolean }) {
  const { t } = useI18n();
  const [days, setDays] = useState("7");
  const [kind, setKind] = useState("");
  const { data: incidents } = useQuery({ queryKey: ["incidents", deviceId, days], queryFn: () => api.get<SecurityIncident[]>(`/devices/${deviceId}/incidents?days=${days}`), refetchInterval: 30000 });
  const qc = useQueryClient();
  const { data: blocks } = useQuery({ queryKey: ["ip-blocks", deviceId], queryFn: () => api.get<IPBlock[]>(`/devices/${deviceId}/blocks`), refetchInterval: 30000 });
  const unblock = useMutation({ mutationFn: (b: IPBlock) => api.post(`/devices/${deviceId}/unblock-ip`, { ip: b.ip }), onSuccess: () => qc.invalidateQueries({ queryKey: ["ip-blocks"] }) });
  const activeBlocks = (blocks ?? []).filter((b) => b.status === "active" || b.status === "pending");
  const { data: events, isLoading } = useQuery({ queryKey: ["auth-events", deviceId, days, kind], queryFn: () => api.get<AuthEvent[]>(`/devices/${deviceId}/auth-events?days=${days}${kind ? `&kind=${kind}` : ""}&limit=500`), refetchInterval: 30000 });
  return (
    <>
      <section className="card">
        <div className="page-head" style={{ marginBottom: 10 }}>
          <h2 style={{ margin: 0 }}>{t("Sicherheitsvorfälle")} {(incidents ?? []).filter((i) => i.status === "open").length > 0 && <span className="tab-count tab-count-bad">{(incidents ?? []).filter((i) => i.status === "open").length}</span>}</h2>
          <select value={days} onChange={(e) => setDays(e.target.value)}>
            <option value="7">{t("7 Tage")}</option>
            <option value="30">{t("30 Tage")}</option>
            <option value="90">{t("90 Tage")}</option>
          </select>
        </div>
        <IncidentsTable incidents={incidents ?? []} canOperate={canOperate} maxHeight={320} actions={canOperate ? (i) => <IncidentActions incident={i} /> : undefined} />
        {activeBlocks.length > 0 && (
          <div style={{ marginTop: 10 }}>
            <span className="muted small">{t("Aktive IP-Sperren")}: </span>
            {activeBlocks.map((b) => (
              <span key={b.id} className="badge badge-offline" style={{ marginRight: 6 }}>🚫 {b.ip}{b.until ? ` · ${t("bis")} ${new Date(b.until).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}` : ""}
                {canOperate && <button className="btn ghost sm" style={{ marginLeft: 4, padding: "0 4px" }} onClick={() => unblock.mutate(b)} title={t("Aufheben")}>✕</button>}
              </span>
            ))}
          </div>
        )}
      </section>
      <section className="card">
        <div className="page-head" style={{ marginBottom: 10 }}>
          <h2 style={{ margin: 0 }}>{t("Anmeldungen")}</h2>
          <select value={kind} onChange={(e) => setKind(e.target.value)}>
            <option value="">{t("Alle Ereignisse")}</option>
            <option value="failed">{t("Fehlversuche")}</option>
            <option value="login_ok">{t("Anmeldung")}</option>
            <option value="sudo">sudo</option>
            <option value="user_added">{t("Benutzer angelegt")}</option>
            <option value="group_changed">{t("Gruppe geändert")}</option>
          </select>
        </div>
        <p className="muted small">{t("Erscheint nur, wenn eine zugewiesene Richtlinie „Anmeldungen überwachen“ aktiviert hat. Linux: journald/auth.log; Windows: Sicherheitsprotokoll; macOS: Unified Log.")}</p>
        {isLoading ? <p className="muted">{t("Lädt…")}</p> : <AuthEventsTable events={events ?? []} maxHeight={420} />}
      </section>
    </>
  );
}
