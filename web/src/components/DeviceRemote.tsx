import { useEffect, useRef, useState } from "react";
import RFB from "@novnc/novnc";
import { useI18n } from "../i18n";
import { api } from "../api";
import { useAuth } from "../auth";

// DeviceRemote öffnet eine Web-Fernsteuerung (Remote Desktop) über noVNC. Der Server
// startet on-demand einen VNC-Server am Gerät und tunnelt die RFB-Bytes über eine
// WebSocket – gleiche Origin, daher Cookie-Auth. fill = füllt das Popout-Fenster;
// autoStart = sofort verbinden (Popout).
export function DeviceRemote({ id, os, fill, autoStart }: {
  id: string; os: string; fill?: boolean; autoStart?: boolean;
}) {
  const { t } = useI18n();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const [status, setStatus] = useState("");
  const [connected, setConnected] = useState(false);
  const [monitor, setMonitor] = useState(1); // 1=primär, 0=alle, N=Monitor N
  const [session, setSession] = useState(autoStart ? 1 : 0); // hochzählen = (neu) verbinden
  const hostRef = useRef<HTMLDivElement>(null);
  const rfbRef = useRef<any>(null);

  // Zustimmungs-Modus (device-level; "" = erben). Nur für Admin steuerbar.
  const [consent, setConsent] = useState<{ effective: string; device: string } | null>(null);
  useEffect(() => {
    if (!isAdmin || fill) return; // im Popout nicht anzeigen
    api.get<{ effective: string; device: string }>(`/devices/${id}/remote-consent`).then(setConsent).catch(() => {});
  }, [id, isAdmin, fill]);
  const setConsentMode = async (mode: string) => {
    await api.put(`/remote-consent`, { target_type: "device", target_id: id, mode });
    const c = await api.get<{ effective: string; device: string }>(`/devices/${id}/remote-consent`);
    setConsent(c);
  };

  const popout = () => {
    window.open(`/devices/${id}/remote?os=${encodeURIComponent(os)}`, `remote-${id}`,
      "width=1024,height=720,menubar=no,toolbar=no,location=no,status=no");
  };

  useEffect(() => {
    if (session === 0 || !hostRef.current) return;
    let rfb: any = null;
    let cancelled = false;
    setStatus(t("verbinde…"));
    setConnected(false);

    (async () => {
      let start: { session: string; password: string };
      try {
        start = await api.post<{ session: string; password: string }>(`/devices/${id}/remote/start`, { monitor });
      } catch (e) {
        if (!cancelled) setStatus(t("Fernsteuerung konnte nicht gestartet werden."));
        return;
      }
      if (cancelled || !hostRef.current) return;

      const proto = location.protocol === "https:" ? "wss" : "ws";
      const url = `${proto}://${location.host}/api/v1/devices/${id}/remote/ws?session=${encodeURIComponent(start.session)}`;
      rfb = new RFB(hostRef.current, url, { credentials: { password: start.password } });
      rfb.scaleViewport = true;
      rfb.clipViewport = true;
      rfb.background = "#0b0e14";
      rfbRef.current = rfb;

      rfb.addEventListener("connect", () => { setStatus(t("verbunden")); setConnected(true); });
      rfb.addEventListener("disconnect", (e: any) => {
        setConnected(false);
        setStatus(e?.detail?.clean ? t("getrennt") : t("Verbindung verloren"));
      });
      rfb.addEventListener("credentialsrequired", () => rfb.sendCredentials({ password: start.password }));
      rfb.addEventListener("securityfailure", () => setStatus(t("Authentifizierung fehlgeschlagen")));
      // Zwischenablage Gerät → Browser: in die lokale Zwischenablage schreiben.
      rfb.addEventListener("clipboard", (e: any) => {
        if (e?.detail?.text) navigator.clipboard?.writeText(e.detail.text).catch(() => {});
      });
    })();

    return () => {
      cancelled = true;
      try { rfb?.disconnect(); } catch { /* ignore */ }
      rfbRef.current = null;
    };
  }, [session, id]);

  return (
    <div className={fill ? "remote-fill" : "remote-panel"}>
      <div className="remote-bar">
        <span className={`badge ${connected ? "badge-online" : "badge-unknown"}`}>{status || t("getrennt")}</span>
        <div className="spacer" />
        <select value={monitor} title={t("Monitor")}
          onChange={(e) => { setMonitor(Number(e.target.value)); if (session > 0) setSession((n) => n + 1); }}>
          <option value={1}>{t("Primär")}</option>
          <option value={0}>{t("Alle Monitore")}</option>
          <option value={2}>{t("Monitor 2")}</option>
          <option value={3}>{t("Monitor 3")}</option>
        </select>
        {connected && (
          <>
            <button className="btn ghost sm" onClick={() => rfbRef.current?.sendCtrlAltDel()}>Ctrl+Alt+Entf</button>
            <button className="btn ghost sm" title={t("Lokale Zwischenablage zum Gerät senden")}
              onClick={() => navigator.clipboard?.readText().then((tx) => rfbRef.current?.clipboardPasteFrom(tx)).catch(() => {})}>
              📋 → {t("Gerät")}
            </button>
            <button className="btn ghost sm" onClick={() => setSession((n) => n + 1)}>{t("Neu verbinden")}</button>
            <button className="btn ghost sm" onClick={() => { try { rfbRef.current?.disconnect(); } catch { /* */ } setSession(0); }}>{t("Trennen")}</button>
          </>
        )}
        {!connected && <button className="btn primary sm" onClick={() => setSession((n) => n + 1)}>{t("Verbinden")}</button>}
        {!fill && <button className="btn ghost sm" title={t("In eigenem Fenster öffnen")} onClick={popout}>⧉</button>}
      </div>
      <div ref={hostRef} className="remote-screen" />
      {consent && (
        <p className="muted small">
          {t("Zustimmung")}:{" "}
          <select value={consent.device} onChange={(e) => setConsentMode(e.target.value)}>
            <option value="">{t("Erben")} ({consent.effective === "prompt" ? t("nachfragen") : t("unbeaufsichtigt")})</option>
            <option value="unattended">{t("unbeaufsichtigt")}</option>
            <option value="prompt">{t("nachfragen")}</option>
          </select>{" "}
          {t("— nachfragen verlangt eine Bestätigung am Gerät (für Nutzer-PCs), unbeaufsichtigt für Server.")}
        </p>
      )}
      <p className="muted small">{t("Startet on-demand einen VNC-Server am Gerät (nur während der Sitzung, nur lokal). Bei Nutzer-PCs muss die Verbindung ggf. am Gerät bestätigt werden.")}</p>
    </div>
  );
}
