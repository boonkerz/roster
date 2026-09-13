import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { Device, ProxmoxGuest } from "../types";
import { formatBytes } from "../pages/Devices";
import { relTime } from "./StatusBadge";

type Action = "start" | "shutdown" | "stop" | "reboot";

// Backup gilt als veraltet, wenn es älter als dieser Wert ist – nur für die Anzeige.
// Die verbindliche Schwelle setzt der Check „Proxmox-Backups" in der Richtlinie.
const STALE_HOURS = 26;

// backupState fasst den Backup-Stand eines Gasts für Plakette und Zusammenfassung zusammen.
export function backupState(g: ProxmoxGuest): "ok" | "stale" | "failed" | "none" | "nojob" {
  if (g.backup_task_status === "failed") return "failed";
  if (!g.backup_at) return "none";
  if (Date.now() - new Date(g.backup_at).getTime() > STALE_HOURS * 3600_000) return "stale";
  if (g.backup_job === false) return "nojob";
  return "ok";
}

function uptimeText(sec?: number): string {
  if (!sec) return "—";
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  return d > 0 ? `${d}d ${h}h` : `${h}h ${Math.floor((sec % 3600) / 60)}m`;
}

// ProxmoxPanel zeigt die vom Agent auf einem Proxmox-VE-Host gemeldeten VMs und
// Container samt Backup-Status. Mit Bedienrecht lassen sich Gäste starten,
// herunterfahren, hart stoppen und neu starten – der Agent führt das per pvesh aus,
// der neue Zustand kommt mit dem folgenden Checkin.
export function ProxmoxPanel({ device, canOperate }: { device: Device; canOperate?: boolean }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const guests = device.proxmox_guests ?? [];
  const [busy, setBusy] = useState<number | null>(null);
  const [message, setMessage] = useState<{ text: string; bad: boolean } | null>(null);

  const control = useMutation({
    mutationFn: async (v: { guest: ProxmoxGuest; action: Action }) => {
      setBusy(v.guest.vmid);
      setMessage(null);
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${device.id}/proxmox-control`, {
        node: v.guest.node, type: v.guest.type, vmid: v.guest.vmid, action: v.action,
      });
      // Herunterfahren kann dauern – bis zu 3 Minuten auf das Ergebnis warten.
      for (let i = 0; i < 180; i++) {
        await new Promise((r) => setTimeout(r, 1000));
        const cmd = await api.get<{ status: string; exit_code: number; output?: string }>(`/commands/${command_id}`);
        if (cmd.status === "done") {
          if (cmd.exit_code !== 0) throw new Error(cmd.output || t("Aktion fehlgeschlagen."));
          return cmd.output ?? "";
        }
      }
      return t("Aktion läuft noch – der Status aktualisiert sich beim nächsten Checkin.");
    },
    onSuccess: (out) => setMessage({ text: out, bad: false }),
    onError: (e: Error) => setMessage({ text: e.message, bad: true }),
    onSettled: () => {
      setBusy(null);
      qc.invalidateQueries({ queryKey: ["device", device.id] });
      setTimeout(() => qc.invalidateQueries({ queryKey: ["device", device.id] }), 2000);
    },
  });

  const act = (g: ProxmoxGuest, action: Action) => {
    const what = `${g.type === "qemu" ? "VM" : "CT"} ${g.vmid}${g.name ? ` (${g.name})` : ""}`;
    if (action === "stop" && !confirm(t("{what} hart ausschalten? Nicht gespeicherte Daten gehen verloren.", { what }))) return;
    if (action === "reboot" && !confirm(t("{what} neu starten?", { what }))) return;
    control.mutate({ guest: g, action });
  };

  const active = guests.filter((g) => !g.template);
  const running = active.filter((g) => g.status === "running").length;
  const problems = active.filter((g) => backupState(g) !== "ok").length;

  const backupCell = (g: ProxmoxGuest) => {
    if (g.template) return <span className="muted small">—</span>;
    const st = backupState(g);
    const detail = [
      g.backup_at && `${t("Letztes Backup")}: ${new Date(g.backup_at).toLocaleString()}`,
      g.backup_storage && `${t("Speicher")}: ${g.backup_storage}`,
      g.backup_count && `${t("Anzahl")}: ${g.backup_count}`,
      g.backup_task_at && `${t("Letzter Lauf")}: ${new Date(g.backup_task_at).toLocaleString()}`,
      g.backup_task_msg,
    ].filter(Boolean).join("\n");
    return (
      <span title={detail}>
        {st === "failed" && <span className="badge badge-offline">{t("fehlgeschlagen")}</span>}
        {st === "none" && <span className="badge badge-offline">{t("kein Backup")}</span>}
        {st === "stale" && <span className="badge badge-warn">{relTime(g.backup_at)}</span>}
        {(st === "ok" || st === "nojob") && <span className="badge badge-online">{relTime(g.backup_at)}</span>}
        {g.backup_size ? <span className="muted small" style={{ marginLeft: 6 }}>{formatBytes(g.backup_size)}</span> : null}
        {g.backup_job === false && <span className="badge badge-warn" style={{ marginLeft: 6 }}>{t("kein Backup-Job")}</span>}
        {st === "failed" && g.backup_task_msg && <div className="muted small">{g.backup_task_msg}</div>}
      </span>
    );
  };

  return (
    <section className="card" style={{ marginTop: 12 }}>
      <h3 className="muted small" style={{ display: "flex", alignItems: "center", gap: 8, margin: 0, flexWrap: "wrap" }}>
        Proxmox VE {device.proxmox_version !== "unbekannt" ? device.proxmox_version : ""}
        <span className="badge badge-online"><span className="dot" /> {running} {t("laufen")}</span>
        <span className="muted small">/ {active.length} {t("Gäste")}</span>
        {problems > 0
          ? <span className="badge badge-offline">{t("{n} ohne aktuelles Backup", { n: problems })}</span>
          : active.length > 0 && <span className="badge badge-online">{t("Backups aktuell")}</span>}
      </h3>
      {guests.length === 0 ? (
        <p className="muted small">{t("Keine VMs oder Container auf diesem Host.")}</p>
      ) : (
        <div className="scroll-list">
          <table className="table">
            <thead>
              <tr>
                <th>{t("Status")}</th><th>ID</th><th>{t("Name")}</th><th>{t("Ressourcen")}</th>
                <th>{t("Laufzeit")}</th><th>{t("Backup")}</th>{canOperate && <th>{t("Aktion")}</th>}
              </tr>
            </thead>
            <tbody>
              {guests.map((g) => (
                <tr key={`${g.node}-${g.vmid}`} style={g.template ? { opacity: 0.55 } : undefined}>
                  <td>
                    {g.template ? <span className="badge badge-unknown">{t("Vorlage")}</span>
                      : g.status === "running" ? <span className="badge badge-online"><span className="dot" /> {g.status}</span>
                      : <span className="badge badge-offline"><span className="dot" /> {g.status}</span>}
                  </td>
                  <td className="mono small">{g.vmid}</td>
                  <td>
                    <span className="link-strong">{g.name || "—"}</span>
                    <div className="muted small">{g.type === "qemu" ? "VM" : "Container"} · {g.node}</div>
                  </td>
                  <td className="small">
                    {g.cpus ? `${g.cpus} vCPU` : ""}{g.status === "running" && g.cpu !== undefined ? ` · ${(g.cpu * 100).toFixed(0)} %` : ""}
                    <div className="muted small">
                      {g.status === "running" && g.mem ? `${formatBytes(g.mem)} / ` : ""}{formatBytes(g.maxmem ?? 0)} RAM
                      {g.maxdisk ? ` · ${formatBytes(g.maxdisk)}` : ""}
                    </div>
                  </td>
                  <td className="muted small">{g.status === "running" ? uptimeText(g.uptime) : "—"}</td>
                  <td>{backupCell(g)}</td>
                  {canOperate && (
                    <td style={{ whiteSpace: "nowrap" }}>
                      {g.template ? null : busy === g.vmid ? (
                        <span className="muted small">{t("läuft …")}</span>
                      ) : g.status === "running" ? (
                        <>
                          <button className="btn ghost sm" disabled={control.isPending} onClick={() => act(g, "shutdown")}
                            title={t("Sauber herunterfahren (ACPI bzw. Container-Shutdown)")}>{t("Herunterfahren")}</button>
                          <button className="btn ghost sm" style={{ marginLeft: 4 }} disabled={control.isPending} onClick={() => act(g, "reboot")}>{t("Neustart")}</button>
                          <button className="btn ghost sm" style={{ marginLeft: 4 }} disabled={control.isPending} onClick={() => act(g, "stop")}
                            title={t("Sofort ausschalten, wie Stecker ziehen")}>{t("Stoppen")}</button>
                        </>
                      ) : (
                        <button className="btn ghost sm" disabled={control.isPending} onClick={() => act(g, "start")}>{t("Starten")}</button>
                      )}
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {message && <p className={message.bad ? "error small" : "muted small"} style={{ marginTop: 6 }}>{message.text}</p>}
      <p className="muted small" style={{ marginTop: 6 }}>
        {t("Backups gelten hier ab {h} Stunden als veraltet. Für Alarme den Check „Proxmox-Backups“ in einer Richtlinie zuweisen.", { h: STALE_HOURS })}
      </p>
    </section>
  );
}
