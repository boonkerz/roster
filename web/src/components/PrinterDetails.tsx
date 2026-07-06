import { useI18n } from "../i18n";
import type { PrinterInfo } from "../types";

// PrinterDetails rendert per SNMP ausgelesene Druckerdaten (Kennwerte + Toner-Balken).
export function PrinterDetails({ info }: { info: PrinterInfo }) {
  const { t } = useI18n();
  return (
    <>
      <div className="kv-grid">
        <div><span className="muted">{t("Modell")}</span><div>{info.model || info.description || "—"}</div></div>
        <div><span className="muted">{t("Seriennummer")}</span><div className="mono">{info.serial || "—"}</div></div>
        <div><span className="muted">Firmware</span><div>{info.firmware || <span className="muted" title={t("Firmware ist meist Teil der Beschreibung; ein „Update verfügbar“ liefert kein Standard-SNMP.")}>{t("in Beschreibung")}</span>}</div></div>
        <div><span className="muted">{t("Status")}</span><div>{info.status || "—"}</div></div>
        <div><span className="muted">{t("Seitenzähler")}</span><div>{info.page_count ? info.page_count.toLocaleString() : "—"}</div></div>
        <div><span className="muted">{t("Beschreibung")}</span><div className="small">{info.description || "—"}</div></div>
      </div>
      {info.supplies && info.supplies.length > 0 && (
        <div style={{ marginTop: 10 }}>
          <div className="muted small" style={{ marginBottom: 4 }}>{t("Verbrauchsmaterial")}</div>
          {info.supplies.map((s, i) => {
            const pct = s.max > 0 && s.level >= 0 ? Math.round((s.level / s.max) * 100) : -1;
            return (
              <div key={i} className="supply-row">
                <span className="supply-name">{s.name || `#${i + 1}`}</span>
                <span className="supply-bar"><span style={{ width: `${pct < 0 ? 0 : pct}%` }} /></span>
                <span className="supply-pct muted small">{pct < 0 ? "?" : `${pct}%`}</span>
              </div>
            );
          })}
        </div>
      )}
    </>
  );
}
