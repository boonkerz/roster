import { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { FlowParam } from "../types";
import { DiffView } from "./FlowResultView";

interface OpResult { line: number; op: string; path: string; message: string; changed: boolean; error?: string }
interface PreviewResp { result: string; diff: string; results: OpResult[]; changes: number; error?: string; warnings?: string[] }

const STORE_KEY = "roster-yaml-playground";
const SAMPLE = `# Beispiel – eigene Compose-Datei hier einfügen
services:
  web:
    image: registry.example.com/shop/web-prod:2.4.0
  php-fpm:
    image: "registry.example.com/shop/php-fpm-prod:2.4.0"
    volumes:
      - ./data:/var/www/data
  cron:
    image: registry.example.com/shop/php-cron-prod:2.4.0
`;

function loadInput(): string {
  try { return localStorage.getItem(STORE_KEY) || SAMPLE; } catch { return SAMPLE; }
}

// YamlPlayground: links Eingabe-YAML (+ Parameter), Mitte die Operationen des Bausteins,
// rechts die Vorschau – gerechnet vom Server mit demselben Code wie im Agent.
export function YamlPlayground({ ops, onOpsChange, params }: { ops: string; onOpsChange: (v: string) => void; params: FlowParam[] }) {
  const { t } = useI18n();
  const [input, setInput] = useState(loadInput);
  const [vals, setVals] = useState<Record<string, string>>(() => Object.fromEntries(params.map((p) => [p.name, p.default ?? ""])));
  const [resp, setResp] = useState<PreviewResp | null>(null);
  const [view, setView] = useState<"diff" | "result">("diff");
  const [busy, setBusy] = useState(false);

  // Parameter, die in den Operationen vorkommen (auch solche ohne Definition im Ablauf).
  const used = useMemo(() => [...new Set([...ops.matchAll(/\{\{\s*param\.([A-Za-z0-9_]+)\s*\}\}/g)].map((m) => m[1]))], [ops]);

  useEffect(() => { try { localStorage.setItem(STORE_KEY, input); } catch { /* egal */ } }, [input]);
  useEffect(() => {
    const h = setTimeout(async () => {
      setBusy(true);
      try { setResp(await api.post<PreviewResp>("/flows/yaml-preview", { yaml: input, ops, params: vals })); }
      catch (e) { setResp({ result: "", diff: "", results: [], changes: 0, error: (e as Error).message }); }
      finally { setBusy(false); }
    }, 350);
    return () => clearTimeout(h);
  }, [input, ops, vals]);

  return (
    <div className="yaml-play">
      <div className="yaml-play-head">
        <h2 style={{ margin: 0 }}>{t("YAML ändern – ausprobieren")}</h2>
        <span className="muted small">{t("Nichts wird gespeichert oder ausgeführt; die Operationen werden direkt im Baustein übernommen.")}</span>
      </div>
      <div className="yaml-play-cols">
        <section>
          <div className="yaml-play-title">{t("Eingabe (YAML)")}
            <button className="btn ghost sm" onClick={() => setInput(SAMPLE)}>{t("Beispiel")}</button>
          </div>
          <textarea className="mono yaml-play-text" spellCheck={false} value={input} onChange={(e) => setInput(e.target.value)} />
          {used.length > 0 && (
            <div className="yaml-play-params">
              <div className="muted small">{t("Parameter für die Vorschau")}</div>
              {used.map((n) => (
                <label key={n} className="flow-field"><span className="muted small mono">{`{{param.${n}}}`}</span>
                  <input className="mono" value={vals[n] ?? ""} onChange={(e) => setVals({ ...vals, [n]: e.target.value })} /></label>
              ))}
            </div>
          )}
        </section>
        <section>
          <div className="yaml-play-title">{t("Operationen")}</div>
          <textarea className="mono yaml-play-text" spellCheck={false} value={ops} onChange={(e) => onOpsChange(e.target.value)} />
          <div className="yaml-play-ops">
            {(resp?.results ?? []).map((r, i) => (
              <div key={i} className={`small ${r.error ? "error" : ""}`}>
                {r.error ? "✗" : r.changed ? "✓" : "·"} <span className="muted">Z.{r.line}</span> <span className="mono">{r.op} {r.path}</span> – {r.error ?? r.message}
              </div>
            ))}
            <details className="small" style={{ marginTop: 6 }}>
              <summary className="muted">{t("Syntax")}</summary>
              <pre className="flow-output">{`set    pfad = wert          # ersetzt bzw. legt an
append pfad = eintrag       # Listeneintrag, nur wenn neu
ensure pfad = wert          # nur wenn der Schlüssel fehlt
delete pfad [= eintrag]     # Schlüssel bzw. Listeneintrag
services.{web,cron}.image   # mehrere Einträge
{{value.repo}} {{value.tag}} {{value}}   # aktueller Wert
"a.b"                       # Punkt im Schlüsselnamen
ensure volumes.media = {driver: local, driver_opts: {type: none, o: bind}}
                            # {…}/[…] wird als Block geschrieben`}</pre>
            </details>
          </div>
        </section>
        <section>
          <div className="yaml-play-title">{t("Vorschau")} {busy && <span className="muted small">…</span>}
            <span className="inline-form" style={{ gap: 4 }}>
              <button className={`btn sm ${view === "diff" ? "primary" : "ghost"}`} onClick={() => setView("diff")}>Diff</button>
              <button className={`btn sm ${view === "result" ? "primary" : "ghost"}`} onClick={() => setView("result")}>{t("Ergebnis")}</button>
            </span>
          </div>
          {resp?.error && <p className="error small">{resp.error}</p>}
          {(resp?.warnings ?? []).map((w, i) => <p key={i} className="small" style={{ color: "var(--amber)", margin: "2px 0" }}>⚠ {w}</p>)}
          {resp && !resp.error && <p className="muted small" style={{ margin: "2px 0 6px" }}>{t("{n} Änderung(en)", { n: resp.changes })}{resp.changes === 0 ? " – " + t("im Lauf endet der Ablauf hier (nichts zu tun)") : ""}</p>}
          <div className="yaml-play-out">
            {view === "diff"
              ? (resp?.diff ? <DiffView diff={resp.diff} /> : <p className="muted small">{t("Keine Änderung.")}</p>)
              : <pre className="flow-output mono">{resp?.result ?? ""}</pre>}
          </div>
        </section>
      </div>
    </div>
  );
}
