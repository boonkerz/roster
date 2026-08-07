import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { Device } from "../types";

// DockerPanel zeigt die vom Agent gemeldeten Docker-Container und -Images eines
// Geräts (periodisches Inventar). Mit Bedienrecht können Container gestartet,
// gestoppt oder neu gestartet werden (Ergebnis erscheint beim nächsten Inventar).
export function DockerPanel({ device, canOperate }: { device: Device; canOperate?: boolean }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const containers = device.docker_containers ?? [];
  const images = device.docker_images ?? [];

  const control = useMutation({
    mutationFn: (v: { container_id: string; action: "start" | "stop" | "restart" }) =>
      api.post(`/devices/${device.id}/docker-control`, v),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["device", device.id] });
      setTimeout(() => qc.invalidateQueries({ queryKey: ["device", device.id] }), 4000);
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
          <h3 className="muted small" style={{ margin: 0 }}>{t("Images")} <span className="muted small">({images.length})</span></h3>
          <div className="scroll-list">
            <table className="table">
              <thead><tr><th>Repository</th><th>Tag</th><th>{t("Größe")}</th><th>{t("Erstellt")}</th><th>ID</th></tr></thead>
              <tbody>
                {images.map((im, idx) => (
                  <tr key={idx}>
                    <td className="mono small">{im.repository}</td>
                    <td className="mono small">{im.tag}</td>
                    <td className="muted small">{im.size || "—"}</td>
                    <td className="muted small">{im.created || "—"}</td>
                    <td className="mono small muted">{im.image_id}</td>
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
