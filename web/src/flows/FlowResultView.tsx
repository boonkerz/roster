import { useI18n } from "../i18n";
import type { FlowResult } from "../types";
import { NODE_TYPE } from "./nodeTypes";

const STATUS_CLASS: Record<string, string> = {
  ok: "badge-online", dry: "badge-unknown", nothing: "badge-unknown", empty: "badge-unknown", skipped: "badge-unknown",
  fail: "badge-offline", rolled_back: "badge-warn", queued: "badge-unknown",
};
export const FLOW_STATUS: Record<string, string> = {
  ok: "erfolgreich", dry: "Vorschau", nothing: "nicht betroffen", empty: "keine Treffer", skipped: "übersprungen",
  fail: "fehlgeschlagen", rolled_back: "zurückgerollt", queued: "wartet auf Agent",
};

export function FlowStatusBadge({ status }: { status: string }) {
  const { t } = useI18n();
  return <span className={`badge ${STATUS_CLASS[status] ?? "badge-unknown"}`}>{t(FLOW_STATUS[status] ?? status)}</span>;
}

// DiffView färbt einen Unified-Diff zeilenweise.
export function DiffView({ diff }: { diff: string }) {
  return (
    <pre className="flow-diff">
      {diff.split("\n").map((l, i) => (
        <span key={i} className={l.startsWith("+++") || l.startsWith("---") ? "d-file" : l.startsWith("@@") ? "d-hunk" : l.startsWith("+") ? "d-add" : l.startsWith("-") ? "d-del" : ""}>{l + "\n"}</span>
      ))}
    </pre>
  );
}

// FlowResultView zeigt das Ergebnis eines Geräts: Status, Fehler, Bausteine mit Ausgabe/Diff.
export function FlowResultView({ result, hideStatus }: { result: FlowResult; hideStatus?: boolean }) {
  const { t } = useI18n();
  return (
    <div className="flow-result">
      <div className="inline-form" style={{ gap: 8, marginBottom: 8 }}>
        {!hideStatus && <FlowStatusBadge status={result.status} />}
        {result.error && <span className="error small">{result.error}</span>}
      </div>
      {(result.rolled_back ?? []).length > 0 && (
        <p className="small"><strong>{t("Wiederhergestellt")}:</strong> <span className="mono">{result.rolled_back!.join(", ")}</span></p>
      )}
      <ol className="flow-steps">
        {(result.nodes ?? []).map((n, i) => (
          <li key={i}>
            <div className="inline-form" style={{ gap: 8 }}>
              <span>{NODE_TYPE[n.type]?.icon}</span>
              <strong>{n.label || t(NODE_TYPE[n.type]?.label ?? n.type)}</strong>
              <FlowStatusBadge status={n.status} />
              <span className="muted small">{(n.duration_ms / 1000).toFixed(1)} s</span>
            </div>
            {n.output && <pre className="flow-output">{n.output}</pre>}
            {n.diff && <DiffView diff={n.diff} />}
          </li>
        ))}
      </ol>
    </div>
  );
}
