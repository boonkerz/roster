import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { Device, DockerDatabase } from "../types";

// engineLabel benennt die Datenbank-Engine lesbar.
const ENGINE: Record<string, string> = { mongo: "MongoDB", mysql: "MySQL/MariaDB", postgres: "PostgreSQL" };

// groupDBs fasst den Datenbank-Bestand je Container zusammen (Fehlerzeile oder Liste).
function groupDBs(dbs: DockerDatabase[]): { container: string; engine: string; error?: string; dbs: DockerDatabase[] }[] {
  const map = new Map<string, { container: string; engine: string; error?: string; dbs: DockerDatabase[] }>();
  for (const d of dbs) {
    let g = map.get(d.container);
    if (!g) { g = { container: d.container, engine: d.engine, dbs: [] }; map.set(d.container, g); }
    if (d.error) g.error = d.error; else g.dbs.push(d);
  }
  return [...map.values()];
}

// DockerPanel zeigt die vom Agent gemeldeten Docker-Container und -Images eines
// Geräts (periodisches Inventar). Mit Bedienrecht können Container gestartet,
// gestoppt oder neu gestartet werden (Ergebnis erscheint beim nächsten Inventar).
export function DockerPanel({ device, canOperate }: { device: Device; canOperate?: boolean }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const containers = device.docker_containers ?? [];
  const images = device.docker_images ?? [];
  const scans = new Map((device.docker_image_scans ?? []).map((s) => [s.image_id, s]));
  const dbGroups = groupDBs(device.docker_dbs ?? []);
  const scanImages = useMutation({
    mutationFn: () => api.post(`/devices/${device.id}/scan-images`, {}),
  });

  const control = useMutation({
    mutationFn: async (v: { container_id: string; action: "start" | "stop" | "restart" }) => {
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${device.id}/docker-control`, v);
      // Auf Ausführung warten – der Agent schickt im selben Checkin frisches Inventar.
      for (let i = 0; i < 40 && command_id; i++) {
        await new Promise((r) => setTimeout(r, 500));
        const cmd = await api.get<{ status: string }>(`/commands/${command_id}`);
        if (cmd.status === "done") break;
      }
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["device", device.id] });
      setTimeout(() => qc.invalidateQueries({ queryKey: ["device", device.id] }), 1500);
    },
  });

  if (containers.length === 0 && images.length === 0) {
    return (
      <section className="card" style={{ marginTop: 12 }}>
        <p className="muted small">{t("Kein Docker erkannt (oder Agent noch nicht aktualisiert).")}</p>
      </section>
    );
  }

  const running = containers.filter((c) => c.state === "running").length;

  return (
    <>
      <section className="card" style={{ marginTop: 12 }}>
        <h3 className="muted small" style={{ display: "flex", alignItems: "center", gap: 8, margin: 0 }}>
          {t("Container")}
          <span className="badge badge-online"><span className="dot" /> {running} {t("laufen")}</span>
          <span className="muted small">/ {containers.length} {t("gesamt")}</span>
        </h3>
        {containers.length === 0 ? (
          <p className="muted small">{t("Keine Container.")}</p>
        ) : (
          <div className="scroll-list">
            <table className="table">
              <thead><tr><th>{t("Status")}</th><th>{t("Name")}</th><th>Image</th><th>{t("Compose-Projekt")}</th><th>Ports</th><th></th>{canOperate && <th>{t("Aktion")}</th>}</tr></thead>
              <tbody>
                {containers.map((c, idx) => (
                  <tr key={idx}>
                    <td>{c.state === "running"
                      ? <span className="badge badge-online"><span className="dot" /> {c.state}</span>
                      : <span className="badge badge-offline"><span className="dot" /> {c.state}</span>}
                      {c.health === "unhealthy" && <span className="badge badge-offline" style={{ marginLeft: 4 }} title="Healthcheck">unhealthy</span>}
                      {c.health === "healthy" && <span className="muted small" style={{ marginLeft: 4 }} title="Healthcheck">healthy</span>}
                      {c.health === "starting" && <span className="muted small" style={{ marginLeft: 4 }}>starting</span>}</td>
                    <td className="link-strong">{c.name || "—"}</td>
                    <td className="mono small">{c.image}</td>
                    <td className="muted small">{c.compose || "—"}</td>
                    <td className="mono small">{c.ports || "—"}</td>
                    <td className="muted small" title={c.created}>{c.status}</td>
                    {canOperate && (
                      <td style={{ whiteSpace: "nowrap" }}>
                        {c.state === "running" ? (
                          <>
                            <button className="btn ghost sm" disabled={control.isPending || !c.container_id}
                              onClick={() => control.mutate({ container_id: c.container_id, action: "stop" })}>{t("Stoppen")}</button>
                            <button className="btn ghost sm" style={{ marginLeft: 4 }} disabled={control.isPending || !c.container_id}
                              onClick={() => control.mutate({ container_id: c.container_id, action: "restart" })}>{t("Neustart")}</button>
                          </>
                        ) : (
                          <button className="btn ghost sm" disabled={control.isPending || !c.container_id}
                            onClick={() => control.mutate({ container_id: c.container_id, action: "start" })}>{t("Starten")}</button>
                        )}
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {control.isError && <p className="error small" style={{ marginTop: 6 }}>{t("Aktion fehlgeschlagen.")}</p>}
      </section>

      {images.length > 0 && (
        <section className="card" style={{ marginTop: 12 }}>
          <div className="inline-form" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h3 className="muted small" style={{ margin: 0 }}>{t("Images")} <span className="muted small">({images.length})</span></h3>
            {canOperate && <button className="btn ghost sm" disabled={scanImages.isPending} onClick={() => scanImages.mutate()}
              title={t("Der Agent liest die Paketlisten aller Images laufender Container neu; der OSV-Abgleich folgt nach dem nächsten Checkin.")}>
              {scanImages.isSuccess ? t("Scan angestoßen") : t("Images neu scannen")}</button>}
          </div>
          <p className="muted small" style={{ margin: "4px 0 0" }}>{t("Pakete der Images laufender Container werden täglich gelesen und gegen OSV.dev abgeglichen (Alpine, Debian, Ubuntu, Rocky/Alma). Images ohne Shell (distroless) sind nicht prüfbar.")}</p>
          <div className="scroll-list">
            <table className="table">
              <thead><tr><th>Repository</th><th>Tag</th><th>{t("Basis-OS")}</th><th>{t("Pakete")}</th><th>{t("Schwachstellen")}</th><th>{t("Dienste")}</th><th>{t("Größe")}</th><th>{t("Erstellt")}</th><th>ID</th></tr></thead>
              <tbody>
                {[...images].sort((a, b) => {
                  // Geprüfte Images mit den meisten kritischen/hohen Treffern zuerst, ungeprüfte ans Ende.
                  const rank = (im: typeof a) => { const sc = scans.get(im.image_id); return sc ? (sc.error ? 1 : 2 + sc.critical * 1000 + sc.high * 10 + sc.vulns / 1000) : 0; };
                  return rank(b) - rank(a);
                }).map((im, idx) => {
                  const sc = scans.get(im.image_id);
                  return (
                    <tr key={idx}>
                      <td className="mono small">{im.repository}</td>
                      <td className="mono small">{im.tag}</td>
                      <td className="small" title={sc?.os_name}>{sc?.error ? <span className="muted" title={sc.error}>⚠ {t("nicht prüfbar")}</span> : sc ? (sc.os_name || sc.os || sc.pkg || "—") : <span className="muted">—</span>}</td>
                      <td className="muted small">{sc && !sc.error ? sc.package_count : "—"}</td>
                      <td>{sc && !sc.error ? (sc.vuln_scanned_at ? (sc.vulns === 0 ? <span className="badge badge-online">0</span> : <>
                        {sc.critical > 0 && <span className="badge badge-offline" title={t("kritisch")}>{sc.critical}</span>}
                        {sc.high > 0 && <span className="badge badge-offline" style={{ marginLeft: 3 }} title={t("hoch")}>{sc.high}</span>}
                        {sc.vulns - sc.critical - sc.high > 0 && <span className="badge badge-warn" style={{ marginLeft: 3 }}>{sc.vulns - sc.critical - sc.high}</span>}
                        <span className="muted small" style={{ marginLeft: 4 }} title={t("Mit neuem Image behebbar; der Rest ist im Release noch ohne Korrektur.")}>{t("{n} behebbar", { n: sc.fixable })}</span>
                      </>) : <span className="muted small">{t("Abgleich ausstehend")}</span>) : sc?.error ? <span className="muted small">—</span> : <span className="muted small" title={t("Image läuft in keinem Container oder noch nicht gescannt")}>—</span>}</td>
                      <td className="small">{(sc?.services ?? []).map((sv) => <span key={sv} className={`badge ${sv === "sshd" || sv === "telnetd/ftpd (busybox-extras)" ? "badge-warn" : "badge-unknown"}`} style={{ marginRight: 3 }} title={t("Netzdienst im Image erkannt")}>{sv}</span>)}</td>
                      <td className="muted small">{im.size || "—"}</td>
                      <td className="muted small" title={im.created_at}>{im.created || "—"}</td>
                      <td className="mono small muted">{im.image_id}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {dbGroups.length > 0 && (
        <section className="card" style={{ marginTop: 12 }}>
          <h3 className="muted small" style={{ margin: 0 }}>{t("Datenbanken")} <span className="muted small">({dbGroups.length} {t("Container")})</span></h3>
          <p className="muted small" style={{ margin: "4px 0 0" }}>{t("Bestand je Datenbank-Container (per docker exec mit den Zugangsdaten aus der Container-Umgebung). Neue, gelöschte oder geleerte Datenbanken zwischen zwei Checkins erzeugen den Vorfall „Datenbank-Abweichung“.")}</p>
          <div className="scroll-list">
            <table className="table">
              <thead><tr><th>{t("Container")}</th><th>Engine</th><th>{t("Datenbanken")}</th></tr></thead>
              <tbody>
                {dbGroups.map((g) => (
                  <tr key={g.container}>
                    <td className="link-strong">{g.container}</td>
                    <td className="muted small">{ENGINE[g.engine] ?? g.engine}</td>
                    <td className="small">{g.error ? <span className="badge badge-warn" title={g.error}>⚠ {g.error}</span>
                      : g.dbs.length === 0 ? <span className="muted">{t("keine")}</span>
                      : g.dbs.map((d) => <span key={d.name} className="badge badge-unknown" style={{ marginRight: 4 }} title={t("{n} Collections/Tabellen", { n: d.collections })}><span className="mono">{d.name}</span> <span className="muted">({d.collections})</span></span>)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </>
  );
}
