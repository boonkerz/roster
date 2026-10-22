import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { Device } from "../types";
import { useI18n } from "../i18n";
import { usePref } from "../prefs";
import { baseColumns, fieldColumns, devicesCSV, downloadText, EntityFieldValue, ExportColumn } from "../export";


// ExportDialog lässt die Spalten des CSV-Exports wählen – Stammdaten und die
// benutzerdefinierten Felder, die bei den gewählten Geräten gesetzt sind.
export function ExportDialog({ devices, onClose, onDone }: { devices: Device[]; onClose: () => void; onDone: (n: number) => void }) {
  const { t } = useI18n();
  // Älterer Server ohne Sammelabruf -> Fehler -> Export ohne Feldspalten.
  const fields = useQuery({
    queryKey: ["custom-field-values", "device", "all"],
    queryFn: () => api.get<EntityFieldValue[]>("/custom-field-values?model=device"),
    retry: false,
    staleTime: 30_000,
  });
  const base = useMemo(() => baseColumns(t), [t]);
  const custom = useMemo(() => fieldColumns(devices, fields.data ?? [], t), [devices, fields.data, t]);
  // Gemerkte Spaltenauswahl (Benutzerkonto): Liste der abgewählten Schlüssel – neue
  // Spalten/Felder sind damit automatisch dabei, bis man sie bewusst abwählt.
  const [offList, setOffList] = usePref<string[]>("export.columns_off", [], "roster-export-cols");
  const off = useMemo(() => new Set(offList), [offList]);
  const setOff = (fn: (s: Set<string>) => Set<string>) => setOffList([...fn(off)]);

  const toggle = (key: string) => setOff((s) => { const n = new Set(s); if (n.has(key)) n.delete(key); else n.add(key); return n; });
  const setGroup = (cols: ExportColumn[], on: boolean) => setOff((s) => {
    const n = new Set(s);
    cols.forEach((c) => (on ? n.delete(c.key) : n.add(c.key)));
    return n;
  });
  const chosen = [...base, ...custom].filter((c) => !off.has(c.key));

  const run = () => {
    const stamp = new Date().toISOString().slice(0, 10);
    downloadText(`roster-geraete-${stamp}.csv`, devicesCSV(devices, chosen));
    onDone(devices.length);
  };

  const group = (title: string, cols: ExportColumn[]) => (
    <section style={{ marginTop: 14 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 8 }}>
        <strong>{title}</strong>
        <span className="muted small">
          <button className="link-btn" onClick={() => setGroup(cols, true)}>{t("alle")}</button>
          {" · "}
          <button className="link-btn" onClick={() => setGroup(cols, false)}>{t("keine")}</button>
        </span>
      </div>
      <div className="chips">
        {cols.map((c) => (
          <label key={c.key} className={`chip ${off.has(c.key) ? "" : "chip-on"}`}>
            <input type="checkbox" checked={!off.has(c.key)} onChange={() => toggle(c.key)} />
            {c.label}
          </label>
        ))}
      </div>
    </section>
  );

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h2>{t("Export (CSV)")}</h2>
          <button className="modal-close" onClick={onClose} aria-label={t("Schließen")}>×</button>
        </header>
        <p className="muted">
          {t("{n} Gerät(e) als Semikolon-CSV (UTF-8, Excel-kompatibel). Spalten abwählen, die nicht in die Datei sollen – die Auswahl wird gemerkt.", { n: devices.length })}
        </p>

        {group(t("Gerätedaten"), base)}
        {fields.isLoading && <div className="muted small" style={{ marginTop: 14 }}>{t("Lädt…")}</div>}
        {custom.length > 0 && group(t("Benutzerdefinierte Felder"), custom)}
        {!fields.isLoading && custom.length === 0 && (
          <div className="muted small" style={{ marginTop: 14 }}>
            {fields.isError ? t("Benutzerdefinierte Felder stehen auf diesem Server nicht für den Export bereit.") : t("Bei den gewählten Geräten sind keine benutzerdefinierten Felder gesetzt.")}
          </div>
        )}

        <div style={{ display: "flex", justifyContent: "flex-end", gap: 10, marginTop: 20 }}>
          <button className="btn ghost" onClick={onClose}>{t("Abbrechen")}</button>
          <button className="btn primary" disabled={chosen.length === 0} onClick={run}>
            ⤓ {t("{n} Spalten exportieren", { n: chosen.length })}
          </button>
        </div>
      </div>
    </div>
  );
}
