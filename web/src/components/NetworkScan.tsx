import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { Device, ClientTree, Command, NetworkAsset, PrinterInfo } from "../types";
import { PrinterDetails } from "./PrinterDetails";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// primaryIP liefert die erste IPv4 eines Geräts (für die CIDR-Vorbelegung).
function primaryIP(d: Device): string {
  for (const i of d.interfaces ?? []) {
    const ip = (i.ipv4 || "").split(",")[0].trim();
    if (ip && !ip.startsWith("127.")) return ip;
  }
  return "";
}
function cidrFromIP(ip: string): string {
  const m = ip.match(/^(\d+)\.(\d+)\.(\d+)\.\d+$/);
  return m ? `${m[1]}.${m[2]}.${m[3]}.0/24` : "";
}

// guessType leitet aus den offenen Ports einen groben Gerätetyp ab.
function guessType(ports: string): string {
  const p = new Set(ports.split(",").map((x) => x.trim()));
  const has = (...xs: string[]) => xs.some((x) => p.has(x));
  if (has("9100", "631", "515")) return "🖨 Drucker";
  if (has("3389")) return "🪟 Windows (RDP)";
  if (has("445", "139", "135")) return "🪟 Windows/SMB";
  if (has("5900")) return "🖥 VNC";
  if (has("22")) return "🐧 SSH/Linux";
  if (has("80", "443", "8080")) return "🌐 Web/Gerät";
  return "—";
}

// NetworkScan lässt einen Agenten ein Segment scannen und importiert die Funde in eine Site.
export function NetworkScan() {
  const { t } = useI18n();
  const qc = useQueryClient();
  const { data: devices } = useQuery({ queryKey: ["devices"], queryFn: () => api.get<Device[]>("/devices") });
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });

  const [deviceId, setDeviceId] = useState("");
  const [cidr, setCidr] = useState("");
  const [siteId, setSiteId] = useState("");
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");

  const online = (devices ?? []).filter((d) => d.status === "online" && !d.revoked);
  const sites = useMemo(() => {
    const out: { id: string; label: string }[] = [];
    for (const c of tree?.clients ?? []) for (const s of c.sites ?? []) out.push({ id: s.id, label: `${c.name} › ${s.name}` });
    return out;
  }, [tree]);

  const pickDevice = (id: string) => {
    setDeviceId(id);
    const d = online.find((x) => x.id === id);
    if (d) setCidr(cidrFromIP(primaryIP(d)));
  };

  const { data: assets } = useQuery({
    queryKey: ["site-assets", siteId],
    queryFn: () => api.get<NetworkAsset[]>(`/sites/${siteId}/assets`),
    enabled: !!siteId,
    refetchInterval: busy ? 3000 : false,
  });

  const start = async () => {
    if (!deviceId || !cidr || !siteId) { setMsg(t("Bitte Agent, Bereich und Site wählen.")); return; }
    setBusy(true); setMsg(t("Scan läuft… (kann eine Minute dauern)"));
    try {
      const resp = await api.post<{ command_id?: string; imported?: number }>("/network-scan", { device_id: deviceId, cidr, site_id: siteId });
      if (!resp.command_id) {
        // Server-Scan: synchron erledigt.
        setMsg(t("Scan fertig – Funde importiert."));
        qc.invalidateQueries({ queryKey: ["site-assets", siteId] });
      } else {
        for (let i = 0; i < 90; i++) {
          await sleep(2500);
          const cmd = await api.get<Command>(`/commands/${resp.command_id}`);
          if (cmd.status === "done") {
            setMsg(cmd.exit_code === 0 ? t("Scan fertig – Funde importiert.") : t("Scan fehlgeschlagen: {o}", { o: cmd.output || "" }));
            qc.invalidateQueries({ queryKey: ["site-assets", siteId] });
            break;
          }
        }
      }
    } catch (e) { setMsg((e as Error).message); } finally { setBusy(false); }
  };

  // SNMP-Druckerabfrage (über den gewählten Agenten bzw. den Server).
  const [printer, setPrinter] = useState<PrinterInfo | null>(null);
  const [pBusy, setPBusy] = useState("");
  const queryPrinter = async (ip: string) => {
    setPBusy(ip); setPrinter(null);
    try {
      const resp = await api.post<{ command_id?: string } & Partial<PrinterInfo>>("/snmp-printer", { device_id: deviceId || "server", ip });
      if (!resp.command_id) { setPrinter(resp as PrinterInfo); return; }
      for (let i = 0; i < 20; i++) {
        await sleep(2000);
        const cmd = await api.get<Command>(`/commands/${resp.command_id}`);
        if (cmd.status === "done") {
          if (cmd.exit_code === 0) setPrinter(JSON.parse(cmd.output || "{}") as PrinterInfo);
          else setMsg(t("SNMP fehlgeschlagen (Community/Firewall?)."));
          break;
        }
      }
    } catch { setMsg(t("SNMP fehlgeschlagen (Community/Firewall?).")); } finally { setPBusy(""); }
  };

  const del = async (id: string) => { await api.del(`/network-assets/${id}`); qc.invalidateQueries({ queryKey: ["site-assets", siteId] }); };
  const adopt = async (id: string) => { await api.post(`/network-assets/${id}/adopt`, {}); qc.invalidateQueries({ queryKey: ["site-assets", siteId] }); qc.invalidateQueries({ queryKey: ["devices"] }); };
  const adoptAll = async () => {
    const r = await api.post<{ adopted: number }>(`/sites/${siteId}/assets/adopt-all`, {});
    setMsg(t("{n} als nicht verwaltete Geräte übernommen.", { n: r.adopted }));
    qc.invalidateQueries({ queryKey: ["site-assets", siteId] }); qc.invalidateQueries({ queryKey: ["devices"] });
  };

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>{t("Netzwerk-Scan")}</h1>
          <p className="muted">{t("Ein online-Agent im Zielsegment tastet den Bereich ab (TCP/ARP/DNS); die Funde landen als Assets in der gewählten Site.")}</p>
        </div>
      </header>

      <section className="card">
        <div className="inline-form" style={{ flexWrap: "wrap", alignItems: "flex-end", gap: 10 }}>
          <label className="field"><span>{t("Scan-Agent (online)")}</span>
            <select value={deviceId} onChange={(e) => pickDevice(e.target.value)}>
              <option value="">{t("— wählen —")}</option>
              <option value="server">{t("Dieser Server (dieses Netz)")}</option>
              {online.map((d) => <option key={d.id} value={d.id}>{d.hostname || d.id} ({primaryIP(d) || "?"})</option>)}
            </select>
          </label>
          <label className="field"><span>{t("Bereich (CIDR)")}</span>
            <input value={cidr} placeholder="192.168.1.0/24" onChange={(e) => setCidr(e.target.value)} />
          </label>
          <label className="field"><span>{t("Ziel-Site")}</span>
            <select value={siteId} onChange={(e) => setSiteId(e.target.value)}>
              <option value="">{t("— wählen —")}</option>
              {sites.map((s) => <option key={s.id} value={s.id}>{s.label}</option>)}
            </select>
          </label>
          <button className="btn primary" disabled={busy} onClick={start}>{busy ? t("Scanne…") : t("Scan starten")}</button>
        </div>
        {msg && <p className={msg.includes("fehlgeschlagen") ? "form-err" : "form-ok"} style={{ marginTop: 8 }}>{msg}</p>}
      </section>

      {siteId && (
        <section className="card">
          <div className="inline-form" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h3 style={{ margin: 0 }}>{t("Assets in dieser Site")} {assets && <span className="muted">({assets.length})</span>}</h3>
            {assets && assets.some((a) => !a.managed) && (
              <button className="btn ghost sm" onClick={adoptAll} title={t("Alle nicht verwalteten als Geräte aufnehmen")}>{t("Alle übernehmen")}</button>
            )}
          </div>
          {(!assets || assets.length === 0) ? (
            <p className="muted">{t("Noch keine Assets. Starte einen Scan.")}</p>
          ) : (
            <table className="table">
              <thead><tr><th>IP</th><th>Hostname</th><th>{t("Typ")}</th><th>MAC</th><th>{t("Ports")}</th><th>{t("Verwaltet")}</th><th></th></tr></thead>
              <tbody>
                {assets.map((a) => (
                  <tr key={a.id}>
                    <td className="mono">{a.ip}</td>
                    <td>{a.hostname || "—"}</td>
                    <td>{guessType(a.ports)}</td>
                    <td className="mono muted">{a.mac || "—"}</td>
                    <td className="mono muted small">{a.ports || "—"}</td>
                    <td>{a.managed ? <span className="badge badge-online">{t("verwaltet")}</span> : <span className="muted">—</span>}</td>
                    <td style={{ whiteSpace: "nowrap" }}>
                      {guessType(a.ports).includes("Drucker") && <button className="btn ghost sm" disabled={pBusy === a.ip} onClick={() => queryPrinter(a.ip)} title={t("Drucker per SNMP abfragen")}>{pBusy === a.ip ? "…" : "🖨"}</button>}
                      {!a.managed && <button className="btn ghost sm" onClick={() => adopt(a.id)} title={t("Als nicht verwaltetes Gerät aufnehmen")}>{t("Übernehmen")}</button>}
                      <button className="btn ghost sm" onClick={() => del(a.id)}>{t("Löschen")}</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      )}

      {printer && (
        <section className="card">
          <div className="inline-form" style={{ justifyContent: "space-between", alignItems: "center" }}>
            <h3 style={{ margin: 0 }}>🖨 {t("Drucker")} {printer.ip}</h3>
            <button className="btn ghost sm" onClick={() => setPrinter(null)}>{t("Schließen")}</button>
          </div>
          <div style={{ marginTop: 8 }}><PrinterDetails info={printer} /></div>
        </section>
      )}
    </div>
  );
}
