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
    <div className="unmanaged-panel">
      <div className="card">
        <div className="um-head">
          <div className="um-title">
            <span className="um-icon">{isPrinter ? "🖨" : "🖧"}</span>
            <div>
              <div className="um-name">{device.hostname || ip}</div>
              <div className="um-sub">
                <StatusBadge status="unmanaged" />
                {device.os && <span className="sep">{device.os}</span>}
                {device.site_name && <span className="sep">{device.client_name} › {device.site_name}</span>}
              </div>
            </div>
          </div>
          <div className="head-actions">
            {isPrinter && <button className="btn primary sm" disabled={busy} onClick={queryPrinter}>{busy ? t("Frage ab…") : t("Per SNMP abfragen")}</button>}
            {isAdmin && <button className="btn ghost sm" onClick={() => confirm(t("Gerät wirklich löschen?")) && remove.mutate()}>{t("Löschen")}</button>}
          </div>
        </div>

        <div className="um-facts">
          <div><span className="muted small">IP</span><span className="mono">{ip || "—"}</span></div>
          <div><span className="muted small">MAC</span><span className="mono">{mac || "—"}</span></div>
          <div><span className="muted small">Hostname</span><span>{device.hostname || "—"}</span></div>
          <div><span className="muted small">{t("Typ")}</span><span>{device.os || "—"}</span></div>
        </div>
        <p className="muted small" style={{ margin: "10px 0 0" }}>{t("Nicht verwaltetes Gerät (ohne Agent, aus dem Netzwerk-Scan). Keine Fernsteuerung/Checks – nur Bestandsdaten.")}</p>
      </div>

      {msg && <p className="form-err">{msg}</p>}
      {printer && (
        <div className="card">
          <h3 style={{ marginTop: 0 }}>🖨 {t("Druckerdaten")} <span className="muted small">(SNMP)</span></h3>
          <PrinterDetails info={printer} />
        </div>
      )}
    </div>
  );
}
