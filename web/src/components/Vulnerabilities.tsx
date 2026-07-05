import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { Vulnerability } from "../types";

// sevClass ordnet einer OSV/CVE-Stufe eine Badge-Farbe zu.
export function sevClass(sev: string): string {
  switch (sev.toUpperCase()) {
    case "CRITICAL": return "badge-offline";
    case "HIGH": return "badge-offline";
    case "MEDIUM": return "badge-warn";
    case "LOW": return "badge-unknown";
    default: return "badge-unknown";
  }
}

// Vulnerabilities zeigt die CVE/OSV-Treffer eines Geräts und erlaubt einen Neu-Scan.
export function Vulnerabilities({ deviceId }: { deviceId: string }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const [msg, setMsg] = useState("");
  const { data } = useQuery({
    queryKey: ["vulns", deviceId],
    queryFn: () => api.get<Vulnerability[]>(`/devices/${deviceId}/vulnerabilities`),
  });
  const scan = useMutation({
    mutationFn: () => api.post<{ count: number }>(`/devices/${deviceId}/scan-cve`, {}),
    onSuccess: (d) => { setMsg(t("{n} Schwachstellen gefunden.", { n: d.count })); qc.invalidateQueries({ queryKey: ["vulns", deviceId] }); },
    onError: () => setMsg(t("Scan fehlgeschlagen (OSV.dev nicht erreichbar?).")),
  });
  const vulns = data ?? [];

  return (
    <section className="card">
      <div className="inline-form" style={{ justifyContent: "space-between", alignItems: "center" }}>
        <h3 style={{ margin: 0 }}>{t("Schwachstellen")} {vulns.length > 0 && <span className="muted">({vulns.length})</span>}</h3>
        <button className="btn primary sm" disabled={scan.isPending} onClick={() => { setMsg(""); scan.mutate(); }}>
          {scan.isPending ? t("Scanne…") : t("Jetzt scannen")}
        </button>
      </div>
      <p className="muted small">{t("Abgleich der installierten Software gegen OSV.dev (CVE). Beste Abdeckung bei Linux-Paketen; Windows/macOS best effort.")}</p>
      {msg && <p className="form-ok">{msg}</p>}
      {vulns.length === 0 ? (
        <p className="muted">{t("Keine bekannten Schwachstellen (oder noch nicht gescannt).")}</p>
      ) : (
        <table className="table">
          <thead><tr><th>{t("Stufe")}</th><th>CVE / OSV</th><th>{t("Paket")}</th><th>{t("Version")}</th><th>{t("Behoben in")}</th><th>{t("Beschreibung")}</th></tr></thead>
          <tbody>
            {vulns.map((v) => (
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
    </section>
  );
}
