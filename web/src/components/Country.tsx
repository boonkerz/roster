import { useI18n } from "../i18n";

// Länder-Anzeige für Quell-IPs (ISO-3166-Code vom Server, Name über Intl in der UI-Sprache).

export function countryFlag(cc?: string): string {
  if (!cc || cc.length !== 2) return "🌐";
  return String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65));
}

const cache: Record<string, Intl.DisplayNames | null> = {};
export function countryName(cc: string | undefined, lang: string, unknown: string): string {
  if (!cc) return unknown;
  try {
    if (!(lang in cache)) cache[lang] = typeof Intl.DisplayNames === "function" ? new Intl.DisplayNames([lang], { type: "region" }) : null;
    return cache[lang]?.of(cc.toUpperCase()) ?? cc;
  } catch { return cc; }
}

// Flag zeigt die Flagge mit Ländernamen als Tooltip (nichts bei unbekanntem Land).
export function Flag({ cc }: { cc?: string }) {
  const { t, lang } = useI18n();
  if (!cc) return null;
  return <span title={countryName(cc, lang, t("Unbekannt"))} style={{ marginRight: 4 }}>{countryFlag(cc)}</span>;
}

// CountryChips listet Länder mit Flagge, Name und Zähler.
export function CountryChips({ items, max = 8, onClick }: { items: { country: string; ips: number; count: number }[]; max?: number; onClick?: (cc: string) => void }) {
  const { t, lang } = useI18n();
  if (items.length === 0) return <span className="muted">—</span>;
  return <>{items.slice(0, max).map((c) => (
    <span key={c.country || "?"} className="badge badge-unknown" style={{ marginRight: 6, marginBottom: 4, cursor: onClick ? "pointer" : undefined }} title={t("{n} IPs · {m} Treffer", { n: c.ips, m: c.count })} onClick={onClick ? () => onClick(c.country) : undefined}>
      {countryFlag(c.country)} {countryName(c.country, lang, t("Unbekannt"))} · {c.ips}
    </span>
  ))}</>;
}
