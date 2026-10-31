import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { pollCmd } from "../pollCmd";
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


// FileBrowser: Verzeichnisse durchsuchen, Dateien herunterladen/hochladen (≤ 32 MB).
export function FileBrowser({ deviceId }: { deviceId: string }) {
  const { t } = useI18n();
  const [listing, setListing] = useState<FileListing | null>(null);
  const [path, setPath] = useState(""); // "" = Wurzel/Laufwerke
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const uploadRef = useRef<HTMLInputElement>(null);

  const browse = async (p: string) => {
    setLoading(true); setError("");
    try {
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${deviceId}/browse`, { path: p });
      const cmd = await pollCmd(command_id);
      const data = JSON.parse(cmd.output || "{}") as FileListing;
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
      const { command_id } = await api.post<{ command_id: string }>(`/devices/${deviceId}/read-file`, { path: f.path });
      const cmd = await pollCmd(command_id);
      if (cmd.exit_code !== 0) throw new Error(cmd.output || t("Lesen fehlgeschlagen"));
      // Datei liegt jetzt beim Server bereit -> per Browser-Navigation abholen.
      window.location.href = `/api/v1/devices/${deviceId}/file/${command_id}`;
    } catch (e) { setError((e as Error).message); } finally { setBusy(""); }
  };

  const upload = async (file: File) => {
    if (!listing || listing.path === "") { setError(t("Bitte zuerst in ein Verzeichnis wechseln.")); return; }
    setBusy("upload"); setError("");
    try {
      const sep = listing.path.includes("\\") ? "\\" : "/";
      const target = listing.path.replace(/[/\\]$/, "") + sep + file.name;
      const res = await fetch(`/api/v1/devices/${deviceId}/write-file?path=${encodeURIComponent(target)}`, {
        method: "POST", credentials: "include", body: file,
      });
      if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || t("Upload fehlgeschlagen"));
      const { command_id } = await res.json();
      const cmd = await pollCmd(command_id);
      if (cmd.exit_code !== 0) throw new Error(cmd.output || t("Schreiben fehlgeschlagen"));
      await browse(listing.path);
    } catch (e) { setError((e as Error).message); } finally { setBusy(""); if (uploadRef.current) uploadRef.current.value = ""; }
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
                    <td>{!f.dir && f.size <= 32 * 1024 * 1024 && (
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
      <p className="muted small" style={{ marginTop: 8 }}>{t("Datei-Transfer bis 32 MB. Läuft mit den Rechten des Agent-Dienstes.")}</p>
    </section>
  );
}
