import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { Vulnerability } from "../types";
import { sevClass } from "./Vulnerabilities";

// FleetVulnerabilities zeigt alle erkannten Schwachstellen der Flotte.
export function FleetVulnerabilities() {
  const { t } = useI18n();
  const { data } = useQuery({ queryKey: ["fleet-vulns"], queryFn: () => api.get<Vulnerability[]>("/vulnerabilities") });
  const vulns = data ?? [];
  const crit = vulns.filter((v) => v.severity.toUpperCase() === "CRITICAL").length;
  const high = vulns.filter((v) => v.severity.toUpperCase() === "HIGH").length;

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>{t("Schwachstellen")}</h1>
          <p className="muted">{t("Über OSV.dev erkannte Schwachstellen. Scan je Gerät im Tab „Schwachstellen“.")}</p>
        </div>
      </header>
      <div className="inline-form" style={{ marginBottom: 10 }}>
        <span className="badge badge-offline">{crit} {t("kritisch")}</span>
        <span className="badge badge-warn">{high} {t("hoch")}</span>
        <span className="muted">{vulns.length} {t("gesamt")}</span>
      </div>
      {vulns.length === 0 ? (
        <p className="muted">{t("Keine Schwachstellen erkannt (oder noch nicht gescannt).")}</p>
      ) : (
        <table className="table">
          <thead><tr><th>{t("Stufe")}</th><th>{t("Gerät")}</th><th>CVE / OSV</th><th>{t("Paket")}</th><th>{t("Version")}</th><th>{t("Behoben in")}</th></tr></thead>
          <tbody>
            {vulns.map((v) => (
              <tr key={v.device_id + v.package + v.vuln_id}>
                <td><span className={`badge ${sevClass(v.severity)}`}>{v.severity || "—"}</span></td>
                <td><Link to={`/devices/${v.device_id}`} className="link-strong">{v.hostname || v.device_id}</Link></td>
                <td><a href={v.url} target="_blank" rel="noreferrer" className="mono">{v.vuln_id}</a></td>
                <td className="mono">{v.package}</td>
                <td className="mono muted">{v.version || "—"}</td>
                <td className="mono muted">{v.fixed || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
