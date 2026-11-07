import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../auth";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { ContainerVulnerability, FleetContainers as FleetData, FleetImageRow, Flow, FlowTemplate } from "../types";
import { sevClass } from "./Vulnerabilities";
import { localizeTemplate } from "../flows/nodeTypes";

const ENGINE: Record<string, string> = { mongo: "MongoDB", mysql: "MySQL/MariaDB", postgres: "PostgreSQL" };
const RISKY_SERVICES = new Set(["sshd", "telnetd/ftpd (busybox-extras)", "telnetd", "ftpd"]);

// FleetContainers ist der Tab „Container“ unter Sicherheit: Images der ganzen Flotte mit
// OSV-Treffern, erkannten Diensten und der Datenbank-Bestand aller DB-Container.
export function FleetContainers() {
  const { t } = useI18n();
  const { data } = useQuery({ queryKey: ["fleet-containers"], queryFn: () => api.get<FleetData>("/security/containers"), refetchInterval: 60000 });
  const [q, setQ] = useState("");
  const [onlyBad, setOnlyBad] = useState(false);
  const [onlyServices, setOnlyServices] = useState(false);
  const [open, setOpen] = useState<string | null>(null);
  const images = data?.images ?? [];
  const dbs = data?.databases ?? [];

  const filtered = useMemo(() => images.filter((im) => {
    if (onlyBad && im.critical + im.high === 0) return false;
    if (onlyServices && !(im.services ?? []).some((s) => RISKY_SERVICES.has(s))) return false;
    const s = q.trim().toLowerCase();
    if (s && !(im.image.toLowerCase().includes(s) || (im.os_name ?? "").toLowerCase().includes(s) || im.hostnames.some((h) => h.toLowerCase().includes(s)))) return false;
    return true;
  }), [images, q, onlyBad, onlyServices]);

  const withBad = images.filter((im) => im.critical + im.high > 0).length;
  const pending = images.filter((im) => im.pending).length;
  const notCheckable = images.filter((im) => im.error).length;
  const withSSH = images.filter((im) => (im.services ?? []).some((s) => RISKY_SERVICES.has(s))).length;
  const dbErrors = dbs.filter((d) => d.error).length;
  const keyOf = (im: FleetImageRow) => im.image + "|" + im.os + "|" + im.os_version;

  return (
    <>
      <div className="inline-form" style={{ margin: "10px 0", gap: 8, flexWrap: "wrap" }}>
        <span className="badge badge-unknown">{images.length} {t("Images")}</span>
        <span className={`badge ${withBad > 0 ? "badge-offline" : "badge-online"}`}>{withBad} {t("mit kritischen/hohen Treffern")}</span>
        {withSSH > 0 && <span className="badge badge-warn">{withSSH} {t("mit SSH/FTP/Telnet")}</span>}
        {pending > 0 && <span className="muted small">{t("{n} Abgleich ausstehend", { n: pending })}</span>}
        {notCheckable > 0 && <span className="muted small">{t("{n} nicht prüfbar", { n: notCheckable })}</span>}
        <span className="badge badge-unknown">{dbs.length} {t("Datenbank-Container")}</span>
        {dbErrors > 0 && <span className="badge badge-warn">{dbErrors} {t("nicht abfragbar")}</span>}
        <Link to="/security?tab=incidents&kind=db_change&status=" className="small">{t("Datenbank-Abweichungen anzeigen")} →</Link>
      </div>

      <section className="card">
        <div className="inline-form" style={{ justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 8 }}>
          <h3 style={{ margin: 0 }}>{t("Container-Images")}</h3>
          <div className="inline-form" style={{ gap: 8 }}>
            <input placeholder={t("Image, Basis-OS oder Gerät …")} value={q} onChange={(e) => setQ(e.target.value)} style={{ minWidth: 220 }} />
            <label className="chip"><input type="checkbox" checked={onlyBad} onChange={(e) => setOnlyBad(e.target.checked)} /> {t("nur kritisch/hoch")}</label>
            <label className="chip"><input type="checkbox" checked={onlyServices} onChange={(e) => setOnlyServices(e.target.checked)} /> {t("nur mit SSH/FTP/Telnet")}</label>
          </div>
        </div>
        <p className="muted small">{t("Pakete der Images laufender Container, täglich per docker exec gelesen und gegen OSV.dev abgeglichen. Gleiche Images auf mehreren Geräten sind zusammengefasst. Behebung: Image neu bauen oder aktualisieren.")}</p>
        {filtered.length === 0 ? (
          <p className="muted">{images.length === 0 ? t("Noch keine Images gescannt (Agent ab 0.20.0, Docker-Hosts).") : t("Keine Treffer für den Filter.")}</p>
        ) : (
          <table className="table">
            <thead><tr><th>Image</th><th>{t("Basis-OS")}</th><th>{t("Geräte")}</th><th>{t("Pakete")}</th><th>{t("Schwachstellen")}</th><th>{t("Dienste")}</th></tr></thead>
            <tbody>
              {filtered.map((im) => {
                const k = keyOf(im);
                return [
                  <tr key={k} className="clickable" onClick={() => setOpen(open === k ? null : k)} style={{ cursor: "pointer" }}>
                    <td className="mono small">{open === k ? "▾ " : "▸ "}{im.image}</td>
                    <td className="small">{im.error ? <span className="muted" title={im.error}>⚠ {t("nicht prüfbar")}</span> : (im.os_name || im.os || "—")}</td>
                    <td>{im.hosts}</td>
                    <td className="muted small">{im.error ? "—" : im.packages}</td>
                    <td>{im.error ? <span className="muted small">—</span> : im.pending && im.vulns === 0 ? <span className="muted small">{t("Abgleich ausstehend")}</span> : im.vulns === 0 ? <span className="badge badge-online">0</span> : <>
                      {im.critical > 0 && <span className="badge badge-offline" title={t("kritisch")}>{im.critical} {t("krit.")}</span>}
                      {im.high > 0 && <span className="badge badge-offline" style={{ marginLeft: 3 }} title={t("hoch")}>{im.high} {t("hoch")}</span>}
                      <span className="muted small" style={{ marginLeft: 4 }}>{t("{n} gesamt", { n: im.vulns })}</span>
                    </>}</td>
                    <td className="small">{(im.services ?? []).map((sv) => <span key={sv} className={`badge ${RISKY_SERVICES.has(sv) ? "badge-warn" : "badge-unknown"}`} style={{ marginRight: 3 }}>{sv}</span>)}</td>
                  </tr>,
                  open === k && <tr key={k + "-d"}><td colSpan={6}><ImageDetail row={im} /></td></tr>,
                ];
              })}
            </tbody>
          </table>
        )}
      </section>

      <section className="card" style={{ marginTop: 12 }}>
        <h3 style={{ margin: 0 }}>{t("Datenbanken in Containern")}</h3>
        <p className="muted small">{t("Bestand je MongoDB-, MySQL/MariaDB- und PostgreSQL-Container (Zugangsdaten aus der Container-Umgebung). Neue, gelöschte oder geleerte Datenbanken erzeugen den Vorfall „Datenbank-Abweichung“ – typisch für Datenbank-Erpressung.")}</p>
        {dbs.length === 0 ? <p className="muted">{t("Keine Datenbank-Container erkannt.")}</p> : (
          <table className="table">
            <thead><tr><th>{t("Gerät")}</th><th>{t("Container")}</th><th>Engine</th><th>{t("Datenbanken")}</th></tr></thead>
            <tbody>
              {[...dbs].sort((a, b) => Number(!!b.error) - Number(!!a.error)).map((d) => (
                <tr key={d.device_id + d.container}>
                  <td><Link to={`/devices/${d.device_id}?tab=docker`} className="link-strong">{d.hostname}</Link></td>
                  <td className="mono small">{d.container}</td>
                  <td className="muted small">{ENGINE[d.engine] ?? d.engine}</td>
                  <td className="small">{d.error ? <span className="badge badge-warn" title={d.error}>⚠ {d.error}</span>
                    : d.databases.length === 0 ? <span className="muted">{t("keine")}</span>
                    : d.databases.map((x) => <span key={x.name} className="badge badge-unknown" style={{ marginRight: 4 }} title={t("{n} Collections/Tabellen", { n: x.collections })}><span className="mono">{x.name}</span> <span className="muted">({x.collections})</span></span>)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}

// ImageDetail zeigt Geräte und Treffer eines Images (vom ersten Gerät; gleiche Images = gleiche Pakete).
function ImageDetail({ row }: { row: FleetImageRow }) {
  const { t } = useI18n();
  const dev = row.devices[0];
  const { data } = useQuery({
    queryKey: ["container-vulns", dev],
    queryFn: () => api.get<ContainerVulnerability[]>(`/devices/${dev}/container-vulnerabilities`),
    enabled: !!dev && row.vulns > 0,
  });
  const vulns = (data ?? []).filter((v) => v.image === row.image);
  const nav = useNavigate();
  const { hasPerm } = useAuth();
  const [busy, setBusy] = useState(false);
  // replaceImage öffnet den Ablauf „Image in Compose ersetzen“ (legt ihn bei Bedarf aus der
  // Vorlage an) mit altem Image, Vorschau-Server und betroffenen Geräten vorbelegt.
  const replaceImage = async () => {
    setBusy(true);
    try {
      const flows = await api.get<Flow[]>("/flows");
      let f = flows.find((x) => x.graph.nodes.some((n) => n.type === "find_files" && n.config?.mode === "compose_image" && n.config?.image === "{{param.FROM}}") && x.params.some((p) => p.name === "TO"));
      if (!f && hasPerm("page.scripts")) {
        const tpl = (await api.get<FlowTemplate[]>("/flows/templates")).find((x) => x.key === "compose_image");
        if (tpl) f = await api.post<Flow>("/flows", localizeTemplate(tpl, t));
      }
      if (!f) { window.alert(t("Kein Ablauf „Image in Compose ersetzen“ vorhanden – bitte unter Abläufe aus der Vorlage anlegen.")); return; }
      const from = row.image.replace(/@.*$/, "").replace(/:[^/:]*$/, "");
      const q = new URLSearchParams({ "p.FROM": from, device: row.devices[0] ?? "", devices: row.devices.join(",") });
      nav(`/flows/${f.id}/run?${q.toString()}`);
    } finally { setBusy(false); }
  };
  return (
    <div style={{ padding: "4px 0 8px" }}>
      {hasPerm("devices.operate") && !row.error && (
        <button className="btn ghost sm" style={{ float: "right" }} disabled={busy} onClick={replaceImage}
          title={t("Öffnet den Ablauf „Image in Compose ersetzen“: Vorschau auf einem Server, Testlauf, dann alle betroffenen Geräte.")}>{t("Image ersetzen …")}</button>
      )}
      <div className="small" style={{ marginBottom: 6 }}>
        <span className="muted">{t("Geräte")}: </span>
        {row.devices.map((id, i) => <span key={id}>{i > 0 && ", "}<Link to={`/devices/${id}?tab=docker`}>{row.hostnames[i] || id}</Link></span>)}
        {row.hosts > row.devices.length && <span className="muted"> {t("+{n} weitere", { n: row.hosts - row.devices.length })}</span>}
      </div>
      {row.error && <p className="muted small">{row.error}</p>}
      {vulns.length > 0 && (
        <table className="table">
          <thead><tr><th>{t("Stufe")}</th><th>CVE / OSV</th><th>{t("Paket")}</th><th>{t("Version")}</th><th>{t("Behoben in")}</th><th>{t("Beschreibung")}</th></tr></thead>
          <tbody>
            {vulns.slice(0, 200).map((v) => (
              <tr key={v.package + v.vuln_id}>
                <td><span className={`badge ${sevClass(v.severity)}`}>{v.severity || "—"}</span></td>
                <td><a href={v.url} target="_blank" rel="noreferrer" className="mono">{v.vuln_id}</a></td>
                <td className="mono">{v.package}</td>
                <td className="mono muted">{v.version || "—"}</td>
                <td className="mono muted">{v.fixed || "—"}</td>
                <td className="muted small">{v.summary || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      {vulns.length > 200 && <p className="muted small">{t("+{n} weitere", { n: vulns.length - 200 })}</p>}
    </div>
  );
}
