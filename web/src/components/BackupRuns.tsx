import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import { relTime } from "./StatusBadge";
import type { BackupRun, Device, Policy } from "../types";

// Lauf-Historie der Backups eines Geräts, plus „Jetzt starten" für die Einträge, die
// auf dieses Gerät zutreffen. Läuft ein Backup gerade, zeigt die Zeile den Zwischenstand
// beim nächsten Aktualisieren – der Agent meldet ihn fortlaufend.

const STATUS: Record<string, { label: string; cls: string }> = {
  ok: { label: "erfolgreich", cls: "badge-online" },
  running: { label: "läuft", cls: "badge-warn" },
  waiting: { label: "wartet", cls: "badge-unknown" },
  failed: { label: "fehlgeschlagen", cls: "badge-offline" },
  timeout: { label: "Zeitlimit", cls: "badge-offline" },
  skipped: { label: "übersprungen", cls: "badge-unknown" },
};

function duration(run: BackupRun): string {
  if (!run.started_at) return "—";
  const end = run.finished_at ? new Date(run.finished_at) : new Date();
  const sec = Math.max(0, Math.round((end.getTime() - new Date(run.started_at).getTime()) / 1000));
  if (sec < 60) return `${sec} s`;
  const min = Math.floor(sec / 60);
  return min < 60 ? `${min} min` : `${Math.floor(min / 60)} h ${min % 60} min`;
}

export function BackupRuns({ device, canOperate }: { device: Device; canOperate?: boolean }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [open, setOpen] = useState<string | null>(null);
  const [msg, setMsg] = useState("");

  const { data: runs } = useQuery({
    queryKey: ["backup-runs", device.id],
    queryFn: () => api.get<BackupRun[]>(`/devices/${device.id}/backup-runs?limit=50`),
    refetchInterval: 30000,
  });
  // Einträge, die für dieses Gerät in Frage kommen (für „Jetzt starten").
  const { data: policies } = useQuery({ queryKey: ["policies"], queryFn: () => api.get<Policy[]>("/policies") });
  const entries = (policies ?? []).flatMap((p) => p.backups ?? [])
    .filter((b) => b.type !== "proxmox" || !!device.proxmox_version);

  const start = useMutation({
    mutationFn: (backupID: string) => api.post(`/backups/${backupID}/run`, { device_id: device.id }),
    onSuccess: () => {
      setMsg(t("Lauf gestartet – der Stand aktualisiert sich automatisch."));
      qc.invalidateQueries({ queryKey: ["backup-runs", device.id] });
    },
    onError: (e: Error) => setMsg(e.message),
  });

  return (
    <section className="card" style={{ marginTop: 12 }}>
      <h3 className="muted small" style={{ margin: 0 }}>{t("Backups")}</h3>
      {canOperate && entries.length > 0 && (
        <div className="inline-form" style={{ marginTop: 8 }}>
          <span className="muted small">{t("Jetzt starten")}:</span>
          {entries.map((b) => (
            <button key={b.id} className="btn ghost sm" disabled={start.isPending} onClick={() => start.mutate(b.id)}>
              ▶ {b.name}
            </button>
          ))}
        </div>
      )}
      {msg && <p className="muted small">{msg}</p>}

      {(runs ?? []).length === 0 ? (
        <p className="muted small">{t("Noch keine Backup-Läufe für dieses Gerät.")}</p>
      ) : (
        <div className="scroll-list">
          <table className="table">
            <thead>
              <tr><th>{t("Status")}</th><th>{t("Backup")}</th><th>{t("Start")}</th><th>{t("Dauer")}</th><th>{t("Ergebnis")}</th></tr>
            </thead>
            <tbody>
              {(runs ?? []).map((r) => {
                const st = STATUS[r.status] ?? { label: r.status, cls: "badge-unknown" };
                return (
                  <tr key={r.id}>
                    <td><span className={`badge ${st.cls}`}>{t(st.label)}</span></td>
                    <td className="link-strong">{r.backup_name || "—"}
                      {r.trigger_type === "manual" && <span className="muted small"> ({t("manuell")})</span>}
                    </td>
                    <td className="muted small" title={r.started_at ?? r.scheduled_at}>{relTime(r.started_at ?? r.scheduled_at)}</td>
                    <td className="muted small">{duration(r)}</td>
                    <td className="small">
                      {r.summary || "—"}
                      {r.output && (
                        <button className="btn ghost sm" style={{ marginLeft: 6 }}
                          onClick={() => setOpen(open === r.id ? null : r.id)}>
                          {open === r.id ? t("Protokoll ausblenden") : t("Protokoll")}
                        </button>
                      )}
                      {open === r.id && <pre className="code-block">{r.output}</pre>}
                    </td>
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
