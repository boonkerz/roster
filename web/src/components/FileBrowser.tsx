import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../api";
import { pollCmd } from "../pollCmd";
import { directFiles, LegacyError, MAX_TRANSFER, pollTransfer, uploadDirect, uploadFile } from "../transfer";
import { useI18n } from "../i18n";

interface FileEntry { name: string; path: string; dir: boolean; size: number; modified: number; }
interface FileListing { path: string; parent: string; entries: FileEntry[]; error?: string; }


function fmtBytes(n: number): string {
  if (!n) return "0";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0, v = n;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${u[i]}`;
}


// FileBrowser: Verzeichnisse durchsuchen, Dateien herunterladen/hochladen (≤ 4 GB).
export function FileBrowser({ deviceId, agentVersion }: { deviceId: string; agentVersion?: string }) {
  const { t } = useI18n();
  const [listing, setListing] = useState<FileListing | null>(null);
  const [path, setPath] = useState(""); // "" = Wurzel/Laufwerke
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState("");
  const [progress, setProgress] = useState("");
  const [error, setError] = useState("");
  const uploadRef = useRef<HTMLInputElement>(null);
  // Direkter Weg (Agent ≥ 0.29.0): Server reicht Dateien durch, kein Befehl je Datei.
  const [direct, setDirect] = useState(directFiles(agentVersion));
  const isLegacy = (e: unknown) => e instanceof LegacyError || (e instanceof ApiError && e.status === 409 && e.message === "legacy");

  const browse = async (p: string) => {
    setLoading(true); setError("");
    try {
      let data: FileListing | null = null;
      if (direct) {
        try { data = await api.get<FileListing>(`/devices/${deviceId}/fs/list?path=${encodeURIComponent(p)}`); }
        catch (e) { if (!isLegacy(e)) throw e; setDirect(false); }
      }
      if (!data) {
        const { command_id } = await api.post<{ command_id: string }>(`/devices/${deviceId}/browse`, { path: p });
        const cmd = await pollCmd(command_id);
        data = JSON.parse(cmd.output || "{}") as FileListing;
      }
      if (data.error) setError(data.error);
      setListing(data);
      setPath(p);
    } catch (e) { setError((e as Error).message); } finally { setLoading(false); }
  };

  useEffect(() => { void browse(""); /* initial: Wurzel */ // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const download = async (f: FileEntry) => {
    setBusy(f.path); setError("");
    try {
      if (direct) {
        try {
          // Erst prüfen (Fehler sichtbar machen), dann der Browser-Download direkt vom Gerät.
          const st = await api.get<{ exists: boolean; dir: boolean }>(`/devices/${deviceId}/fs/stat?path=${encodeURIComponent(f.path)}`);
          if (!st.exists || st.dir) throw new Error(t("Datei nicht gefunden"));
          const a = document.createElement("a");
          a.href = `/api/v1/devices/${deviceId}/fs/download?path=${encodeURIComponent(f.path)}`;
          a.download = f.name;
          document.body.appendChild(a); a.click(); a.remove();
          return;
        } catch (e) { if (!isLegacy(e)) throw e; setDirect(false); }
      }
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${deviceId}/read-file`, { path: f.path });
      setProgress(t("Gerät bereitet {name} vor…", { name: f.name }));
      const cmd = await pollTransfer(command_id, setProgress);
      if (cmd.exit_code !== 0) throw new Error(cmd.output || t("Lesen fehlgeschlagen"));
      // Datei liegt jetzt beim Server bereit -> per Browser-Navigation abholen.
      window.location.href = `/api/v1/devices/${deviceId}/file/${command_id}`;
    } catch (e) { setError((e as Error).message); } finally { setBusy(""); setProgress(""); }
  };

  const upload = async (file: File) => {
    if (!listing || listing.path === "") { setError(t("Bitte zuerst in ein Verzeichnis wechseln.")); return; }
    if (file.size > MAX_TRANSFER) { setError(t("Datei zu groß (max 4 GB)")); return; }
    setBusy("upload"); setError("");
    try {
      const sep = listing.path.includes("\\") ? "\\" : "/";
      const target = listing.path.replace(/[/\\]$/, "") + sep + file.name;
      if (direct) {
        try {
          await uploadDirect(deviceId, target, file, (pct) => setProgress(t("Hochladen: {pct} %", { pct }) + ` – ${file.name}`));
          await browse(listing.path);
          return;
        } catch (e) { if (!isLegacy(e)) throw e; setDirect(false); }
      }
      const { command_id } = await uploadFile(`/api/v1/devices/${deviceId}/write-file?path=${encodeURIComponent(target)}`, file,
        (pct) => setProgress(t("Hochladen zum Server: {pct} %", { pct })));
      setProgress(t("Gerät übernimmt {name}…", { name: file.name }));
      const cmd = await pollTransfer(command_id, setProgress);
      if (cmd.exit_code !== 0) throw new Error(cmd.output || t("Schreiben fehlgeschlagen"));
      await browse(listing.path);
    } catch (e) { setError((e as Error).message); } finally { setBusy(""); setProgress(""); if (uploadRef.current) uploadRef.current.value = ""; }
  };

  return (
    <section className="card">
      <div className="page-head" style={{ marginBottom: 10 }}>
        <div className="inline-form">
          {listing && listing.path !== "" && (
            <button className="btn ghost sm" disabled={loading} onClick={() => browse(listing.parent)}>⬆ {t("Übergeordnet")}</button>
          )}
          <span className="mono small muted">{path || t("Laufwerke")}</span>
        </div>
        <div className="inline-form">
          <button className="btn ghost sm" disabled={loading} onClick={() => browse(path)}>↻</button>
          {listing && listing.path !== "" && (
            <>
              <button className="btn ghost sm" disabled={!!busy} onClick={() => uploadRef.current?.click()}>⬆ {t("Hochladen")}</button>
              <input ref={uploadRef} type="file" style={{ display: "none" }}
                onChange={(e) => { const f = e.target.files?.[0]; if (f) void upload(f); }} />
            </>
          )}
        </div>
      </div>

      {progress && <p className="muted small">⏳ {progress}</p>}
      {loading ? <p className="muted">{t("Wird abgefragt… (Agent muss online sein)")}</p>
        : error ? <p className="muted">{t("Fehler")}: {error}</p>
        : (
          <div className="scroll-list">
            <table className="table">
              <thead><tr><th>{t("Name")}</th><th>{t("Größe")}</th><th>{t("Geändert")}</th><th></th></tr></thead>
              <tbody>
                {(listing?.entries ?? []).map((f) => (
                  <tr key={f.path}>
                    <td>
                      {f.dir
                        ? <button className="btn-link" onClick={() => browse(f.path)}>📁 {f.name}</button>
                        : <span>📄 {f.name}</span>}
                    </td>
                    <td className="muted mono small">{f.dir ? "—" : fmtBytes(f.size)}</td>
                    <td className="muted small">{f.modified ? new Date(f.modified * 1000).toLocaleString() : "—"}</td>
                    <td>{!f.dir && f.size <= MAX_TRANSFER && (
                      <button className="btn ghost sm" disabled={!!busy} onClick={() => download(f)}>
                        {busy === f.path ? "…" : "⬇ " + t("Download")}
                      </button>
                    )}</td>
                  </tr>
                ))}
                {(listing?.entries ?? []).length === 0 && <tr><td colSpan={4} className="empty">{t("Leeres Verzeichnis.")}</td></tr>}
              </tbody>
            </table>
          </div>
        )}
      <p className="muted small" style={{ marginTop: 8 }}>{t("Datei-Transfer bis 4 GB. Läuft mit den Rechten des Agent-Dienstes.")}</p>
    </section>
  );
}
