import { FormEvent, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import { useAuth } from "../auth";
import type { AlertChannel, AlertProvider, AlertsResponse, AuditEntry, ChannelScope, ClientTree, CustomField, CustomFieldType, CustomRole, DeployPackage, Device, EnrollmentToken, MaintenanceWindow, ReportSchedule, User } from "../types";

export function Settings() {
  const { t } = useI18n();
  return (
    <div className="page">
      <header className="page-head"><h1>{t("Einstellungen")}</h1></header>
      <TwoFactor />
      <Alerts />
      <Maintenance />
      <Reports />
      <CustomFields />
      <SoftwarePackages />
      <Tokens />
      <Roles />
      <Users />
      <AuditLog />
    </div>
  );
}

// permLabel liefert die Anzeige für einen Permission-Key.
function usePermLabels() {
  const { t } = useI18n();
  return (key: string) => ({
    "page.dashboard": t("Übersicht"),
    "page.devices": t("Geräte (ansehen)"),
    "page.policies": t("Richtlinien"),
    "page.scripts": t("Skripte"),
    "page.settings": t("Einstellungen/Verwaltung"),
    "devices.operate": t("Geräte bedienen (Terminal, Fernsteuerung, Skripte …)"),
  }[key] ?? key);
}

// Roles verwaltet Custom-Rollen (wiederverwendbare Rechte-Sets).
function Roles() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const permLabel = usePermLabels();
  const [name, setName] = useState("");
  const [perms, setPerms] = useState<string[]>([]);
  const [editId, setEditId] = useState<string | null>(null);

  const { data } = useQuery({
    queryKey: ["roles"],
    queryFn: () => api.get<{ roles: CustomRole[]; permissions: string[] }>("/roles"),
  });
  const catalog = data?.permissions ?? [];
  const invalidate = () => qc.invalidateQueries({ queryKey: ["roles"] });
  const reset = () => { setName(""); setPerms([]); setEditId(null); };

  const save = useMutation({
    mutationFn: () => editId
      ? api.put(`/roles/${editId}`, { name, permissions: perms })
      : api.post("/roles", { name, permissions: perms }),
    onSuccess: () => { reset(); invalidate(); },
  });
  const del = useMutation({ mutationFn: (id: string) => api.del(`/roles/${id}`), onSuccess: invalidate });

  const toggle = (p: string) => setPerms((cur) => cur.includes(p) ? cur.filter((x) => x !== p) : [...cur, p]);
  const edit = (r: CustomRole) => { setEditId(r.id); setName(r.name); setPerms(r.permissions); };

  return (
    <section className="card">
      <h2>{t("Rollen (Rechte-Sets)")}</h2>
      <p className="muted small">{t("Eigene Rollen mit gezielten Seiten-/Funktions-Rechten. Benutzer bekommen eine Rolle zugewiesen und sehen nur das Erlaubte. Admins haben immer alle Rechte.")}</p>
      <form className="inline-form" onSubmit={(e) => { e.preventDefault(); if (name.trim()) save.mutate(); }}>
        <input placeholder={t("Rollenname")} value={name} onChange={(e) => setName(e.target.value)} />
        <div className="perm-grid">
          {catalog.map((p) => (
            <label key={p} className="perm-item">
              <input type="checkbox" checked={perms.includes(p)} onChange={() => toggle(p)} /> {permLabel(p)}
            </label>
          ))}
        </div>
        <button className="btn primary" type="submit" disabled={save.isPending}>{editId ? t("Speichern") : t("Rolle anlegen")}</button>
        {editId && <button className="btn ghost" type="button" onClick={reset}>{t("Abbrechen")}</button>}
      </form>

      <table className="table">
        <thead><tr><th>{t("Rolle")}</th><th>{t("Rechte")}</th><th>{t("Benutzer")}</th><th></th></tr></thead>
        <tbody>
          {(data?.roles ?? []).map((r) => (
            <tr key={r.id}>
              <td>{r.name}</td>
              <td className="muted small">{r.permissions.map(permLabel).join(", ") || "—"}</td>
              <td className="muted">{r.user_count}</td>
              <td>
                <button className="btn ghost sm" onClick={() => edit(r)}>{t("Bearbeiten")}</button>
                <button className="btn ghost sm" onClick={() => confirm(t("Rolle „{name}“ löschen? Zugeordnete Benutzer fallen auf ihre Grundrolle zurück.", { name: r.name })) && del.mutate(r.id)}>{t("Löschen")}</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

// SoftwarePackages verwaltet den Katalog verteilbarer Pakete (Software-Verteilung).
function SoftwarePackages() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["software-packages"], queryFn: () => api.get<DeployPackage[]>("/software-packages") });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["software-packages"] });
  const empty: DeployPackage = { id: "", name: "", winget: "", choco: "", apt: "", dnf: "", brew: "" };
  const [form, setForm] = useState<DeployPackage>(empty);

  const save = useMutation({
    mutationFn: () => form.id ? api.put(`/software-packages/${form.id}`, form) : api.post("/software-packages", form),
    onSuccess: () => { invalidate(); setForm(empty); },
  });
  const del = useMutation({ mutationFn: (id: string) => api.del(`/software-packages/${id}`), onSuccess: invalidate });
  const set = (k: keyof DeployPackage, v: string) => setForm((f) => ({ ...f, [k]: v }));

  return (
    <section className="card">
      <h2>{t("Software-Pakete")}</h2>
      <p className="muted small">{t("Verteilbare Pakete – je Paketmanager eine Kennung. Ausrollen über „Sammelaktion → Software installieren“. Der Agent nutzt den auf dem Gerät verfügbaren Manager.")}</p>
      {(data ?? []).length > 0 && (
        <table className="table">
          <thead><tr><th>{t("Name")}</th><th>winget</th><th>choco</th><th>apt</th><th>dnf</th><th>brew</th><th></th></tr></thead>
          <tbody>
            {(data ?? []).map((p) => (
              <tr key={p.id}>
                <td className="link-strong">{p.name}</td>
                <td className="muted mono small">{p.winget || "—"}</td>
                <td className="muted mono small">{p.choco || "—"}</td>
                <td className="muted mono small">{p.apt || "—"}</td>
                <td className="muted mono small">{p.dnf || "—"}</td>
                <td className="muted mono small">{p.brew || "—"}</td>
                <td>
                  <button className="btn ghost sm" onClick={() => setForm(p)}>{t("Bearbeiten")}</button>
                  <button className="btn ghost sm" onClick={() => del.mutate(p.id)}>{t("Löschen")}</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <form className="inline-form" style={{ marginTop: 10, flexWrap: "wrap" }} onSubmit={(e) => { e.preventDefault(); if (form.name) save.mutate(); }}>
        <input placeholder={t("Name")} value={form.name} onChange={(e) => set("name", e.target.value)} />
        <input placeholder="winget-ID (Mozilla.Firefox)" value={form.winget} onChange={(e) => set("winget", e.target.value)} />
        <input placeholder="choco (firefox)" value={form.choco} onChange={(e) => set("choco", e.target.value)} />
        <input placeholder="apt (firefox)" value={form.apt} onChange={(e) => set("apt", e.target.value)} />
        <input placeholder="dnf" value={form.dnf} onChange={(e) => set("dnf", e.target.value)} />
        <input placeholder="brew" value={form.brew} onChange={(e) => set("brew", e.target.value)} />
        <button className="btn primary" type="submit">{form.id ? t("Speichern") : t("Anlegen")}</button>
        {form.id && <button className="btn ghost" type="button" onClick={() => setForm(empty)}>{t("Abbrechen")}</button>}
      </form>
    </section>
  );
}

// AuditLog zeigt die jüngsten ändernden Aktionen (wer/was/wann).
function AuditLog() {
  const { t } = useI18n();
  const { data } = useQuery({ queryKey: ["audit"], queryFn: () => api.get<AuditEntry[]>("/audit"), refetchInterval: 20000 });
  return (
    <section className="card">
      <h2>{t("Audit-Log")}</h2>
      <p className="muted small">{t("Ändernde Aktionen (letzte 300). Anmeldungen inkl. Fehlversuche.")}</p>
      {(data ?? []).length === 0 ? (
        <p className="muted">{t("Noch keine Einträge.")}</p>
      ) : (
        <div className="scroll-list" style={{ maxHeight: 380 }}>
          <table className="table">
            <thead><tr><th>{t("Zeitpunkt")}</th><th>{t("Benutzer")}</th><th>{t("Aktion")}</th><th>{t("Status")}</th><th>IP</th></tr></thead>
            <tbody>
              {data!.map((e) => (
                <tr key={e.id}>
                  <td className="muted small" title={new Date(e.ts).toLocaleString()}>{new Date(e.ts).toLocaleString()}</td>
                  <td>{e.username || "—"}</td>
                  <td>{e.action}</td>
                  <td><span className={`badge ${e.status < 400 ? "badge-online" : "badge-offline"}`}>{e.status}</span></td>
                  <td className="muted mono small">{e.ip || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

// Maintenance verwaltet Wartungsfenster (unterdrücken Alarme im Zeitraum).
function Maintenance() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { data: windows } = useQuery({ queryKey: ["maintenance"], queryFn: () => api.get<MaintenanceWindow[]>("/maintenance") });
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: devices } = useQuery({ queryKey: ["devices"], queryFn: () => api.get<Device[]>("/devices") });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["maintenance"] });

  const [tType, setTType] = useState<"client" | "site" | "device">("device");
  const [target, setTarget] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [note, setNote] = useState("");
  const [msg, setMsg] = useState("");

  const targets =
    tType === "client" ? (tree?.clients ?? []).map((c) => ({ id: c.id, name: c.name }))
    : tType === "site" ? (tree?.clients ?? []).flatMap((c) => (c.sites ?? []).map((s) => ({ id: s.id, name: `${c.name} › ${s.name}` })))
    : (devices ?? []).map((d) => ({ id: d.id, name: d.hostname }));

  const create = useMutation({
    mutationFn: () => api.post("/maintenance", {
      target_type: tType, target_id: target, note,
      starts_at: new Date(start).toISOString(), ends_at: new Date(end).toISOString(),
    }),
    onSuccess: () => { invalidate(); setTarget(""); setStart(""); setEnd(""); setNote(""); setMsg(""); },
    onError: (e) => setMsg(e instanceof ApiError ? e.message : t("Fehler")),
  });
  const remove = useMutation({ mutationFn: (id: string) => api.del(`/maintenance/${id}`), onSuccess: invalidate });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setMsg("");
    if (!target) { setMsg(t("Bitte ein Ziel wählen.")); return; }
    if (!start || !end) { setMsg(t("Start und Ende angeben.")); return; }
    if (new Date(end) <= new Date(start)) { setMsg(t("Ende muss nach dem Beginn liegen.")); return; }
    create.mutate();
  };

  const typeLabel = (tt: string) => tt === "client" ? t("Client") : tt === "site" ? t("Standort") : t("Gerät");
  const state = (m: MaintenanceWindow) => {
    const now = Date.now();
    if (now < new Date(m.starts_at).getTime()) return { label: "geplant", cls: "badge-unknown" };
    if (now > new Date(m.ends_at).getTime()) return { label: "beendet", cls: "badge-unknown" };
    return { label: "aktiv", cls: "badge-warn" };
  };

  return (
    <section className="card">
      <h2>{t("Wartungsfenster")}</h2>
      <p className="muted small">{t("Im Zeitraum werden für das Ziel (und darunterliegende Geräte) keine Alarme versendet. Checks laufen und der Verlauf wird weiter protokolliert.")}</p>
      {(windows ?? []).length === 0 ? (
        <p className="muted">{t("Keine Wartungsfenster.")}</p>
      ) : (
        <table className="table">
          <thead><tr><th>{t("Status")}</th><th>{t("Ziel")}</th><th>{t("Zeitraum")}</th><th>{t("Notiz")}</th><th></th></tr></thead>
          <tbody>
            {windows!.map((m) => {
              const s = state(m);
              return (
                <tr key={m.id}>
                  <td><span className={`badge ${s.cls}`}>{t(s.label)}</span></td>
                  <td>{typeLabel(m.target_type)}: {m.target_name || m.target_id}</td>
                  <td className="muted small">{new Date(m.starts_at).toLocaleString()} – {new Date(m.ends_at).toLocaleString()}</td>
                  <td className="muted small">{m.note || "—"}</td>
                  <td><button className="btn ghost sm" onClick={() => remove.mutate(m.id)}>×</button></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      <form className="inline-form" style={{ marginTop: 10, flexWrap: "wrap" }} onSubmit={submit}>
        <select value={tType} onChange={(e) => { setTType(e.target.value as "client" | "site" | "device"); setTarget(""); }}>
          <option value="device">{t("Gerät")}</option>
          <option value="site">{t("Standort")}</option>
          <option value="client">{t("Client")}</option>
        </select>
        <select value={target} onChange={(e) => setTarget(e.target.value)}>
          <option value="">{t("— Ziel wählen —")}</option>
          {targets.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
        <label className="num">{t("Von")}<input type="datetime-local" value={start} onChange={(e) => setStart(e.target.value)} /></label>
        <label className="num">{t("Bis")}<input type="datetime-local" value={end} onChange={(e) => setEnd(e.target.value)} /></label>
        <input placeholder={t("Notiz (optional)")} value={note} onChange={(e) => setNote(e.target.value)} />
        <button className="btn primary" type="submit" disabled={create.isPending}>+ {t("Fenster")}</button>
      </form>
      {msg && <p className="form-err">{msg}</p>}
    </section>
  );
}

function TwoFactor() {
  const { t } = useI18n();
  const { user, reload } = useAuth();
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const [msg, setMsg] = useState("");
  const regen = useMutation({
    mutationFn: () => api.post<{ recovery_codes: string[] }>("/auth/2fa/recovery", { code: code.trim() }),
    onSuccess: (d) => { setCodes(d.recovery_codes); setCode(""); setMsg(""); },
    onError: (e) => setMsg(e instanceof ApiError ? e.message : t("Fehler")),
  });
  const disable = useMutation({
    mutationFn: () => api.post("/auth/2fa/disable", { code: code.trim() }),
    onSuccess: () => reload(),
    onError: (e) => setMsg(e instanceof ApiError ? e.message : t("Fehler")),
  });

  return (
    <section className="card">
      <h2>{t("Zwei-Faktor-Authentifizierung")}</h2>
      <p className="muted">
        Status: {user?.totp_enabled ? "aktiv ✓" : "nicht eingerichtet"}
        {user?.require_2fa ? " · Pflicht" : ""}
      </p>
      {user?.totp_enabled && (
        <>
          {codes ? (
            <>
              <p className="muted small">{t("Neue Wiederherstellungscodes (alte sind ungültig) – jetzt sichern:")}</p>
              <pre className="help-code" style={{ columns: 2 }}>{codes.join("\n")}</pre>
            </>
          ) : (
            <div className="inline-form">
              <input placeholder={t("Aktueller Code")} value={code} onChange={(e) => setCode(e.target.value)} inputMode="numeric" />
              <button className="btn" onClick={() => regen.mutate()} disabled={!code.trim() || regen.isPending}>{t("Neue Backup-Codes")}</button>
              <button className="btn ghost" onClick={() => disable.mutate()} disabled={!code.trim() || disable.isPending}>{t("2FA deaktivieren")}</button>
              {msg && <span className="muted small">{msg}</span>}
            </div>
          )}
        </>
      )}
    </section>
  );
}

const FIELD_TYPES: Record<CustomFieldType, string> = {
  text: "Text", number: "Zahl", checkbox: "Checkbox", select: "Einfachauswahl",
  multiselect: "Mehrfachauswahl", datetime: "Datum/Zeit", list: "Liste",
};
const FIELD_MODELS: Record<string, string> = { device: "Gerät", client: "Client", site: "Standort" };

const REPORT_FREQ: Record<string, string> = { daily: "Täglich", weekly: "Wöchentlich", monthly: "Monatlich" };

// Reports: On-demand-Health-Bericht ansehen + geplante Berichte per E-Mail.
function Reports() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { data: schedules } = useQuery({ queryKey: ["report-schedules"], queryFn: () => api.get<ReportSchedule[]>("/report-schedules") });
  const { data: alerts } = useQuery({ queryKey: ["alerts"], queryFn: () => api.get<AlertsResponse>("/settings/alerts") });
  const invalidate = () => qc.invalidateQueries({ queryKey: ["report-schedules"] });

  const [title, setTitle] = useState("Health-Bericht");
  const [freq, setFreq] = useState("weekly");
  const [channel, setChannel] = useState("");
  const [msg, setMsg] = useState("");

  const create = useMutation({
    mutationFn: () => api.post("/report-schedules", { title, frequency: freq, channel_id: channel }),
    onSuccess: () => { invalidate(); setMsg(""); },
    onError: (e) => setMsg(e instanceof ApiError ? e.message : t("Fehler")),
  });
  const remove = useMutation({ mutationFn: (id: string) => api.del(`/report-schedules/${id}`), onSuccess: invalidate });

  const channels = alerts?.channels ?? [];

  return (
    <section className="card">
      <h2>{t("Berichte")}</h2>
      <p className="muted small">{t("Health-Bericht (Geräte online/offline, fehlerhafte Checks/Tasks, ausstehende Patches je Kunde). Jetzt ansehen oder regelmäßig per Kanal (i.d.R. E-Mail) versenden.")}</p>
      <div className="inline-form" style={{ marginBottom: 12 }}>
        <a className="btn ghost sm" href="/api/v1/reports/health" target="_blank" rel="noreferrer">{t("Bericht ansehen")} ↗</a>
        <span className="muted small">{t("(im Browser druckbar zu PDF)")}</span>
      </div>

      {(schedules ?? []).length === 0 ? (
        <p className="muted">{t("Keine geplanten Berichte.")}</p>
      ) : (
        <table className="table">
          <thead><tr><th>{t("Titel")}</th><th>{t("Häufigkeit")}</th><th>{t("Kanal")}</th><th>{t("Zuletzt")}</th><th></th></tr></thead>
          <tbody>
            {schedules!.map((s) => (
              <tr key={s.id}>
                <td>{s.title}</td>
                <td>{t(REPORT_FREQ[s.frequency] ?? s.frequency)}</td>
                <td className="muted">{s.channel_name || "—"}</td>
                <td className="muted small">{s.last_run ? new Date(s.last_run).toLocaleString() : "noch nicht"}</td>
                <td><button className="btn ghost sm" onClick={() => remove.mutate(s.id)}>×</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <form className="inline-form" style={{ marginTop: 10, flexWrap: "wrap" }}
        onSubmit={(e) => { e.preventDefault(); if (!channel) { setMsg(t("Bitte einen Kanal wählen.")); return; } create.mutate(); }}>
        <input placeholder={t("Titel")} value={title} onChange={(e) => setTitle(e.target.value)} />
        <select value={freq} onChange={(e) => setFreq(e.target.value)}>
          {Object.entries(REPORT_FREQ).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <select value={channel} onChange={(e) => setChannel(e.target.value)}>
          <option value="">{t("— Kanal wählen —")}</option>
          {channels.map((c) => <option key={c.id} value={c.id}>{c.name} ({c.type})</option>)}
        </select>
        <button className="btn primary" type="submit" disabled={create.isPending}>+ {t("Planen")}</button>
      </form>
      {channels.length === 0 && <p className="muted small">{t("Zuerst oben einen Alarm-Kanal (z. B. E-Mail) anlegen.")}</p>}
      {msg && <p className="form-err">{msg}</p>}
    </section>
  );
}

function CustomFields() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["custom-fields"], queryFn: () => api.get<CustomField[]>("/custom-fields") });
  const [editId, setEditId] = useState<string | null>(null);
  const [model, setModel] = useState<"device" | "client" | "site">("device");
  const [name, setName] = useState("");
  const [type, setType] = useState<CustomFieldType>("text");
  const [options, setOptions] = useState("");
  const [def, setDef] = useState("");
  const [required, setRequired] = useState(false);
  const inval = () => qc.invalidateQueries({ queryKey: ["custom-fields"] });
  const reset = () => { setEditId(null); setName(""); setType("text"); setOptions(""); setDef(""); setRequired(false); };

  const save = useMutation({
    mutationFn: () => {
      const body = { model, name, type, options: options.split(",").map((o) => o.trim()).filter(Boolean), default_value: def, required };
      return editId ? api.put(`/custom-fields/${editId}`, body) : api.post("/custom-fields", body);
    },
    onSuccess: () => { inval(); reset(); },
  });
  const remove = useMutation({ mutationFn: (id: string) => api.del(`/custom-fields/${id}`), onSuccess: inval });

  const startEdit = (f: CustomField) => {
    setEditId(f.id); setModel(f.model); setName(f.name); setType(f.type);
    setOptions(f.options.join(", ")); setDef(f.default_value); setRequired(f.required);
  };
  const needsOptions = type === "select" || type === "multiselect";

  return (
    <section className="card">
      <h2>{t("Benutzerdefinierte Felder")}</h2>
      <p className="muted">{t("Eigene Felder für Geräte, Clients und Standorte – manuell oder per Collector-Task (JSON) befüllbar.")}</p>

      <table className="table">
        <thead><tr><th>{t("Ebene")}</th><th>{t("Name")}</th><th>{t("Typ")}</th><th>{t("Optionen")}</th><th>{t("Pflicht")}</th><th></th></tr></thead>
        <tbody>
          {(data ?? []).map((f) => (
            <tr key={f.id}>
              <td className="muted">{t(FIELD_MODELS[f.model] ?? f.model)}</td>
              <td className="link-strong">{f.name}</td>
              <td>{t(FIELD_TYPES[f.type] ?? f.type)}</td>
              <td className="muted small">{f.options.join(", ") || "—"}</td>
              <td>{f.required ? "ja" : "—"}</td>
              <td>
                <button className="btn ghost sm" onClick={() => startEdit(f)}>Bearbeiten</button>
                <button className="btn ghost sm" onClick={() => remove.mutate(f.id)}>Löschen</button>
              </td>
            </tr>
          ))}
          {(data ?? []).length === 0 && <tr><td colSpan={6} className="empty">{t("Noch keine Felder.")}</td></tr>}
        </tbody>
      </table>

      <form className="inline-form" style={{ marginTop: 10, flexWrap: "wrap" }} onSubmit={(e) => { e.preventDefault(); if (name) save.mutate(); }}>
        <select value={model} onChange={(e) => setModel(e.target.value as "device" | "client" | "site")} disabled={!!editId}>
          {Object.entries(FIELD_MODELS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        <input placeholder={t("Feldname")} value={name} onChange={(e) => setName(e.target.value)} />
        <select value={type} onChange={(e) => setType(e.target.value as CustomFieldType)}>
          {Object.entries(FIELD_TYPES).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
        {needsOptions && <input placeholder={t("Optionen (kommagetrennt)")} value={options} onChange={(e) => setOptions(e.target.value)} />}
        <input placeholder={t("Standardwert")} value={def} onChange={(e) => setDef(e.target.value)} />
        <label className="chip"><input type="checkbox" checked={required} onChange={(e) => setRequired(e.target.checked)} /> {t("Pflicht")}</label>
        <button className="btn primary" type="submit" disabled={save.isPending}>{editId ? "Speichern" : "+ Feld"}</button>
        {editId && <button type="button" className="btn ghost" onClick={reset}>Abbrechen</button>}
      </form>
    </section>
  );
}

function Alerts() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { data: alerts } = useQuery({ queryKey: ["alerts"], queryFn: () => api.get<AlertsResponse>("/settings/alerts") });
  const { data: providers } = useQuery({ queryKey: ["alert-providers"], queryFn: () => api.get<AlertProvider[]>("/settings/alert-providers") });
  const [editing, setEditing] = useState<AlertChannel | "new" | null>(null);
  const [testState, setTestState] = useState<Record<string, string>>({});
  const inval = () => qc.invalidateQueries({ queryKey: ["alerts"] });

  const setEnabled = useMutation({ mutationFn: (en: boolean) => api.put("/settings/alerts", { enabled: en, alert_software: alerts?.alert_software ?? false }), onSuccess: inval });
  const setSoftware = useMutation({ mutationFn: (on: boolean) => api.put("/settings/alerts", { enabled: alerts?.enabled ?? false, alert_software: on }), onSuccess: inval });
  const toggleCh = useMutation({
    mutationFn: (ch: AlertChannel) => api.put(`/settings/alert-channels/${ch.id}`, { name: ch.name, enabled: !ch.enabled, config: ch.config }),
    onSuccess: inval,
  });
  const removeCh = useMutation({ mutationFn: (id: string) => api.del(`/settings/alert-channels/${id}`), onSuccess: inval });
  const testCh = useMutation({
    mutationFn: (id: string) => api.post(`/settings/alert-channels/${id}/test`),
    onMutate: (id) => setTestState((s) => ({ ...s, [id]: "…" })),
    onSuccess: (_d, id) => setTestState((s) => ({ ...s, [id]: "✓ gesendet" })),
    onError: (e: unknown, id) => setTestState((s) => ({ ...s, [id]: "✗ " + (e instanceof ApiError ? e.message : t("Fehler")) })),
  });

  const channels = alerts?.channels ?? [];
  const provLabel = (ty: string) => providers?.find((p) => p.type === ty)?.label ?? ty;

  return (
    <section className="card">
      <h2>{t("Alerting bei Check-Fehlern")}</h2>
      <p className="muted">{t("Benachrichtigung über beliebige Kanäle, wenn ein Check neu fehlschlägt.")}</p>
      <label className="chip" style={{ width: "fit-content", marginBottom: 8 }}>
        <input type="checkbox" checked={alerts?.enabled ?? false} onChange={(e) => setEnabled.mutate(e.target.checked)} /> {t("Alerting aktiviert")}
      </label>
      <label className="chip" style={{ width: "fit-content", marginBottom: 12 }}>
        <input type="checkbox" checked={alerts?.alert_software ?? false} disabled={!alerts?.enabled} onChange={(e) => setSoftware.mutate(e.target.checked)} /> {t("Auch bei Software-Änderungen benachrichtigen")}
      </label>

      <table className="table">
        <thead><tr><th>{t("Name")}</th><th>{t("Typ")}</th><th>{t("Aktiv")}</th><th>{t("Test")}</th><th></th></tr></thead>
        <tbody>
          {channels.map((ch) => (
            <tr key={ch.id}>
              <td>{ch.name || "—"}</td>
              <td className="muted">{provLabel(ch.type)}</td>
              <td>
                <label className="chip"><input type="checkbox" checked={ch.enabled} onChange={() => toggleCh.mutate(ch)} /></label>
              </td>
              <td>
                <button className="btn ghost sm" onClick={() => testCh.mutate(ch.id)} disabled={testCh.isPending}>Test</button>
                {testState[ch.id] && <span className="muted small" style={{ marginLeft: 6 }}>{testState[ch.id]}</span>}
              </td>
              <td>
                <button className="btn ghost sm" onClick={() => setEditing(ch)}>Bearbeiten</button>
                <button className="btn ghost sm" onClick={() => removeCh.mutate(ch.id)}>Löschen</button>
              </td>
            </tr>
          ))}
          {channels.length === 0 && <tr><td colSpan={5} className="empty">{t("Noch keine Kanäle.")}</td></tr>}
        </tbody>
      </table>

      <div style={{ marginTop: 10 }}>
        <button className="btn primary" onClick={() => setEditing("new")}>{t("Kanal hinzufügen")}</button>
      </div>

      {editing && providers && (
        <ChannelForm
          providers={providers}
          channel={editing === "new" ? null : editing}
          onClose={() => setEditing(null)}
          onSaved={inval}
        />
      )}
    </section>
  );
}

function ChannelForm({ providers, channel, onClose, onSaved }: {
  providers: AlertProvider[];
  channel: AlertChannel | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useI18n();
  const isEdit = !!channel;
  const [type, setType] = useState(channel?.type ?? providers[0]?.type ?? "");
  const [name, setName] = useState(channel?.name ?? "");
  const [config, setConfig] = useState<Record<string, string>>(channel?.config ?? {});
  const [minSeverity, setMinSeverity] = useState<"warning" | "critical">(channel?.min_severity ?? "warning");
  const [assignments, setAssignments] = useState<ChannelScope[]>(channel?.assignments ?? []);
  const [aType, setAType] = useState<"client" | "site" | "device">("client");
  const [aTarget, setATarget] = useState("");
  const prov = providers.find((p) => p.type === type);
  const set = (k: string, v: string) => setConfig((c) => ({ ...c, [k]: v }));

  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: devices } = useQuery({ queryKey: ["devices"], queryFn: () => api.get<Device[]>("/devices") });
  const targets =
    aType === "client" ? (tree?.clients ?? []).map((c) => ({ id: c.id, name: c.name }))
    : aType === "site" ? (tree?.clients ?? []).flatMap((c) => (c.sites ?? []).map((s) => ({ id: s.id, name: `${c.name} › ${s.name}` })))
    : (devices ?? []).map((d) => ({ id: d.id, name: d.hostname }));
  const targetName = (t: ChannelScope) =>
    (t.target_type === "client" ? (tree?.clients ?? []).find((c) => c.id === t.target_id)?.name
      : t.target_type === "site" ? (tree?.clients ?? []).flatMap((c) => c.sites ?? []).find((s) => s.id === t.target_id)?.name
      : (devices ?? []).find((d) => d.id === t.target_id)?.hostname) ?? t.target_id;
  const addScope = () => {
    if (!aTarget || assignments.some((a) => a.target_type === aType && a.target_id === aTarget)) return;
    setAssignments((a) => [...a, { target_type: aType, target_id: aTarget }]);
    setATarget("");
  };

  const save = useMutation({
    mutationFn: () => {
      const body = { name, config, min_severity: minSeverity, assignments };
      return isEdit
        ? api.put(`/settings/alert-channels/${channel!.id}`, { ...body, enabled: channel!.enabled })
        : api.post("/settings/alert-channels", { ...body, type, enabled: true });
    },
    onSuccess: () => { onSaved(); onClose(); },
  });

  return (
    <div className="card" style={{ marginTop: 12, background: "var(--surface-2, #f7f8fa)" }}>
      <h3>{isEdit ? t("Kanal bearbeiten") : t("Neuer Kanal")}</h3>
      <div className="inline-form">
        <label className="num">Typ
          <select value={type} onChange={(e) => { setType(e.target.value); setConfig({}); }} disabled={isEdit}>
            {providers.map((p) => <option key={p.type} value={p.type}>{p.label}</option>)}
          </select>
        </label>
        <input placeholder={t("Name (frei wählbar)")} value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 8, marginTop: 8, maxWidth: 520 }}>
        {prov?.fields.map((f) => {
          const val = config[f.key] ?? "";
          if (f.type === "checkbox") {
            return (
              <label key={f.key} className="chip" style={{ width: "fit-content" }}>
                <input type="checkbox" checked={val === "true"} onChange={(e) => set(f.key, e.target.checked ? "true" : "false")} /> {f.label}
              </label>
            );
          }
          return (
            <label key={f.key} className="field">
              <span className="muted small">{f.label}{f.required ? " *" : ""}</span>
              <input
                type={f.type === "password" ? "password" : f.type === "number" ? "number" : "text"}
                value={val}
                placeholder={f.type === "password" && isEdit ? "leer = unverändert" : f.help ?? ""}
                onChange={(e) => set(f.key, e.target.value)}
              />
              {f.help && <span className="muted small">{f.help}</span>}
            </label>
          );
        })}
      </div>

      <h4 style={{ marginBottom: 4 }}>{t("Schweregrad")}</h4>
      <select value={minSeverity} onChange={(e) => setMinSeverity(e.target.value as "warning" | "critical")}>
        <option value="warning">{t("Bei jedem Fehler benachrichtigen")}</option>
        <option value="critical">{t("Nur bei kritischen Checks")}</option>
      </select>

      <h4 style={{ marginBottom: 4, marginTop: 14 }}>{t("Geltungsbereich")}</h4>
      <p className="muted small" style={{ marginTop: 0 }}>
        {assignments.length === 0 ? "Global – gilt für alle Geräte." : "Gilt nur für die folgenden Ziele:"}
      </p>
      {assignments.map((a, i) => (
        <div key={i} className="list-row">
          <span className="muted small">{a.target_type === "client" ? "Client" : a.target_type === "site" ? "Standort" : "Gerät"}: {targetName(a)}</span>
          <button className="btn ghost sm" onClick={() => setAssignments((arr) => arr.filter((_, j) => j !== i))}>×</button>
        </div>
      ))}
      <div className="inline-form" style={{ marginTop: 6 }}>
        <select value={aType} onChange={(e) => { setAType(e.target.value as "client" | "site" | "device"); setATarget(""); }}>
          <option value="client">{t("Client")}</option>
          <option value="site">{t("Standort")}</option>
          <option value="device">{t("Gerät")}</option>
        </select>
        <select value={aTarget} onChange={(e) => setATarget(e.target.value)}>
          <option value="">{t("— wählen —")}</option>
          {targets.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>
        <button className="btn" onClick={addScope} disabled={!aTarget}>+ {t("Ziel")}</button>
      </div>

      <div style={{ marginTop: 14 }}>
        <button className="btn primary" onClick={() => save.mutate()} disabled={save.isPending}>Speichern</button>
        <button className="btn ghost" onClick={onClose}>Abbrechen</button>
        {save.isError && <span className="muted small" style={{ marginLeft: 10 }}>{save.error instanceof ApiError ? save.error.message : "Fehler"}</span>}
      </div>
    </div>
  );
}

function Tokens() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [label, setLabel] = useState("");
  const [maxUses, setMaxUses] = useState(0);
  const [expiresInH, setExpiresInH] = useState(24);
  const [created, setCreated] = useState<EnrollmentToken | null>(null);

  const { data } = useQuery({ queryKey: ["tokens"], queryFn: () => api.get<EnrollmentToken[]>("/enrollment-tokens") });
  const create = useMutation({
    mutationFn: () =>
      api.post<EnrollmentToken>("/enrollment-tokens", { label, max_uses: maxUses, expires_in_hours: expiresInH }),
    onSuccess: (t) => {
      setCreated(t);
      setLabel("");
      qc.invalidateQueries({ queryKey: ["tokens"] });
    },
  });
  const remove = useMutation({
    mutationFn: (id: string) => api.del(`/enrollment-tokens/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["tokens"] }),
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate();
  };

  return (
    <section className="card">
      <h2>{t("Enrollment-Tokens")}</h2>
      <p className="muted">{t("Werden per GPO an die Agents verteilt und beim ersten Start gegen ein Agent-Token getauscht.")}</p>

      <form className="inline-form" onSubmit={submit}>
        <input placeholder={t("Bezeichnung")} value={label} onChange={(e) => setLabel(e.target.value)} />
        <label className="num">{t("Max. Nutzungen")}<input type="number" min={0} value={maxUses} onChange={(e) => setMaxUses(+e.target.value)} /></label>
        <label className="num">Gültig (Std., 0=∞)<input type="number" min={0} value={expiresInH} onChange={(e) => setExpiresInH(+e.target.value)} /></label>
        <button className="btn primary" type="submit" disabled={create.isPending}>{t("Erzeugen")}</button>
      </form>

      {created?.token && (
        <div className="token-reveal">
          <strong>{t("Token (nur jetzt sichtbar):")}</strong>
          <code className="mono">{created.token}</code>
          <button className="btn ghost sm" onClick={() => navigator.clipboard.writeText(created.token!)}>{t("Kopieren")}</button>
        </div>
      )}

      <table className="table">
        <thead><tr><th>{t("Bezeichnung")}</th><th>{t("Nutzungen")}</th><th>{t("Läuft ab")}</th><th>{t("Erstellt von")}</th><th></th></tr></thead>
        <tbody>
          {(data ?? []).map((tok) => (
            <tr key={tok.id}>
              <td>{tok.label || "—"}</td>
              <td>{tok.used_count}{tok.max_uses ? ` / ${tok.max_uses}` : ""}</td>
              <td className="muted">{tok.expires_at ? new Date(tok.expires_at).toLocaleString() : t("nie")}</td>
              <td className="muted">{tok.created_by}</td>
              <td><button className="btn ghost sm" onClick={() => remove.mutate(tok.id)}>{t("Widerrufen")}</button></td>
            </tr>
          ))}
          {(data ?? []).length === 0 && <tr><td colSpan={5} className="empty">{t("Keine Tokens.")}</td></tr>}
        </tbody>
      </table>
    </section>
  );
}

function Users() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [roleSel, setRoleSel] = useState("builtin:viewer"); // "builtin:<role>" | "custom:<id>"

  const { data } = useQuery({ queryKey: ["users"], queryFn: () => api.get<User[]>("/users") });
  const { data: rolesData } = useQuery({ queryKey: ["roles"], queryFn: () => api.get<{ roles: CustomRole[] }>("/roles") });
  const customRoles = rolesData?.roles ?? [];
  const invalidate = () => qc.invalidateQueries({ queryKey: ["users"] });

  // decode/encode der kombinierten Auswahl -> API-Felder {role, custom_role_id}.
  const encode = (u: User) => u.custom_role_id ? `custom:${u.custom_role_id}` : `builtin:${u.role}`;
  const toFields = (sel: string) => sel.startsWith("custom:")
    ? { role: "viewer", custom_role_id: sel.slice(7) }
    : { role: sel.slice(8), custom_role_id: "" };

  const create = useMutation({
    mutationFn: () => api.post<User>("/users", { username, email, password, ...toFields(roleSel) }),
    onSuccess: () => { setUsername(""); setEmail(""); setPassword(""); invalidate(); },
  });
  const setUserRole = useMutation({
    mutationFn: ({ id, sel }: { id: string; sel: string }) => api.put(`/users/${id}`, toFields(sel)),
    onSuccess: invalidate,
  });
  const reset2fa = useMutation({
    mutationFn: (id: string) => api.post(`/users/${id}/reset-2fa`),
    onSuccess: invalidate,
  });

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (username && password) create.mutate();
  };

  const roleOptions = (
    <>
      <option value="builtin:viewer">{t("Viewer (nur lesen)")}</option>
      <option value="builtin:technician">{t("Techniker (bedienen)")}</option>
      <option value="builtin:admin">{t("Admin (alles)")}</option>
      {customRoles.length > 0 && <optgroup label={t("Eigene Rollen")}>
        {customRoles.map((r) => <option key={r.id} value={`custom:${r.id}`}>{r.name}</option>)}
      </optgroup>}
    </>
  );

  return (
    <section className="card">
      <h2>{t("Benutzer")}</h2>
      <form className="inline-form" onSubmit={submit}>
        <input placeholder={t("Benutzername")} value={username} onChange={(e) => setUsername(e.target.value)} />
        <input placeholder={t("E-Mail")} value={email} onChange={(e) => setEmail(e.target.value)} />
        <input type="password" placeholder={t("Passwort")} value={password} onChange={(e) => setPassword(e.target.value)} />
        <select value={roleSel} onChange={(e) => setRoleSel(e.target.value)}>{roleOptions}</select>
        <button className="btn primary" type="submit" disabled={create.isPending}>{t("Anlegen")}</button>
      </form>

      <table className="table">
        <thead><tr><th>{t("Benutzer")}</th><th>{t("E-Mail")}</th><th>{t("Rolle")}</th><th>2FA</th><th>{t("Quelle")}</th><th>{t("Letzte Anmeldung")}</th><th></th></tr></thead>
        <tbody>
          {(data ?? []).map((u) => (
            <tr key={u.id}>
              <td>{u.username}</td>
              <td className="muted">{u.email || "—"}</td>
              <td>
                {u.auth_source === "local"
                  ? <select value={encode(u)} onChange={(e) => setUserRole.mutate({ id: u.id, sel: e.target.value })}>{roleOptions}</select>
                  : u.role}
              </td>
              <td>{u.totp_enabled ? <span className="badge badge-online">{t("aktiv")}</span> : <span className="muted small">—</span>}</td>
              <td className="muted">{u.auth_source}</td>
              <td className="muted">{u.last_login ? new Date(u.last_login).toLocaleString() : "—"}</td>
              <td>
                {u.totp_enabled && (
                  <button className="btn ghost sm" disabled={reset2fa.isPending}
                    onClick={() => confirm(t("2FA für „{name}“ zurücksetzen? Der Nutzer muss es beim nächsten Login neu einrichten.", { name: u.username })) && reset2fa.mutate(u.id)}>
                    {t("2FA zurücksetzen")}
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
