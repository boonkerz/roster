import { useI18n } from "../i18n";
import type { DevColumn } from "../deviceColumns";
import { CF_PREFIX, DEFAULT_COLUMNS } from "../deviceColumns";

// ColumnsDialog wählt die Spalten der Geräteliste: Stammdaten und benutzerdefinierte
// Felder (z. B. ein Feld, das ein Check mit einer Docker-Image-Version füllt).
export function ColumnsDialog({ all, value, onChange, onClose }: {
  all: DevColumn[]; value: string[]; onChange: (keys: string[]) => void; onClose: () => void;
}) {
  const { t } = useI18n();
  const on = new Set(value);
  const base = all.filter((c) => !c.key.startsWith(CF_PREFIX));
  const custom = all.filter((c) => c.key.startsWith(CF_PREFIX));

  // Reihenfolge: Stammdaten in Registerreihenfolge, danach die Felder.
  const apply = (next: Set<string>) => onChange(all.filter((c) => next.has(c.key) || c.locked).map((c) => c.key));
  const toggle = (key: string) => { const n = new Set(on); if (n.has(key)) n.delete(key); else n.add(key); apply(n); };
  const setGroup = (cols: DevColumn[], add: boolean) => { const n = new Set(on); cols.forEach((c) => (add ? n.add(c.key) : n.delete(c.key))); apply(n); };

  const group = (title: string, cols: DevColumn[]) => (
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
          <label key={c.key} className={`chip ${on.has(c.key) || c.locked ? "chip-on" : ""}`} style={c.locked ? { opacity: 0.7 } : undefined}>
            <input type="checkbox" checked={on.has(c.key) || !!c.locked} disabled={c.locked} onChange={() => toggle(c.key)} />
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
          <h2>{t("Spalten")}</h2>
          <button className="modal-close" onClick={onClose} aria-label={t("Schließen")}>×</button>
        </header>
        <p className="muted">{t("Welche Spalten die Geräteliste zeigt. Die Auswahl wird in deinem Benutzerkonto gespeichert und gilt in jedem Browser.")}</p>

        {group(t("Gerätedaten"), base)}
        {custom.length > 0
          ? group(t("Benutzerdefinierte Felder"), custom)
          : <div className="muted small" style={{ marginTop: 14 }}>{t("Keine benutzerdefinierten Felder für Geräte angelegt.")}</div>}

        <div style={{ display: "flex", justifyContent: "space-between", gap: 10, marginTop: 20 }}>
          <button className="btn ghost" onClick={() => onChange(DEFAULT_COLUMNS)}>{t("Standard wiederherstellen")}</button>
          <button className="btn primary" onClick={onClose}>{t("Fertig")}</button>
        </div>
      </div>
    </div>
  );
}
