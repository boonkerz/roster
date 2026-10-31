import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import type { Policy, PolicyFIMRule, PolicySecurity } from "../types";

// Sicherheitsüberwachung einer Richtlinie: Datei-Integrität (überwachte Pfade, Intervall)
// und Anmeldungs-Überwachung. Bei mehreren Richtlinien werden die Regeln vereinigt, die
// Schalter per ODER verknüpft und das kleinste Intervall genommen.

const PLATFORMS: [string, string][] = [["linux", "Linux"], ["windows", "Windows"], ["darwin", "macOS"]];

// Standardpfade je Plattform (nur Dateien, keine Registry).
const TEMPLATES: { path: string; recursive: boolean; exclude: string[]; platforms: string[] }[] = [
  { path: "/etc", recursive: true, exclude: ["/etc/mtab", "*.dpkg-new", "/etc/ld.so.cache", "/etc/resolv.conf", "/etc/adjtime"], platforms: ["linux"] },
  { path: "/usr/local/bin", recursive: true, exclude: [], platforms: ["linux", "darwin"] },
  { path: "/usr/local/sbin", recursive: true, exclude: [], platforms: ["linux"] },
  { path: "/root/.ssh", recursive: true, exclude: [], platforms: ["linux"] },
  { path: "/home/*/.ssh/authorized_keys", recursive: false, exclude: [], platforms: ["linux"] },
  { path: "/var/spool/cron", recursive: true, exclude: [], platforms: ["linux"] },
  { path: "/Library/LaunchDaemons", recursive: true, exclude: [], platforms: ["darwin"] },
  { path: "/Library/LaunchAgents", recursive: true, exclude: [], platforms: ["darwin"] },
  { path: "C:\\Windows\\System32\\drivers\\etc", recursive: true, exclude: [], platforms: ["windows"] },
  { path: "C:\\Windows\\System32\\Tasks", recursive: true, exclude: [], platforms: ["windows"] },
  { path: "C:\\ProgramData\\Microsoft\\Windows\\Start Menu\\Programs\\StartUp", recursive: true, exclude: [], platforms: ["windows"] },
];

type Draft = { path: string; recursive: boolean; exclude: string; platforms: string[]; alert: boolean };
const emptyDraft = (): Draft => ({ path: "", recursive: true, exclude: "", platforms: [], alert: true });
const draftFrom = (r: PolicyFIMRule): Draft => ({ path: r.path, recursive: r.recursive, exclude: (r.exclude ?? []).join("\n"), platforms: r.platforms ?? [], alert: r.alert });

export function SecuritySection({ policy }: { policy: Policy }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const sec = policy.security;
  const rules = policy.fim_rules ?? [];
  const [interval, setInterval_] = useState(60);
  const [auth, setAuth] = useState(false);
  const [err, setErr] = useState("");
  const [draft, setDraft] = useState<Draft>(emptyDraft());
  const [editId, setEditId] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);

  useEffect(() => {
    setInterval_(sec?.fim_interval_min ?? 60);
    setAuth(sec?.auth_monitor ?? false);
    setErr("");
  }, [policy.id, sec?.fim_interval_min, sec?.auth_monitor]);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["policies"] });
  const saveSec = useMutation({
    mutationFn: (patch: Partial<PolicySecurity>) => api.put(`/policies/${policy.id}/security`, { ...(sec ?? {}), fim_interval_min: interval, auth_monitor: auth, ...patch }),
    onSuccess: () => { setErr(""); invalidate(); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });
  const saveRule = useMutation({
    mutationFn: (d: Draft) => {
      const body = { path: d.path, recursive: d.recursive, exclude: d.exclude.split("\n").map((x) => x.trim()).filter(Boolean), platforms: d.platforms, alert: d.alert };
      return editId ? api.put(`/fim/${editId}`, body) : api.post(`/policies/${policy.id}/fim`, body);
    },
    onSuccess: () => { setErr(""); setDraft(emptyDraft()); setEditId(null); setShowForm(false); invalidate(); },
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });
  const delRule = useMutation({ mutationFn: (id: string) => api.del(`/fim/${id}`), onSuccess: invalidate });
  const addTemplates = useMutation({
    mutationFn: async () => {
      const have = new Set(rules.map((r) => r.path));
      for (const tpl of TEMPLATES) {
        if (have.has(tpl.path)) continue;
        await api.post(`/policies/${policy.id}/fim`, { ...tpl, alert: true });
      }
    },
    onSuccess: invalidate,
    onError: (e) => setErr(e instanceof ApiError ? e.message : String(e)),
  });

  const dirtySec = interval !== (sec?.fim_interval_min ?? 60) || auth !== (sec?.auth_monitor ?? false);
  const togglePlatform = (p: string) => setDraft((d) => ({ ...d, platforms: d.platforms.includes(p) ? d.platforms.filter((x) => x !== p) : [...d.platforms, p] }));

  return (
    <section className="card">
      <h2>{t("Sicherheitsüberwachung")}</h2>
      <p className="muted small">
        {t("Datei-Integrität: Der Agent merkt sich Hash, Rechte und Besitzer der überwachten Pfade und meldet jede Abweichung. Anmeldungen: fehlgeschlagene/erfolgreiche Logins, sudo und Kontoänderungen werden gesammelt und auf Brute-Force oder neue Quell-IPs geprüft. Bei mehreren Richtlinien werden Regeln vereinigt.")}
      </p>

      <h3 className="muted small" style={{ marginTop: 14 }}>{t("Datei-Integrität (FIM)")} <span className="tab-count">{rules.length}</span></h3>
      {rules.length === 0 && <p className="muted small">{t("Noch keine überwachten Pfade.")}</p>}
      {rules.length > 0 && (
        <table className="table">
          <thead><tr><th>{t("Pfad")}</th><th>{t("Rekursiv")}</th><th>{t("Ausnahmen")}</th><th>{t("Plattform")}</th><th>{t("Alarm")}</th><th></th></tr></thead>
          <tbody>
            {rules.map((r) => (
              <tr key={r.id} className="small">
                <td className="mono">{r.path}</td>
                <td>{r.recursive ? t("ja") : t("nein")}</td>
                <td className="muted mono">{(r.exclude ?? []).join(", ") || "—"}</td>
                <td className="muted">{(r.platforms ?? []).length ? r.platforms.join(", ") : t("alle")}</td>
                <td>{r.alert ? "🔔" : <span className="muted">—</span>}</td>
                <td style={{ textAlign: "right", whiteSpace: "nowrap" }}>
                  <button className="btn ghost sm" onClick={() => { setEditId(r.id); setDraft(draftFrom(r)); setShowForm(true); }}>{t("Bearbeiten")}</button>
                  <button className="btn ghost sm" onClick={() => { if (confirm(t("Regel löschen?"))) delRule.mutate(r.id); }}>✕</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="inline-form" style={{ marginTop: 8 }}>
        {!showForm && <button className="btn ghost sm" onClick={() => { setEditId(null); setDraft(emptyDraft()); setShowForm(true); }}>+ {t("Pfad überwachen")}</button>}
        <button className="btn ghost sm" disabled={addTemplates.isPending} onClick={() => addTemplates.mutate()} title={t("Typische Systempfade je Plattform (/etc, /usr/local/bin, ~/.ssh, cron; Windows hosts/Tasks/Autostart; macOS LaunchDaemons)")}>{t("Standardpfade einfügen")}</button>
        <label className="num" style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>{t("Prüfabstand (Min.)")}
          <input type="number" min={5} max={1440} value={interval} onChange={(e) => setInterval_(Number(e.target.value))} style={{ width: 80 }} />
        </label>
      </div>
      {showForm && (
        <form className="form-col" style={{ marginTop: 10, maxWidth: 640 }} onSubmit={(e) => { e.preventDefault(); saveRule.mutate(draft); }}>
          <label className="field"><span>{t("Pfad (Datei, Verzeichnis oder Glob, z. B. /home/*/.ssh/authorized_keys)")}</span>
            <input className="mono" value={draft.path} onChange={(e) => setDraft({ ...draft, path: e.target.value })} placeholder="/etc" required />
          </label>
          <div className="inline-form">
            <label className="chip"><input type="checkbox" checked={draft.recursive} onChange={(e) => setDraft({ ...draft, recursive: e.target.checked })} /> {t("Unterverzeichnisse einschließen")}</label>
            <label className="chip"><input type="checkbox" checked={draft.alert} onChange={(e) => setDraft({ ...draft, alert: e.target.checked })} /> {t("Änderungen alarmieren")}</label>
            {PLATFORMS.map(([k, label]) => (
              <label key={k} className={`chip ${draft.platforms.includes(k) ? "chip-on" : ""}`}><input type="checkbox" checked={draft.platforms.includes(k)} onChange={() => togglePlatform(k)} /> {label}</label>
            ))}
            <span className="muted small">{t("keine Plattform = alle")}</span>
          </div>
          <label className="field"><span>{t("Ausnahmen (ein Glob-Muster je Zeile: *.log, cache/, /var/lib/tmp)")}</span>
            <textarea className="notes-input" rows={2} value={draft.exclude} onChange={(e) => setDraft({ ...draft, exclude: e.target.value })} />
          </label>
          <div className="inline-form">
            <button className="btn primary sm" type="submit" disabled={saveRule.isPending}>{editId ? t("Speichern") : t("Hinzufügen")}</button>
            <button className="btn ghost sm" type="button" onClick={() => { setShowForm(false); setEditId(null); setDraft(emptyDraft()); }}>{t("Abbrechen")}</button>
          </div>
        </form>
      )}

      <h3 className="muted small" style={{ marginTop: 18 }}>{t("Anmeldungen")}</h3>
      <label className="chip" style={{ display: "inline-flex", whiteSpace: "nowrap" }}>
        <input type="checkbox" checked={auth} onChange={(e) => setAuth(e.target.checked)} /> {t("Anmeldungen überwachen (SSH/RDP/Konsole, sudo, Kontoänderungen)")}
      </label>
      <p className="muted small">{t("Linux liest journald bzw. /var/log/auth.log, Windows das Sicherheitsprotokoll (Anmeldeüberwachung muss aktiv sein). Schwellwerte für Brute-Force stehen in den Einstellungen.")}</p>

      {err && <p className="error small">{err}</p>}
      {dirtySec && (
        <div className="inline-form">
          <button className="btn primary sm" disabled={saveSec.isPending} onClick={() => saveSec.mutate({})}>{t("Speichern")}</button>
        </div>
      )}
    </section>
  );
}
