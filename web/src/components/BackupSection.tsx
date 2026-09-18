import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { Device, GeneralSettings, Policy, PolicyBackup, Script } from "../types";

// Backup-Bereich einer Richtlinie: was gesichert wird, wann, und was danach passiert.
// Anders als Tasks plant der SERVER die Backups – nur so gehen die Startbedingung
// („erst wenn Gerät X erreichbar ist") und die Folgeaktion auf einem anderen Gerät.
// Die Uhrzeit gilt in der zentral eingestellten Zeitzone (Einstellungen → Zeitzone).

const WD: [number, string][] = [[1, "Mo"], [2, "Di"], [3, "Mi"], [4, "Do"], [5, "Fr"], [6, "Sa"], [0, "So"]];
const MODES: [string, string][] = [["snapshot", "Snapshot (laufend)"], ["suspend", "Suspend"], ["stop", "Stop"]];
const AFTER: [string, string][] = [["always", "immer"], ["success", "nur bei Erfolg"], ["failure", "nur bei Fehler"]];

type Draft = {
  id?: string;
  name: string;
  type: "proxmox" | "script";
  enabled: boolean;
  host: string;        // nur zur Gastauswahl im Formular
  all: boolean;
  vmids: number[];
  storage: string;                    // Vorgabe für alle Gäste
  targets: Record<number, string>;    // abweichendes Ziel je VMID
  mode: string;
  scriptId: string;
  weekdays: number[];
  atTime: string;
  waitDeviceId: string;
  waitMinutes: string;
  timeoutMinutes: string;
  afterDeviceId: string;
  afterScriptId: string;
  afterWhen: string;
};

const emptyDraft = (): Draft => ({
  name: "", type: "proxmox", enabled: true, host: "", all: true, vmids: [], storage: "", targets: {}, mode: "snapshot",
  scriptId: "", weekdays: [], atTime: "18:00", waitDeviceId: "", waitMinutes: "30", timeoutMinutes: "720",
  afterDeviceId: "", afterScriptId: "", afterWhen: "always",
});

// draftFrom füllt das Formular aus einem gespeicherten Eintrag.
function draftFrom(b: PolicyBackup, hosts: Device[]): Draft {
  const cfg = b.config ?? {};
  const vmids = Array.isArray(cfg.vmids) ? (cfg.vmids as number[]) : [];
  // Host für die Gastauswahl raten: der erste PVE-Host, der die VMIDs kennt.
  const host = hosts.find((h) => (h.proxmox_guests ?? []).some((g) => vmids.includes(g.vmid)))?.id ?? hosts[0]?.id ?? "";
  return {
    id: b.id, name: b.name, type: b.type, enabled: b.enabled, host,
    all: cfg.all === true, vmids, storage: String(cfg.storage ?? ""), targets: targetsFrom(cfg.targets),
    mode: String(cfg.mode ?? "snapshot"),
    scriptId: b.script_id ?? "",
    weekdays: b.weekdays ? b.weekdays.split(",").map((n) => Number(n.trim())).filter((n) => !Number.isNaN(n)) : [],
    atTime: b.at_time || "18:00",
    waitDeviceId: b.wait_device_id ?? "", waitMinutes: String(b.wait_minutes || 30),
    timeoutMinutes: String(b.timeout_minutes || 720),
    afterDeviceId: b.after_device_id ?? "", afterScriptId: b.after_script_id ?? "", afterWhen: b.after_when || "always",
  };
}

// targetsFrom liest die Ziel-Zuordnung aus der gespeicherten Config ({"110": "…"}).
function targetsFrom(raw: unknown): Record<number, string> {
  const out: Record<number, string> = {};
  if (raw && typeof raw === "object") {
    for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
      const vmid = Number(k);
      if (!Number.isNaN(vmid) && typeof v === "string" && v.trim()) out[vmid] = v.trim();
    }
  }
  return out;
}

// scheduleLabel beschreibt den Zeitplan eines Eintrags in einer Zeile.
function scheduleLabel(b: PolicyBackup, t: (s: string) => string, tz: string): string {
  const days = b.weekdays
    ? b.weekdays.split(",").map((n) => WD.find(([v]) => v === Number(n.trim()))?.[1] ?? n).join(", ")
    : t("täglich");
  return `${days}, ${b.at_time} ${tz ? `(${tz})` : t("(Serverzeit)")}`;
}

export function BackupSection({ policy, scripts, devices }: { policy: Policy; scripts: Script[]; devices: Device[] }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [err, setErr] = useState("");
  const set = (patch: Partial<Draft>) => setDraft((d) => (d ? { ...d, ...patch } : d));

  // Zeitzone nur zum Beschriften – leer heißt "Serverzeit wie bisher".
  const { data: general } = useQuery<GeneralSettings>({
    queryKey: ["settings", "general"],
    queryFn: () => api.get("/settings/general"),
    staleTime: 5 * 60 * 1000,
  });
  const tz = general?.timezone ?? "";

  const pveHosts = devices.filter((d) => d.proxmox_version);
  const host = devices.find((d) => d.id === draft?.host);
  const guests = (host?.proxmox_guests ?? []).filter((g) => !g.template);
  // Die backup-fähigen Speicher meldet der Agent (proxmox_storages). Ältere Agenten
  // kennen das Feld nicht – dann bleiben die Speicher aus den letzten Backups übrig.
  const storages = Array.from(new Set([
    ...(host?.proxmox_storages ?? []).map((st) => st.name),
    ...pveHosts.flatMap((h) => (h.proxmox_guests ?? []).map((g) => g.backup_storage).filter(Boolean) as string[]),
  ])).sort();

  const invalidate = () => qc.invalidateQueries({ queryKey: ["policies"] });
  const save = useMutation({
    mutationFn: (d: Draft) => {
      const body = {
        name: d.name.trim(), type: d.type, enabled: d.enabled,
        config: d.type === "proxmox"
          ? {
            all: d.all, vmids: d.all ? [] : d.vmids, storage: d.storage.trim(), mode: d.mode,
            // Nur echte Abweichungen speichern – sonst wäre jeder Eintrag auf einen
            // Agenten ab 0.16.0 angewiesen.
            targets: Object.fromEntries(
              Object.entries(d.targets).filter(([, v]) => v && v !== d.storage.trim()),
            ),
          }
          : {},
        script_id: d.type === "script" ? d.scriptId : null,
        weekdays: d.weekdays.slice().sort().join(","),
        at_time: d.atTime,
        wait_device_id: d.waitDeviceId || null,
        wait_minutes: Number(d.waitMinutes || 0),
        timeout_minutes: Number(d.timeoutMinutes || 720),
        after_device_id: d.afterDeviceId || null,
        after_script_id: d.afterDeviceId ? d.afterScriptId : null,
        after_when: d.afterWhen,
      };
      return d.id ? api.put(`/backups/${d.id}`, body) : api.post(`/policies/${policy.id}/backups`, body);
    },
    onSuccess: () => { setDraft(null); setErr(""); invalidate(); },
    onError: (e: Error) => setErr(e.message),
  });
  const del = useMutation({ mutationFn: (id: string) => api.del(`/backups/${id}`), onSuccess: invalidate });
  const toggle = useMutation({
    mutationFn: (b: PolicyBackup) => api.put(`/backups/${b.id}`, { ...b, enabled: !b.enabled }),
    onSuccess: invalidate,
  });

  const deviceName = (id?: string | null) => devices.find((d) => d.id === id)?.hostname ?? "—";
  const scriptName = (id?: string | null) => scripts.find((s) => s.id === id)?.name ?? "—";

  return (
    <section className="card">
      <h2>{t("Backups")}</h2>
      <p className="muted small">
        {t("Zeitgesteuerte Sicherungen auf den zugewiesenen Geräten. Proxmox-Einträge laufen nur auf Proxmox-Hosts; nach dem Lauf kann ein Skript auf einem anderen Gerät starten (z. B. Backup-Ziel schlafen legen).")}
      </p>

      {(policy.backups ?? []).map((b) => (
        <div key={b.id} className="list-row">
          <span className="link-strong" style={{ cursor: "pointer" }} onClick={() => setDraft(draftFrom(b, pveHosts))}>
            {b.name}
          </span>
          <span className="muted small">
            {b.type === "proxmox" ? "Proxmox" : t("Skript")}
            {b.type === "proxmox"
              ? ` · ${b.config?.all ? t("alle Gäste") : `${(b.config?.vmids as number[] ?? []).length} ${t("Gäste")}`}${b.config?.storage ? ` → ${b.config.storage}` : ""}`
              : ` · ${scriptName(b.script_id)}`}
            {" · "}{scheduleLabel(b, t, tz)}
            {b.wait_device_id ? ` · ${t("wartet auf")} ${deviceName(b.wait_device_id)}` : ""}
            {b.after_device_id ? ` · ${t("danach")} ${scriptName(b.after_script_id)} ${t("auf")} ${deviceName(b.after_device_id)}` : ""}
            {!b.enabled && ` · ${t("deaktiviert")}`}
          </span>
          <button className="btn ghost sm" onClick={() => toggle.mutate(b)}>{b.enabled ? t("Pause") : t("Aktivieren")}</button>
          <button className="btn ghost sm" onClick={() => del.mutate(b.id)}>×</button>
        </div>
      ))}

      {draft === null ? (
        <button className="btn primary" style={{ marginTop: 10 }} onClick={() => setDraft(emptyDraft())}>
          + {t("Backup")}
        </button>
      ) : (
        <form className="backup-form" onSubmit={(e) => { e.preventDefault(); save.mutate(draft); }}>
          <div className="inline-form">
            <input placeholder={t("Name")} value={draft.name} onChange={(e) => set({ name: e.target.value })} />
            <select value={draft.type} onChange={(e) => set({ type: e.target.value as Draft["type"] })}>
              <option value="proxmox">{t("Proxmox (VMs/Container)")}</option>
              <option value="script">{t("Skript (z. B. Windows-PC)")}</option>
            </select>
            <label className="chip" title={t("Deaktivierte Einträge werden nicht eingeplant.")}>
              <input type="checkbox" checked={draft.enabled} onChange={(e) => set({ enabled: e.target.checked })} /> {t("aktiv")}
            </label>
          </div>

          {draft.type === "proxmox" ? (
            <>
              <div className="inline-form">
                <select value={draft.host} onChange={(e) => set({ host: e.target.value })} title={t("Host, aus dem die Gäste gewählt werden")}>
                  <option value="">{t("— Host für die Gastauswahl —")}</option>
                  {pveHosts.map((h) => <option key={h.id} value={h.id}>{h.hostname}</option>)}
                </select>
                <label className="chip">
                  <input type="checkbox" checked={draft.all} onChange={(e) => set({ all: e.target.checked })} /> {t("alle Gäste des Hosts")}
                </label>
                <input list="backup-storages" placeholder={t("Ziel als Vorgabe (z. B. backup-pi_1)")} value={draft.storage}
                  onChange={(e) => set({ storage: e.target.value })} style={{ minWidth: 190 }} />
                <datalist id="backup-storages">{storages.map((s) => <option key={s} value={s} />)}</datalist>
                <select value={draft.mode} onChange={(e) => set({ mode: e.target.value })} title={t("Sicherungsmodus")}>
                  {MODES.map(([k, v]) => <option key={k} value={k}>{t(v)}</option>)}
                </select>
              </div>
              {/* Ein Eintrag, mehrere Ziele: je Gast lässt sich ein abweichender Speicher
                  wählen – sonst bräuchte man je Ziel einen eigenen Eintrag mit eigenem
                  Zeitplan. Leeres Ziel heißt: Vorgabe von oben. */}
              <div className="backup-guests">
                {guests.length === 0 ? (
                  <span className="muted small">{t("Host wählen, um Gäste anzuzeigen.")}</span>
                ) : (
                  <table className="backup-guest-table">
                    <thead>
                      <tr>
                        <th>{t("Gast")}</th>
                        <th>{t("Ziel")}</th>
                        <th>{draft.all ? t("gesichert") : t("An")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {guests.map((g) => {
                        const on = draft.all || draft.vmids.includes(g.vmid);
                        return (
                          <tr key={g.vmid} className={on ? "" : "muted"}>
                            <td>{g.vmid} {g.name}</td>
                            <td>
                              <select value={draft.targets[g.vmid] ?? ""} disabled={!on}
                                onChange={(e) => {
                                  const next = { ...draft.targets };
                                  if (e.target.value) next[g.vmid] = e.target.value; else delete next[g.vmid];
                                  set({ targets: next });
                                }}>
                                <option value="">{draft.storage ? `${t("Vorgabe")} (${draft.storage})` : t("(Vorgabe)")}</option>
                                {storages.map((st) => <option key={st} value={st}>{st}</option>)}
                              </select>
                            </td>
                            <td>
                              <input type="checkbox" checked={on} disabled={draft.all}
                                title={draft.all ? t("Auswahl steht auf „alle Gäste des Hosts“.") : ""}
                                onChange={(e) => set({ vmids: e.target.checked ? [...draft.vmids, g.vmid] : draft.vmids.filter((v) => v !== g.vmid) })} />
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                )}
              </div>
            </>
          ) : (
            <div className="inline-form">
              <select value={draft.scriptId} onChange={(e) => set({ scriptId: e.target.value })}>
                <option value="">{t("— Skript wählen —")}</option>
                {scripts.map((s) => <option key={s.id} value={s.id}>{s.name} ({s.shell})</option>)}
              </select>
            </div>
          )}

          <div className="inline-form">
            <span className="muted small">{t("Zeitplan")}:</span>
            {WD.map(([v, label]) => (
              <label key={v} className={draft.weekdays.includes(v) ? "chip chip-on" : "chip"}>
                <input type="checkbox" checked={draft.weekdays.includes(v)}
                  onChange={(e) => set({ weekdays: e.target.checked ? [...draft.weekdays, v] : draft.weekdays.filter((d) => d !== v) })} />
                {t(label)}
              </label>
            ))}
            <label className="num">{t("Uhrzeit")}<input type="time" value={draft.atTime} onChange={(e) => set({ atTime: e.target.value })} /></label>
            <span className="muted small">
              {tz ? `(${tz}${t("; keine Auswahl = täglich")})` : t("(Serverzeit; keine Auswahl = täglich)")}
            </span>
          </div>

          <div className="inline-form">
            <span className="muted small">{t("Startbedingung")}:</span>
            <select value={draft.waitDeviceId} onChange={(e) => set({ waitDeviceId: e.target.value })}
              title={t("Erst starten, wenn dieses Gerät erreichbar ist – z. B. das Backup-Ziel.")}>
              <option value="">{t("— sofort starten —")}</option>
              {devices.map((d) => <option key={d.id} value={d.id}>{d.hostname}</option>)}
            </select>
            {draft.waitDeviceId && (
              <label className="num" title={t("So lange wird auf das Gerät gewartet, danach gilt der Lauf als fehlgeschlagen.")}>
                {t("warten (min)")}<input type="number" min={1} value={draft.waitMinutes} onChange={(e) => set({ waitMinutes: e.target.value })} />
              </label>
            )}
            <label className="num" title={t("Bricht den Lauf ab, wenn er so lange dauert.")}>
              {t("Zeitlimit (min)")}<input type="number" min={10} value={draft.timeoutMinutes} onChange={(e) => set({ timeoutMinutes: e.target.value })} />
            </label>
          </div>

          <div className="inline-form">
            <span className="muted small">{t("Danach")}:</span>
            <select value={draft.afterDeviceId} onChange={(e) => set({ afterDeviceId: e.target.value })}>
              <option value="">{t("— keine Folgeaktion —")}</option>
              {devices.map((d) => <option key={d.id} value={d.id}>{d.hostname}</option>)}
            </select>
            {draft.afterDeviceId && (
              <>
                <select value={draft.afterScriptId} onChange={(e) => set({ afterScriptId: e.target.value })}>
                  <option value="">{t("— Skript wählen —")}</option>
                  {scripts.map((s) => <option key={s.id} value={s.id}>{s.name} ({s.shell})</option>)}
                </select>
                <select value={draft.afterWhen} onChange={(e) => set({ afterWhen: e.target.value })}>
                  {AFTER.map(([k, v]) => <option key={k} value={k}>{t(v)}</option>)}
                </select>
              </>
            )}
          </div>
          {draft.afterDeviceId && (
            <p className="muted small">
              {t("Das Skript bekommt das Ergebnis als Umgebungsvariablen: ROSTER_BACKUP_STATUS (ok/failed/timeout), _NAME, _SUMMARY, _EXIT, _DEVICE, _DURATION_SEC.")}
            </p>
          )}

          <div className="inline-form">
            <button className="btn primary" type="submit" disabled={save.isPending}>{t("Speichern")}</button>
            <button className="btn" type="button" onClick={() => { setDraft(null); setErr(""); }}>{t("Abbrechen")}</button>
            {err && <span className="error small">{err}</span>}
          </div>
        </form>
      )}
    </section>
  );
}
