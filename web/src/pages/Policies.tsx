import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n, gt } from "../i18n";
import type { Policy, PolicyTask, Script, ClientTree, Device } from "../types";

const WD = ["So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"];
function weekdayLabel(s: string): string {
  return s.split(",").map((x) => gt(WD[+x.trim()] ?? x)).join(",");
}

// taskScheduleLabel beschreibt den Zeitplan eines Tasks (Häufigkeit oder Legacy).
function taskScheduleLabel(t: PolicyTask): string {
  if (t.frequency) {
    let s = gt(FREQ_LABEL[t.frequency] ?? t.frequency);
    if (t.frequency === "daily" || t.frequency === "weekly") {
      if (t.daily_time) s += ` ${t.daily_time}`;
      if (t.frequency === "weekly" && t.weekdays) s += ` (${weekdayLabel(t.weekdays)})`;
    }
    return s;
  }
  return t.schedule_type === "daily"
    ? gt("täglich {time}", { time: t.daily_time }) + (t.weekdays ? ` (${weekdayLabel(t.weekdays)})` : "")
    : gt("alle {n} Min.", { n: t.interval_minutes });
}

const CHECK_TYPES: Record<string, string> = {
  disk: "Speicherplatz (frei % <)",
  memory: "RAM-Auslastung (% >)",
  cpu: "CPU-Last (% >)",
  updates: "Ausstehende Updates (> N)",
  script: "Skript-Check (Exit-Code)",
  ping: "Ping (Erreichbarkeit)",
  tcp: "TCP-Port erreichbar",
  http: "HTTP-Status",
  ports: "Offene Ports (Whitelist)",
};
const isNetCheck = (t: string) => t === "ping" || t === "tcp" || t === "http";

// Häufigkeits-Presets für Checks und Tasks.
const FREQ: [string, string][] = [
  ["1m", "1 Minute"], ["5m", "5 Minuten"], ["15m", "15 Minuten"], ["30m", "30 Minuten"],
  ["1h", "1 Stunde"], ["2h", "2 Stunden"], ["6h", "6 Stunden"], ["12h", "12 Stunden"],
  ["daily", "Täglich"], ["weekly", "Wöchentlich"], ["monthly", "Monatlich"], ["yearly", "Jährlich"],
];
const FREQ_LABEL: Record<string, string> = Object.fromEntries(FREQ);
const isCalendar = (f: string) => f === "daily" || f === "weekly" || f === "monthly" || f === "yearly";

export function Policies() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const invalidate = () => qc.invalidateQueries({ queryKey: ["policies"] });

  const { data: policies } = useQuery({ queryKey: ["policies"], queryFn: () => api.get<Policy[]>("/policies") });
  const { data: scripts } = useQuery({ queryKey: ["scripts"], queryFn: () => api.get<Script[]>("/scripts") });
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: devices } = useQuery({ queryKey: ["devices"], queryFn: () => api.get<Device[]>("/devices") });

  const [selected, setSelected] = useState<string | null>(null);
  const policy = policies?.find((p) => p.id === selected) ?? null;

  const createPolicy = useMutation({
    mutationFn: (name: string) => api.post<Policy>("/policies", { name, description: "" }),
    onSuccess: (p) => { invalidate(); setSelected(p.id); },
  });

  // Editor-Ansicht für die gewählte Richtlinie.
  if (policy) {
    return (
      <div className="page wide">
        <header className="page-head">
          <div className="inline-form">
            <button className="btn ghost sm" onClick={() => setSelected(null)}>← {t("Zurück")}</button>
            <h1 style={{ margin: 0 }}>{policy.name}</h1>
          </div>
        </header>
        <PolicyEditor
          policy={policy}
          scripts={scripts ?? []}
          tree={tree}
          devices={devices ?? []}
          onChange={invalidate}
          onDeleted={() => { invalidate(); setSelected(null); }}
        />
      </div>
    );
  }

  // Listenansicht.
  return (
    <div className="page wide">
      <header className="page-head">
        <div>
          <h1>{t("Richtlinien")}</h1>
          <p className="muted">{t("Profile mit Checks und Tasks – zuweisbar an Client, Standort oder Gerät (mit Vererbung).")}</p>
        </div>
        <button className="btn primary" onClick={() => { const n = prompt(t("Name der Richtlinie?")); if (n) createPolicy.mutate(n); }}>+ {t("Neu anlegen")}</button>
      </header>

      <section className="card">
        <table className="table">
          <thead><tr><th>{t("Name")}</th><th>{t("Checks")}</th><th>{t("Tasks")}</th><th>{t("Zuweisungen")}</th><th></th></tr></thead>
          <tbody>
            {(policies ?? []).map((p) => (
              <tr key={p.id}>
                <td className="link-strong">{p.name}</td>
                <td>{p.checks?.length ?? 0}</td>
                <td>{p.tasks?.length ?? 0}</td>
                <td className="muted">{p.assignments?.length ?? 0}</td>
                <td><button className="btn ghost sm" onClick={() => setSelected(p.id)}>{t("Bearbeiten")}</button></td>
              </tr>
            ))}
            {(policies ?? []).length === 0 && <tr><td colSpan={5} className="empty">{t("Noch keine Richtlinien.")}</td></tr>}
          </tbody>
        </table>
      </section>
    </div>
  );
}


function PolicyEditor({
  policy, scripts, tree, devices, onChange, onDeleted,
}: {
  policy: Policy; scripts: Script[]; tree?: ClientTree; devices: Device[];
  onChange: () => void; onDeleted: () => void;
}) {
  const { t } = useI18n();
  const delPolicy = useMutation({ mutationFn: () => api.del(`/policies/${policy.id}`), onSuccess: onDeleted });

  // Check anlegen
  const [cName, setCName] = useState("");
  const [cType, setCType] = useState("disk");
  const [cThreshold, setCThreshold] = useState(15);
  const [cScript, setCScript] = useState("");
  const [cSeverity, setCSeverity] = useState<"warning" | "critical">("critical");
  const [cFreq, setCFreq] = useState("");
  const [cOp, setCOp] = useState("");      // Ausgabe-Vergleich (nur Skript-Check)
  const [cWarn, setCWarn] = useState("");
  const [cCrit, setCCrit] = useState("");
  const [cHost, setCHost] = useState("");   // Netzwerk-Checks: Host
  const [cPort, setCPort] = useState("");   // TCP-Check: Port
  const [cUrl, setCUrl] = useState("");     // HTTP-Check: URL
  const [cExpected, setCExpected] = useState(""); // HTTP: erwarteter Status
  const [cRemediation, setCRemediation] = useState(""); // Self-Healing: Skript bei Fehler
  const [cAllowed, setCAllowed] = useState(""); // Ports-Check: erlaubte Ports (Whitelist)
  const addCheck = useMutation({
    mutationFn: () => {
      let config: Record<string, number | string> = { threshold: cThreshold };
      if (cType === "script") {
        config = {};
        if (cOp) {
          config.operator = cOp;
          if (cWarn !== "") config.warn = Number(cWarn);
          if (cCrit !== "") config.crit = Number(cCrit);
        }
      } else if (cType === "ports") {
        config = { allowed: cAllowed.trim() };
      } else if (isNetCheck(cType)) {
        config = {};
        if (cType === "http") {
          config.url = cUrl.trim();
          if (cExpected !== "") config.expected_status = Number(cExpected);
        } else {
          config.host = cHost.trim();
          if (cType === "tcp") config.port = Number(cPort);
        }
        // optionale Latenz-Schwellen (ms): warn -> Warnung, crit -> Fehler
        if (cWarn !== "") config.warn = Number(cWarn);
        if (cCrit !== "") config.crit = Number(cCrit);
      }
      // Bei aktivem Ausgabe-Vergleich definieren die Schwellen den Schweregrad
      // (Fehler-Schwelle = kritisch), daher fix "critical".
      const severity = cType === "script" && cOp ? "critical" : cSeverity;
      return api.post(`/policies/${policy.id}/checks`, {
        name: cName, type: cType, severity, frequency: cFreq, config,
        script_id: cType === "script" ? cScript || null : null,
        remediation_script_id: cRemediation || null,
      });
    },
    onSuccess: () => { onChange(); setCName(""); setCHost(""); setCPort(""); setCUrl(""); setCExpected(""); setCRemediation(""); setCAllowed(""); },
  });
  const delCheck = useMutation({ mutationFn: (id: string) => api.del(`/checks/${id}`), onSuccess: onChange });

  // Task anlegen
  const [tName, setTName] = useState("");
  const [tScript, setTScript] = useState("");
  const [tFreq, setTFreq] = useState("1h");
  const [tDaily, setTDaily] = useState("02:00");
  const [tWeekdays, setTWeekdays] = useState("");
  const [tCollect, setTCollect] = useState(false);
  const addTask = useMutation({
    mutationFn: () =>
      api.post(`/policies/${policy.id}/tasks`, {
        name: tName, script_id: tScript, frequency: tFreq,
        daily_time: tDaily, weekdays: tWeekdays, schedule_type: "", interval_minutes: 0,
        collect_fields: tCollect,
      }),
    onSuccess: () => { onChange(); setTName(""); setTCollect(false); },
  });
  const delTask = useMutation({ mutationFn: (id: string) => api.del(`/tasks/${id}`), onSuccess: onChange });

  // Zuweisung
  const [aType, setAType] = useState<"client" | "site" | "device">("client");
  const [aTarget, setATarget] = useState("");
  const addAssign = useMutation({
    mutationFn: () => api.post(`/policies/${policy.id}/assignments`, { target_type: aType, target_id: aTarget }),
    onSuccess: () => { onChange(); setATarget(""); },
  });
  const delAssign = useMutation({ mutationFn: (id: string) => api.del(`/assignments/${id}`), onSuccess: onChange });

  const scriptName = (sid?: string) => scripts.find((s) => s.id === sid)?.name ?? "—";
  const targets =
    aType === "client" ? (tree?.clients ?? []).map((c) => ({ id: c.id, name: c.name }))
    : aType === "site" ? (tree?.clients ?? []).flatMap((c) => (c.sites ?? []).map((s) => ({ id: s.id, name: `${c.name} › ${s.name}` })))
    : devices.map((d) => ({ id: d.id, name: d.hostname }));
  const targetName = (t: { target_type: string; target_id: string }) =>
    (t.target_type === "client" ? (tree?.clients ?? []).find((c) => c.id === t.target_id)?.name
      : t.target_type === "site" ? (tree?.clients ?? []).flatMap((c) => c.sites ?? []).find((s) => s.id === t.target_id)?.name
      : devices.find((d) => d.id === t.target_id)?.hostname) ?? t.target_id;

  return (
    <>
      <div className="card">
        <div className="page-head" style={{ marginBottom: 0 }}>
          <h2 style={{ margin: 0 }}>{policy.name}</h2>
          <button className="btn danger sm" onClick={() => confirm(t("Richtlinie löschen?")) && delPolicy.mutate()}>{t("Löschen")}</button>
        </div>
      </div>

      <section className="card">
        <h2>Checks</h2>
        {(policy.checks ?? []).map((c) => (
          <div key={c.id} className="list-row">
            <span className="link-strong">{c.name || t(CHECK_TYPES[c.type])}</span>
            <span className="muted small">
              {c.type === "script" ? `Skript: ${scriptName(c.script_id)}`
                : c.type === "http" ? `HTTP: ${c.config?.url ?? ""}${c.config?.expected_status ? ` (=${c.config.expected_status})` : ""}`
                : c.type === "tcp" ? `TCP: ${c.config?.host ?? ""}:${c.config?.port ?? ""}`
                : c.type === "ping" ? `Ping: ${c.config?.host ?? ""}`
                : c.type === "ports" ? `${t("Erlaubt")}: ${c.config?.allowed ?? "—"}`
                : `${t(CHECK_TYPES[c.type])} ${c.config?.threshold ?? ""}`}
              {" · "}{c.severity === "warning" ? t("Warnung") : t("Kritisch")}
              {" · "}{c.frequency ? t(FREQ_LABEL[c.frequency] ?? c.frequency) : t("jeden Checkin")}
              {c.remediation_script_id && <> {" · "}<span title={t("Bei Fehler automatisch ausführen")}>🔧 {scriptName(c.remediation_script_id)}</span></>}
            </span>
            <button className="btn ghost sm" onClick={() => delCheck.mutate(c.id)}>×</button>
          </div>
        ))}
        <form className="inline-form" style={{ marginTop: 10 }} onSubmit={(e) => { e.preventDefault(); addCheck.mutate(); }}>
          <input placeholder={t("Check-Name")} value={cName} onChange={(e) => setCName(e.target.value)} />
          <select value={cType} onChange={(e) => setCType(e.target.value)}>
            {Object.entries(CHECK_TYPES).map(([k, v]) => <option key={k} value={k}>{t(v)}</option>)}
          </select>
          {cType === "script" ? (
            <>
              <select value={cScript} onChange={(e) => setCScript(e.target.value)}>
                <option value="">{t("— Skript wählen —")}</option>
                {scripts.map((s) => <option key={s.id} value={s.id}>{s.name} ({s.shell})</option>)}
              </select>
              <select value={cOp} onChange={(e) => setCOp(e.target.value)} title={t("Ausgabe-Vergleich (optional)")}>
                <option value="">{t("Nur Exit-Code")}</option>
                {["<", "<=", ">", ">=", "==", "!="].map((o) => <option key={o} value={o}>{t("Ausgabe")} {o}</option>)}
              </select>
              {cOp && (
                <>
                  <label className="num">{t("Warnung")}<input type="number" value={cWarn} onChange={(e) => setCWarn(e.target.value)} /></label>
                  <label className="num">{t("Fehler")}<input type="number" value={cCrit} onChange={(e) => setCCrit(e.target.value)} /></label>
                </>
              )}
            </>
          ) : cType === "ports" ? (
            <input placeholder={t("Erlaubte Ports, z.B. 22,80,443")} value={cAllowed}
              onChange={(e) => setCAllowed(e.target.value)} style={{ minWidth: 200 }}
              title={t("Öffentlich erreichbare Ports, die nicht in dieser Liste stehen, lösen den Check aus.")} />
          ) : isNetCheck(cType) ? (
            <>
              {cType === "http" ? (
                <>
                  <input placeholder={t("URL (z.B. example.com)")} value={cUrl} onChange={(e) => setCUrl(e.target.value)} style={{ minWidth: 180 }} />
                  <label className="num">{t("Status")}<input type="number" placeholder="2xx" value={cExpected} onChange={(e) => setCExpected(e.target.value)} /></label>
                </>
              ) : (
                <>
                  <input placeholder={t("Host / IP")} value={cHost} onChange={(e) => setCHost(e.target.value)} />
                  {cType === "tcp" && <label className="num">{t("Port")}<input type="number" value={cPort} onChange={(e) => setCPort(e.target.value)} /></label>}
                </>
              )}
              <label className="num" title={t("Warnung ab Antwortzeit (ms), optional")}>{t("Warn ms")}<input type="number" value={cWarn} onChange={(e) => setCWarn(e.target.value)} /></label>
              <label className="num" title={t("Fehler ab Antwortzeit (ms), optional")}>{t("Fehler ms")}<input type="number" value={cCrit} onChange={(e) => setCCrit(e.target.value)} /></label>
            </>
          ) : (
            <label className="num">{t("Schwelle")}<input type="number" value={cThreshold} onChange={(e) => setCThreshold(+e.target.value)} /></label>
          )}
          {!(cType === "script" && cOp) && (
            <select value={cSeverity} onChange={(e) => setCSeverity(e.target.value as "warning" | "critical")}>
              <option value="critical">{t("Kritisch")}</option>
              <option value="warning">{t("Warnung")}</option>
            </select>
          )}
          <select value={cFreq} onChange={(e) => setCFreq(e.target.value)} title={t("Häufigkeit")}>
            <option value="">{t("Jeden Checkin")}</option>
            {FREQ.map(([k, v]) => <option key={k} value={k}>{t(v)}</option>)}
          </select>
          <select value={cRemediation} onChange={(e) => setCRemediation(e.target.value)} title={t("Remediation: Skript bei Fehler automatisch ausführen")}>
            <option value="">{t("— keine Remediation —")}</option>
            {scripts.map((s) => <option key={s.id} value={s.id}>🔧 {s.name}</option>)}
          </select>
          <button className="btn primary" type="submit">+ {t("Check")}</button>
        </form>
      </section>

      <section className="card">
        <h2>{t("Tasks (geplante Skripte)")}</h2>
        {(policy.tasks ?? []).map((t) => (
          <div key={t.id} className="list-row">
            <span className="link-strong">{t.name}</span>
            <span className="muted small">
              Skript: {scriptName(t.script_id)} · {taskScheduleLabel(t)}
            </span>
            <button className="btn ghost sm" onClick={() => delTask.mutate(t.id)}>×</button>
          </div>
        ))}
        <form className="inline-form" style={{ marginTop: 10 }} onSubmit={(e) => { e.preventDefault(); if (tScript) addTask.mutate(); }}>
          <input placeholder={t("Task-Name")} value={tName} onChange={(e) => setTName(e.target.value)} />
          <select value={tScript} onChange={(e) => setTScript(e.target.value)}>
            <option value="">{t("— Skript wählen —")}</option>
            {scripts.map((s) => <option key={s.id} value={s.id}>{s.name} ({s.shell})</option>)}
          </select>
          <select value={tFreq} onChange={(e) => setTFreq(e.target.value)} title="Häufigkeit">
            {FREQ.map(([k, v]) => <option key={k} value={k}>{t(v)}</option>)}
          </select>
          {isCalendar(tFreq) && (
            <label className="num">{t("Uhrzeit")}<input type="time" value={tDaily} onChange={(e) => setTDaily(e.target.value)} /></label>
          )}
          {tFreq === "weekly" && (
            <input style={{ width: 130 }} placeholder={t("Wochentage 1,3,5")} value={tWeekdays} onChange={(e) => setTWeekdays(e.target.value)} />
          )}
          <label className="chip" title={t("JSON-Ausgabe in benutzerdefinierte Felder übernehmen")}>
            <input type="checkbox" checked={tCollect} onChange={(e) => setTCollect(e.target.checked)} /> Felder aus JSON
          </label>
          <button className="btn primary" type="submit">+ {t("Task")}</button>
        </form>
      </section>

      <section className="card">
        <h2>{t("Zuweisungen")}</h2>
        {(policy.assignments ?? []).map((a) => (
          <div key={a.id} className="list-row">
            <span className="badge badge-unknown">{a.target_type}</span>
            <span className="link-strong">{targetName(a)}</span>
            <button className="btn ghost sm" onClick={() => delAssign.mutate(a.id)}>×</button>
          </div>
        ))}
        <form className="inline-form" style={{ marginTop: 10 }} onSubmit={(e) => { e.preventDefault(); if (aTarget) addAssign.mutate(); }}>
          <select value={aType} onChange={(e) => { setAType(e.target.value as "client" | "site" | "device"); setATarget(""); }}>
            <option value="client">{t("Client")}</option>
            <option value="site">{t("Standort")}</option>
            <option value="device">{t("Gerät")}</option>
          </select>
          <select value={aTarget} onChange={(e) => setATarget(e.target.value)}>
            <option value="">{t("— Ziel wählen —")}</option>
            {targets.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
          <button className="btn primary" type="submit">+ {t("Zuweisen")}</button>
        </form>
      </section>
    </>
  );
}
