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
  const [minutes, setMinutes] = useState(60);
  const done = () => { qc.invalidateQueries({ queryKey: ["ip-blocks"] }); qc.invalidateQueries({ queryKey: ["incidents"] }); };
  const block = useMutation({
    mutationFn: () => api.post(`/devices/${incident.device_id}/block-ip`, { ip: incident.source_ip, minutes, reason: incident.title }),
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
      {incident.source_ip && (
        <span style={{ display: "inline-flex", alignItems: "center", gap: 4, whiteSpace: "nowrap" }}>
          <button className="btn ghost sm" disabled={block.isPending} title={t("Quell-IP auf dem Gerät sperren (Dauer rechts wählen; erneutes Sperren verlängert die bestehende Sperre)")} onClick={() => { if (confirm(t("IP {ip} auf {h} sperren ({d})?", { ip: incident.source_ip ?? "", h: incident.hostname ?? "", d: durationLabel(minutes, t) }))) block.mutate(); }}>🚫 IP</button>
          <BlockDurationSelect value={minutes} onChange={setMinutes} />
        </span>
      )}
      {canLock && <button className="btn ghost sm" disabled={lock.isPending} title={t("Lokales Konto sperren")} onClick={() => { if (confirm(t("Konto {u} auf {h} sperren?", { u: incident.username ?? "", h: incident.hostname ?? "" }))) lock.mutate(); }}>🔒 {incident.username}</button>}
      {msg && <span className="muted small"> {msg}</span>}
    </>
  );
}

// Sperrdauern für manuelle IP-Sperren (Minuten; 0 = dauerhaft).
export const BLOCK_DURATIONS = [60, 24 * 60, 7 * 24 * 60, 30 * 24 * 60, 0];

export function durationLabel(minutes: number, t: (k: string, v?: Record<string, string | number>) => string): string {
  if (minutes <= 0) return t("dauerhaft");
  if (minutes % (24 * 60) === 0 && minutes > 24 * 60) return t("{n} Tage", { n: minutes / (24 * 60) });
  if (minutes % 60 === 0) return t("{n} Std.", { n: minutes / 60 });
  return t("{n} Minuten", { n: minutes });
}

// BlockDurationSelect wählt die Dauer einer IP-Sperre.
export function BlockDurationSelect({ value, onChange }: { value: number; onChange: (m: number) => void }) {
  const { t } = useI18n();
  return (
    <select className="sm" value={value} onChange={(e) => onChange(Number(e.target.value))} title={t("Sperrdauer")}>
      {BLOCK_DURATIONS.map((m) => <option key={m} value={m}>{durationLabel(m, t)}</option>)}
    </select>
  );
}
