import { useEffect, useRef, useState } from "react";
import RFB from "@novnc/novnc";
import { useI18n } from "../i18n";
import { api } from "../api";
import { useAuth } from "../auth";
import type { Command } from "../types";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
async function pollCmd(id: string): Promise<Command> {
  for (let i = 0; i < 90; i++) {
    await sleep(700);
    const cmd = await api.get<Command>(`/commands/${id}`);
    if (cmd.status === "done") return cmd;
  }
  throw new Error("timeout");
}

// DeviceRemote öffnet eine Web-Fernsteuerung (Remote Desktop) über noVNC. Der Server
// startet on-demand einen VNC-Server am Gerät und tunnelt die RFB-Bytes über eine
// WebSocket – gleiche Origin, daher Cookie-Auth. fill = füllt das Popout-Fenster;
// autoStart = sofort verbinden (Popout).
export function DeviceRemote({ id, os, fill, autoStart, initialMonitor }: {
  id: string; os: string; fill?: boolean; autoStart?: boolean; initialMonitor?: number;
}) {
  const { t } = useI18n();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const [status, setStatus] = useState("");
  const [connected, setConnected] = useState(false);
  const [monitor, setMonitor] = useState(initialMonitor ?? 1); // 1=primär, 0=alle, N=Monitor N
  const [session, setSession] = useState(autoStart ? 1 : 0); // hochzählen = (neu) verbinden
  const hostRef = useRef<HTMLDivElement>(null);
  const fillRef = useRef<HTMLDivElement>(null);
  const rfbRef = useRef<any>(null);
  const [fullscreen, setFullscreen] = useState(false);
  // Keyboard Lock (Chromium, HTTPS, nur Vollbild) fängt sonst vom Browser/OS
  // abgefangene Systemtasten ab (Win, Alt+Tab, Esc, F-Tasten, Ctrl+W …).
  const kbLockSupported = typeof navigator !== "undefined" && !!(navigator as any).keyboard?.lock;

  // Vollbild + Keyboard-Lock umschalten. Im Vollbild erreichen dann fast alle
  // Tastenkombinationen das entfernte Gerät statt den Browser.
  const toggleFullscreen = async () => {
    try {
      if (!document.fullscreenElement) {
        await fillRef.current?.requestFullscreen();
      } else {
        await document.exitFullscreen();
      }
    } catch { /* ignore */ }
  };
  useEffect(() => {
    const onFsChange = () => {
      const active = !!document.fullscreenElement;
      setFullscreen(active);
      const kb = (navigator as any).keyboard;
      if (active) {
        kb?.lock?.().catch(() => {});
        rfbRef.current?.focus?.();
      } else {
        kb?.unlock?.();
      }
    };
    document.addEventListener("fullscreenchange", onFsChange);
    return () => document.removeEventListener("fullscreenchange", onFsChange);
  }, []);

  // Einzelne Taste bzw. Kombination an das Gerät senden (RFB-Keysyms).
  const KEY = { win: [0xffeb, "MetaLeft"], alt: [0xffe9, "AltLeft"], tab: [0xff09, "Tab"], esc: [0xff1b, "Escape"] } as const;
  const tapKey = (k: readonly [number, string]) => rfbRef.current?.sendKey(k[0], k[1]);
  const sendAltTab = () => {
    const r = rfbRef.current;
    if (!r) return;
    r.sendKey(KEY.alt[0], KEY.alt[1], true);
    r.sendKey(KEY.tab[0], KEY.tab[1], true);
    r.sendKey(KEY.tab[0], KEY.tab[1], false);
    r.sendKey(KEY.alt[0], KEY.alt[1], false);
  };

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

  // Datei per Drag&Drop auf den Bildschirm zum Gerät übertragen (öffentlicher Desktop).
  const [dropStatus, setDropStatus] = useState("");
  const uploadDropped = async (file: File) => {
    const target = /win/i.test(os) ? `C:\\Users\\Public\\Desktop\\${file.name}` : `/tmp/${file.name}`;
    setDropStatus(t("Übertrage {name}…", { name: file.name }));
    try {
      const res = await fetch(`/api/v1/devices/${id}/write-file?path=${encodeURIComponent(target)}`,
        { method: "POST", credentials: "include", body: file });
      if (!res.ok) throw new Error();
      const { command_id } = await res.json();
      const cmd = await pollCmd(command_id);
      if (cmd.exit_code !== 0) throw new Error();
      setDropStatus(`✓ ${file.name} → ${target}`);
    } catch { setDropStatus(t("Übertragung fehlgeschlagen")); }
  };
  const onDrop = (e: React.DragEvent) => {
    e.preventDefault();
    const f = e.dataTransfer.files?.[0];
    if (f) void uploadDropped(f);
  };

  const popout = () => {
    window.open(`/devices/${id}/remote?os=${encodeURIComponent(os)}&monitor=${monitor}`, `remote-${id}`,
      "width=1280,height=800,menubar=no,toolbar=no,location=no,status=no");
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

      rfb.addEventListener("connect", () => { setStatus(t("verbunden")); setConnected(true); try { rfb.focus(); } catch { /* */ } });
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

  const consentBox = consent && (
    <p className="muted small">
      {t("Zustimmung")}:{" "}
      <select value={consent.device} onChange={(e) => setConsentMode(e.target.value)}>
        <option value="">{t("Erben")} ({consent.effective === "prompt" ? t("nachfragen") : t("unbeaufsichtigt")})</option>
        <option value="unattended">{t("unbeaufsichtigt")}</option>
        <option value="prompt">{t("nachfragen")}</option>
      </select>{" "}
      {t("— nachfragen verlangt eine Bestätigung am Gerät (für Nutzer-PCs), unbeaufsichtigt für Server.")}
    </p>
  );

  // Im Tab (nicht Popout): nur ein Starter – die Fernsteuerung läuft im eigenen Fenster.
  if (!fill) {
    return (
      <div className="remote-panel">
        <p className="muted">{t("Die Fernsteuerung öffnet sich in einem eigenen, großen Fenster.")}</p>
        <div className="inline-form">
          <label className="num">{t("Monitor")}
            <select value={monitor} onChange={(e) => setMonitor(Number(e.target.value))}>
              <option value={1}>{t("Primär")}</option>
              <option value={0}>{t("Alle Monitore")}</option>
              <option value={2}>{t("Monitor 2")}</option>
              <option value={3}>{t("Monitor 3")}</option>
            </select>
          </label>
          <button className="btn primary" onClick={popout}>⧉ {t("Fernsteuerung öffnen")}</button>
        </div>
        {consentBox}
        <p className="muted small">{t("Startet on-demand einen VNC-Server am Gerät (nur während der Sitzung, nur lokal). Bei Nutzer-PCs muss die Verbindung ggf. am Gerät bestätigt werden.")} {t("Datei per Drag&Drop auf den Bildschirm ziehen, um sie zum Gerät zu übertragen.")}</p>
      </div>
    );
  }

  return (
    <div className="remote-fill" ref={fillRef}>
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
            <button className={`btn sm ${fullscreen ? "primary" : "ghost"}`} onClick={toggleFullscreen}
              title={kbLockSupported
                ? t("Vollbild aktiviert die Tastatur-Erfassung – dann erreichen auch Win, Alt+Tab, Esc, F-Tasten das Gerät.")
                : t("Vollbild. Hinweis: Vollständige Tastatur-Erfassung (Win/Alt+Tab) unterstützt nur Chrome/Edge über HTTPS.")}>
              ⛶ {t("Vollbild")}
            </button>
            <button className="btn ghost sm" title={t("Windows-Taste")} onClick={() => tapKey(KEY.win)}>⊞ Win</button>
            <button className="btn ghost sm" onClick={sendAltTab}>Alt+Tab</button>
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
      </div>
      <div ref={hostRef} className="remote-screen" onDragOver={(e) => e.preventDefault()} onDrop={onDrop} />
      {connected && !fullscreen && (
        <p className="muted small">⌨️ {kbLockSupported
          ? t("Tipp: „Vollbild“ für vollständige Tastatur-Erfassung – sonst fängt der Browser Tasten wie Win, Alt+Tab, Esc oder F-Tasten ab.")
          : t("Tipp: Win/Alt+Tab-Tasten oben nutzen. Vollständige Tastatur-Erfassung gibt es nur in Chrome/Edge über HTTPS.")}</p>
      )}
      {dropStatus && <p className="muted small">📁 {dropStatus}</p>}
    </div>
  );
}
