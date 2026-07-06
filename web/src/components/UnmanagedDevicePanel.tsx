import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import { useI18n } from "../i18n";
import { useAuth } from "../auth";
import type { Command, Device, PrinterInfo } from "../types";
import { StatusBadge } from "./StatusBadge";
import { PrinterDetails } from "./PrinterDetails";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// UnmanagedDevicePanel zeigt ein Gerät ohne Agent (z. B. aus dem Netzwerk-Scan). Für
// Drucker gibt es eine SNMP-Abfrage statt der (agentbasierten) Standard-Tabs.
export function UnmanagedDevicePanel({ device }: { device: Device }) {
  const { t } = useI18n();
  const { user } = useAuth();
  const qc = useQueryClient();
  const nav = useNavigate();
  const isAdmin = user?.role === "admin";
  const isPrinter = (device.os || "").toLowerCase().includes("drucker") || (device.os || "").toLowerCase().includes("printer");

  const ip = device.interfaces?.map((i) => (i.ipv4 || "").split(",")[0].trim()).find(Boolean) || "";
  const mac = device.interfaces?.map((i) => i.mac).find(Boolean) || "";

  const [printer, setPrinter] = useState<PrinterInfo | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");

  const queryPrinter = async () => {
    setBusy(true); setMsg(""); setPrinter(null);
    try {
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${device.id}/snmp`, {});
      for (let i = 0; i < 20; i++) {
        await sleep(2000);
        const cmd = await api.get<Command>(`/commands/${command_id}`);
        if (cmd.status === "done") {
          if (cmd.exit_code === 0) setPrinter(JSON.parse(cmd.output || "{}") as PrinterInfo);
          else setMsg(t("SNMP fehlgeschlagen (Community/Firewall?)."));
          break;
        }
      }
    } catch (e) { setMsg((e as Error).message); } finally { setBusy(false); }
  };

  const remove = useMutation({
    mutationFn: () => api.del(`/devices/${device.id}`),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ["devices"] }); nav("/devices"); },
  });

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h2>{isPrinter ? "🖨 " : "🖧 "}{device.hostname || ip}</h2>
          <div className="muted small">
            <StatusBadge status="unmanaged" />
            {device.os && <span style={{ marginLeft: 8 }}>{device.os}</span>}
            {device.site_name && <span style={{ marginLeft: 8 }}>{device.client_name} › {device.site_name}</span>}
          </div>
        </div>
        <div className="head-actions">
          {isPrinter && <button className="btn primary sm" disabled={busy} onClick={queryPrinter}>{busy ? t("Frage ab…") : t("Per SNMP abfragen")}</button>}
          {isAdmin && <button className="btn ghost sm" onClick={() => confirm(t("Gerät wirklich löschen?")) && remove.mutate()}>{t("Löschen")}</button>}
        </div>
      </header>

      <section className="card">
        <p className="muted small">{t("Nicht verwaltetes Gerät (ohne Agent, aus dem Netzwerk-Scan). Keine Fernsteuerung/Checks – nur Bestandsdaten.")}</p>
        <div className="kv-grid">
          <div><span className="muted">IP</span><div className="mono">{ip || "—"}</div></div>
          <div><span className="muted">MAC</span><div className="mono">{mac || "—"}</div></div>
          <div><span className="muted">Hostname</span><div>{device.hostname || "—"}</div></div>
          <div><span className="muted">{t("Typ")}</span><div>{device.os || "—"}</div></div>
        </div>
      </section>

      {msg && <p className="form-err">{msg}</p>}
      {printer && (
        <section className="card">
          <h3 style={{ marginTop: 0 }}>🖨 {t("Druckerdaten")} <span className="muted small">(SNMP)</span></h3>
          <PrinterDetails info={printer} />
        </section>
      )}
    </div>
  );
}
