import { useState } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import { pollCmd } from "../pollCmd";

interface AV { product: string; enabled: boolean; realtime: boolean; signature_age_days: number; version?: string; info?: string; }
interface BLVol { mount_point: string; protection: string; percent: number; recovery_key?: string; recovery_id?: string; }
interface Disk { name: string; model?: string; health: string; detail?: string; }

// SecurityPanel fragt Virenschutz, BitLocker und SMART on-demand ab.
export function SecurityPanel({ deviceId }: { deviceId: string }) {
  const { t } = useI18n();
  const [av, setAv] = useState<AV | null>(null);
  const [bl, setBl] = useState<{ volumes: BLVol[]; info?: string } | null>(null);
  const [disks, setDisks] = useState<{ disks: Disk[]; info?: string } | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const fetchAv = async () => {
    setBusy("av"); setError("");
    try {
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${deviceId}/av-status`);
      const cmd = await pollCmd(command_id);
      setAv(JSON.parse(cmd.output || "{}"));
    } catch (e) { setError((e as Error).message); } finally { setBusy(""); }
  };
  const fetchBl = async () => {
    setBusy("bl"); setError("");
    try {
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${deviceId}/bitlocker`);
      await pollCmd(command_id);
      // Ergebnis über den Escrow-Endpunkt holen (speichert Recovery-Keys serverseitig).
      const res = await api.get<{ volumes: BLVol[]; info?: string }>(`/devices/${deviceId}/bitlocker/${command_id}`);
      setBl(res);
    } catch (e) { setError((e as Error).message); } finally { setBusy(""); }
  };
  const fetchSmart = async () => {
    setBusy("smart"); setError("");
    try {
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${deviceId}/smart`);
      const cmd = await pollCmd(command_id);
      setDisks(JSON.parse(cmd.output || "{}"));
    } catch (e) { setError((e as Error).message); } finally { setBusy(""); }
  };

  const healthBadge = (h: string) => {
    const cls = h === "OK" ? "badge-online" : h === "Fehler" ? "badge-offline" : h === "Warnung" ? "badge-warn" : "badge-unknown";
    return <span className={`badge ${cls}`}>{h}</span>;
  };

  return (
    <section className="card">
      {error && <p className="muted">{t("Fehler")}: {error}</p>}

      <div className="page-head" style={{ marginBottom: 8 }}>
        <h3 className="muted small" style={{ margin: 0 }}>{t("Virenschutz")}</h3>
        <button className="btn ghost sm" disabled={!!busy} onClick={fetchAv}>{busy === "av" ? "…" : t("Abfragen")}</button>
      </div>
      {av && (av.info ? <p className="muted small">{av.info}</p> : (
        <table className="table"><tbody>
          <tr><td>{t("Produkt")}</td><td>{av.product}</td></tr>
          <tr><td>{t("Aktiv")}</td><td>{av.enabled ? t("ja") : <span className="badge badge-offline">{t("nein")}</span>}</td></tr>
          <tr><td>{t("Echtzeitschutz")}</td><td>{av.realtime ? t("ja") : <span className="badge badge-warn">{t("nein")}</span>}</td></tr>
          <tr><td>{t("Signatur-Alter")}</td><td>{t("{n} Tage", { n: av.signature_age_days })}{av.signature_age_days > 7 ? " ⚠" : ""}</td></tr>
          {av.version && <tr><td>{t("Version")}</td><td className="mono small">{av.version}</td></tr>}
        </tbody></table>
      ))}

      <div className="page-head" style={{ marginBottom: 8, marginTop: 18 }}>
        <h3 className="muted small" style={{ margin: 0 }}>{t("BitLocker")}</h3>
        <button className="btn ghost sm" disabled={!!busy} onClick={fetchBl}>{busy === "bl" ? "…" : t("Abfragen")}</button>
      </div>
      {bl && (bl.info && (bl.volumes ?? []).length === 0 ? <p className="muted small">{bl.info}</p> : (
        <table className="table">
          <thead><tr><th>{t("Volume")}</th><th>{t("Schutz")}</th><th>{t("Verschlüsselt")}</th><th>{t("Wiederherstellungsschlüssel")}</th></tr></thead>
          <tbody>
            {(bl.volumes ?? []).map((v) => (
              <tr key={v.mount_point}>
                <td>{v.mount_point}</td>
                <td>{v.protection === "On" ? <span className="badge badge-online">{t("An")}</span> : <span className="badge badge-warn">{v.protection === "Off" ? t("Aus") : "?"}</span>}</td>
                <td className="muted">{v.percent}%</td>
                <td className="mono small">{v.recovery_key || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ))}

      <div className="page-head" style={{ marginBottom: 8, marginTop: 18 }}>
        <h3 className="muted small" style={{ margin: 0 }}>{t("Datenträgergesundheit (SMART)")}</h3>
        <button className="btn ghost sm" disabled={!!busy} onClick={fetchSmart}>{busy === "smart" ? "…" : t("Abfragen")}</button>
      </div>
      {disks && ((disks.disks ?? []).length === 0 && disks.info ? <p className="muted small">{disks.info}</p> : (
        <table className="table">
          <thead><tr><th>{t("Gerät")}</th><th>{t("Modell")}</th><th>{t("Status")}</th></tr></thead>
          <tbody>
            {(disks.disks ?? []).map((d) => (
              <tr key={d.name}>
                <td className="mono small">{d.name}</td>
                <td>{d.model || "—"}</td>
                <td>{healthBadge(d.health)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ))}
      <p className="muted small" style={{ marginTop: 10 }}>{t("On-demand-Abfrage; der Agent muss online sein. BitLocker-Wiederherstellungsschlüssel werden serverseitig hinterlegt (Escrow).")}</p>
    </section>
  );
}
