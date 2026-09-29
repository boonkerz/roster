import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import { relTime } from "./StatusBadge";
import type { BackupRun, Command, Device, Policy, PolicyBackup } from "../types";

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

  // Aufräumen: Proxmox-Einträge mit Aufbewahrung („behalte N") können alte Archive
  // sofort entfernen – erst als Trockenlauf, dann echt. Das Ergebnis kommt als
  // Befehl zurück und wird hier abgefragt, bis der Agent fertig ist.
  const pruneable = entries.filter((b) => b.type === "proxmox" && Number(b.config?.keep_last) > 0);
  const [prune, setPrune] = useState<{ id: string; dry: boolean; name: string } | null>(null);
  const [pruneCmd, setPruneCmd] = useState<Command | null>(null);
  const runPrune = useMutation({
    mutationFn: (p: { id: string; dry: boolean }) =>
      api.post<{ command_id: string }>(`/backups/${p.id}/prune`, { device_id: device.id, dry_run: p.dry }),
    onSuccess: (r, p) => { setPrune({ id: r.command_id, dry: p.dry, name: "" }); setPruneCmd(null); setMsg(""); },
    onError: (e: Error) => setMsg(e.message),
  });
  useEffect(() => {
    if (!prune || pruneCmd?.status === "done") return;
    const tick = async () => {
      try {
        const c = await api.get<Command>(`/commands/${prune.id}`);
        setPruneCmd(c);
      } catch { /* nächster Versuch */ }
    };
    tick();
    const h = setInterval(tick, 3000);
    return () => clearInterval(h);
  }, [prune, pruneCmd?.status]);
  const startPrune = (b: PolicyBackup, dry: boolean) => {
    if (!dry && !window.confirm(t("Alte Sicherungen von „{name}“ auf diesem Host jetzt löschen? Je Gast bleiben die letzten {n}.", { name: b.name, n: Number(b.config?.keep_last) }))) return;
    runPrune.mutate({ id: b.id, dry });
  };

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
      {canOperate && pruneable.length > 0 && (
        <div className="inline-form" style={{ marginTop: 6 }}>
          <span className="muted small" title={t("Entfernt je Gast alle Sicherungen bis auf die letzten N (Aufbewahrung des Eintrags) – ohne vorher neu zu sichern.")}>{t("Alte Sicherungen aufräumen")}:</span>
          {pruneable.map((b) => (
            <span key={b.id} className="inline-form" style={{ gap: 4 }}>
              <button className="btn ghost sm" disabled={runPrune.isPending} onClick={() => startPrune(b, true)}>{b.name} · {t("prüfen")}</button>
              <button className="btn ghost sm" disabled={runPrune.isPending} onClick={() => startPrune(b, false)}>{t("aufräumen")}</button>
            </span>
          ))}
        </div>
      )}
      {prune && (
        <div className="small" style={{ marginTop: 6 }}>
          {!pruneCmd || pruneCmd.status !== "done"
            ? <span className="muted">{t("Aufräumen läuft …")}{pruneCmd?.output ? ` ${pruneCmd.output}` : ""}</span>
            : <>
              <span className={pruneCmd.exit_code === 0 ? "form-ok" : "form-err"}>{(pruneCmd.output ?? "").split("\n")[0]}</span>
              <button className="btn ghost sm" style={{ marginLeft: 6 }} onClick={() => setPrune(null)}>✕</button>
              <pre className="code-block">{pruneCmd.output}</pre>
            </>}
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
