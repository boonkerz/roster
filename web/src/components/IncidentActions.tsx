import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import type { SecurityIncident } from "../types";

const PROTECTED = ["root", "administrator", "system"];

// IncidentActions: manuelle Reaktionen an einem Vorfall (IP sperren, Konto sperren).
export function IncidentActions({ incident }: { incident: SecurityIncident }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [msg, setMsg] = useState("");
  const done = () => { qc.invalidateQueries({ queryKey: ["ip-blocks"] }); qc.invalidateQueries({ queryKey: ["incidents"] }); };
  const block = useMutation({
    mutationFn: () => api.post(`/devices/${incident.device_id}/block-ip`, { ip: incident.source_ip, minutes: 60, reason: incident.title }),
    onSuccess: () => { setMsg(t("Sperre eingereiht")); done(); },
    onError: (e) => setMsg(e instanceof ApiError ? e.message : t("Fehler")),
  });
  const lock = useMutation({
    mutationFn: () => api.post(`/devices/${incident.device_id}/lock-user`, { user: incident.username }),
    onSuccess: () => { setMsg(t("Kontosperre eingereiht")); done(); },
    onError: (e) => setMsg(e instanceof ApiError ? e.message : t("Fehler")),
  });
  const canLock = !!incident.username && !PROTECTED.includes(incident.username.toLowerCase());
  return (
    <>
      {incident.source_ip && <button className="btn ghost sm" disabled={block.isPending} title={t("Quell-IP auf dem Gerät für 60 Minuten sperren")} onClick={() => { if (confirm(t("IP {ip} auf {h} für 60 Minuten sperren?", { ip: incident.source_ip ?? "", h: incident.hostname ?? "" }))) block.mutate(); }}>🚫 IP</button>}
      {canLock && <button className="btn ghost sm" disabled={lock.isPending} title={t("Lokales Konto sperren")} onClick={() => { if (confirm(t("Konto {u} auf {h} sperren?", { u: incident.username ?? "", h: incident.hostname ?? "" }))) lock.mutate(); }}>🔒 {incident.username}</button>}
      {msg && <span className="muted small"> {msg}</span>}
    </>
  );
}
