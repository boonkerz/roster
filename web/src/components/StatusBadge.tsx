import type { Device } from "../types";
import { useI18n, gt } from "../i18n";

const LABEL: Record<Device["status"], string> = {
  online: "Online",
  offline: "Offline",
  unknown: "Unbekannt",
  unmanaged: "Nicht verwaltet",
};

export function StatusBadge({ status }: { status: Device["status"] }) {
  const { t } = useI18n();
  return (
    <span className={`badge badge-${status}`}>
      <span className="dot" /> {t(LABEL[status])}
    </span>
  );
}

// UpdatesBadge zeigt verfügbare OS-Updates: unbekannt (—), aktuell oder Anzahl; mit
// security wird der Anteil Sicherheits-Updates rot hervorgehoben.
export function UpdatesBadge({ count, security }: { count?: number | null; security?: number | null }) {
  if (count === undefined || count === null) return <span className="muted">—</span>;
  if (count === 0)
    return (
      <span className="badge badge-online">
        <span className="dot" /> {gt("Aktuell")}
      </span>
    );
  if (security && security > 0)
    return (
      <span className="badge badge-offline" title={gt("{n} davon Sicherheits-Updates", { n: security })}>
        {gt("{count} verfügbar", { count })} · 🛡 {security}
      </span>
    );
  return <span className="badge badge-warn">{gt("{count} verfügbar", { count })}</span>;
}

// AUTO_UPDATES_LABEL beschriftet den Auto-Update-Status des Agents.
export const AUTO_UPDATES_LABEL: Record<string, { label: string; cls: string }> = {
  enabled: { label: "automatisch (alle)", cls: "badge-online" },
  security: { label: "automatisch (Sicherheit)", cls: "badge-online" },
  download: { label: "nur herunterladen", cls: "badge-warn" },
  notify: { label: "nur benachrichtigen", cls: "badge-warn" },
  disabled: { label: "aus", cls: "badge-offline" },
};

export function AutoUpdatesBadge({ status }: { status?: string }) {
  if (!status) return <span className="muted">—</span>;
  const m = AUTO_UPDATES_LABEL[status] ?? { label: status, cls: "badge-unknown" };
  return <span className={`badge ${m.cls}`}>{gt(m.label)}</span>;
}

// KernelBadge zeigt den laufenden Kernel und warnt, wenn ein neuerer installiert ist.
export function KernelBadge({ running, installed }: { running?: string; installed?: string }) {
  if (!running) return <span className="muted">—</span>;
  const pending = !!installed && installed !== running;
  return (
    <span className={pending ? "badge badge-warn" : "mono small"} title={pending ? gt("Neuer Kernel installiert: {v} – Neustart erforderlich", { v: installed }) : undefined}>
      {running}{pending ? " ⚠" : ""}
    </span>
  );
}

// EolBadge zeigt das Support-Ende eines Geräts (ok / bald / erreicht / unbekannt).
export function EolBadge({ status, date }: { status?: string; date?: string }) {
  if (!status) return <span className="muted">—</span>;
  const d = date ? new Date(date).toLocaleDateString() : "";
  if (status === "eol") return <span className="badge badge-offline" title={gt("Support-Ende erreicht")}>EOL{d ? ` · ${d}` : ""}</span>;
  if (status === "soon") return <span className="badge badge-warn" title={gt("Support-Ende naht")}>⏳ {d || gt("bald")}</span>;
  if (status === "ok") return <span className="badge badge-online" title={gt("Im Support")}>{d || "OK"}</span>;
  return <span className="badge badge-unknown">{gt("unbekannt")}</span>;
}

// SecurityUpdatesBadge zeigt nur die ausstehenden Sicherheits-Updates.
export function SecurityUpdatesBadge({ count }: { count?: number | null }) {
  if (count === undefined || count === null) return <span className="muted">—</span>;
  if (count === 0) return <span className="badge badge-online"><span className="dot" /> 0</span>;
  return <span className="badge badge-offline">🛡 {count}</span>;
}

// HealthBadge zeigt den Policy-Check-Gesamtstatus eines Geräts.
export function HealthBadge({ total, failing }: { total?: number; failing?: number }) {
  if (!total) return <span className="muted">—</span>;
  if (failing && failing > 0) return <span className="badge badge-offline">{failing}/{total} ✗</span>;
  return (
    <span className="badge badge-online">
      <span className="dot" /> {total} OK
    </span>
  );
}

// TaskHealthBadge zeigt den Task-Gesamtstatus eines Geräts (letzter Lauf je Task).
export function TaskHealthBadge({ total, failing }: { total?: number; failing?: number }) {
  if (!total) return <span className="muted">—</span>;
  if (failing && failing > 0) return <span className="badge badge-offline">{failing}/{total} ✗</span>;
  return (
    <span className="badge badge-online">
      <span className="dot" /> {total} OK
    </span>
  );
}

// CheckStatusBadge zeigt den Status eines einzelnen Checks.
export function CheckStatusBadge({ status }: { status: string }) {
  const cls = status === "passing" ? "badge-online"
    : status === "failing" ? "badge-offline"
    : status === "warning" ? "badge-warn" : "badge-unknown";
  const label = status === "passing" ? "OK"
    : status === "failing" ? "Fehler"
    : status === "warning" ? "Warnung" : "Unbekannt";
  return <span className={`badge ${cls}`}><span className="dot" /> {gt(label)}</span>;
}

// SeverityBadge färbt den Schweregrad eines Patches; security hebt Sicherheits-Updates hervor.
export function SeverityBadge({ severity, security, phased }: { severity: string; security?: boolean; phased?: boolean }) {
  const s = (severity || "Other").toLowerCase();
  const cls = s === "critical" || security ? "badge-offline" : s === "important" ? "badge-warn" : "badge-unknown";
  return <>
    <span className={`badge ${cls}`} title={security ? gt("Sicherheits-Update") : undefined}>{security ? "🛡 " : ""}{severity || "Other"}</span>
    {phased && <span className="badge badge-unknown" style={{ marginLeft: 4 }} title={gt("Ubuntu rollt dieses Update stufenweise aus; apt stellt es bis zur Ausrollquote zurück, auch bei dist-upgrade. Option „Phased Updates sofort installieren“ nimmt es mit.")}>⏳ {gt("zurückgestellt (Phasing)")}</span>}
  </>;
}

// relTime formatiert einen ISO-Zeitpunkt als „vor 3 Min." (oder „—").
export function relTime(iso?: string): string {
  if (!iso) return "—";
  const diff = Date.now() - new Date(iso).getTime();
  const s = Math.floor(diff / 1000);
  if (s < 60) return gt("gerade eben");
  const m = Math.floor(s / 60);
  if (m < 60) return gt("vor {m} Min.", { m });
  const h = Math.floor(m / 60);
  if (h < 24) return gt("vor {h} Std.", { h });
  const d = Math.floor(h / 24);
  return gt("vor {d} Tg.", { d });
}
