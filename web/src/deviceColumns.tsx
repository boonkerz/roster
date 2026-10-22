import type { ReactNode } from "react";
import type { Device, CustomField } from "./types";
import { StatusBadge, UpdatesBadge, HealthBadge, TaskHealthBadge, relTime } from "./components/StatusBadge";
import { CopyText } from "./components/CopyText";
import { primaryIPv4, formatBytes } from "./devutil";
import { fieldDisplay } from "./export";

// Spalten der Geräteliste: feste Stammdaten-Spalten plus eine je benutzerdefiniertem
// Feld ("cf:<feld-id>"). Welche sichtbar sind, steht in der Benutzereinstellung
// devices.columns (Reihenfolge = Reihenfolge der Schlüssel).

type Tr = (key: string, vars?: Record<string, string | number>) => string;

export interface ColumnCtx {
  t: Tr;
  jump: (d: Device, tab: string) => void;
  fieldValue: (deviceID: string, fieldID: string) => string | undefined;
}

export interface DevColumn {
  key: string;
  label: string;
  render: (d: Device, ctx: ColumnCtx) => ReactNode;
  locked?: boolean; // Hostname ist immer dabei
}

export const CF_PREFIX = "cf:";

export const DEFAULT_COLUMNS = ["status", "hostname", "users", "os", "ip", "cpu", "ram", "checks", "tasks", "cve", "updates", "agent", "last_seen"];

const dash = <span className="muted">—</span>;

export function baseDeviceColumns(t: Tr): DevColumn[] {
  const jumpCell = (d: Device, ctx: ColumnCtx, tab: string, title: string, body: ReactNode) => (
    <td style={{ cursor: "pointer" }} title={title} onClick={(e) => { e.stopPropagation(); ctx.jump(d, tab); }}>{body}</td>
  );
  return [
    { key: "status", label: t("Status"), render: (d) => <td><StatusBadge status={d.status} /></td> },
    {
      key: "hostname", label: "Hostname", locked: true, render: (d, ctx) => (
        <td>
          <span className="link-strong">{d.hostname || ctx.t("(unbenannt)")}</span>
          {d.revoked && <span className="badge badge-offline" style={{ marginLeft: 8 }}>{ctx.t("widerrufen")}</span>}
          {d.site_id && <div className="muted small">{d.client_name} › {d.site_name}</div>}
        </td>
      ),
    },
    { key: "client", label: t("Firma"), render: (d) => <td className="muted">{d.client_name || "—"}</td> },
    { key: "site", label: t("Standort"), render: (d) => <td className="muted">{d.site_name || "—"}</td> },
    { key: "users", label: t("Benutzer"), render: (d) => <td className="muted">{d.logged_in_users?.join(", ") || "—"}</td> },
    { key: "os", label: t("Betriebssystem"), render: (d) => <td>{d.os} {d.os_version}</td> },
    { key: "vendor", label: t("Hersteller"), render: (d) => <td className="muted">{d.vendor || "—"}</td> },
    { key: "model", label: t("Modell"), render: (d) => <td className="muted">{d.model || "—"}</td> },
    { key: "serial", label: t("Seriennummer"), render: (d) => <td className="mono muted">{d.serial ? <CopyText value={d.serial} /> : "—"}</td> },
    { key: "ip", label: t("IP-Adresse"), render: (d) => <td className="mono"><CopyText value={primaryIPv4(d)} /></td> },
    { key: "public_ip", label: t("Öffentliche IP"), render: (d) => <td className="mono muted">{d.public_ip ? <CopyText value={d.public_ip} /> : "—"}</td> },
    { key: "mac", label: "MAC", render: (d) => <td className="mono muted">{(d.interfaces ?? []).map((i) => i.mac).filter(Boolean).join(", ") || "—"}</td> },
    { key: "cpu", label: "CPU", render: (d, ctx) => <td className="muted">{d.cpu_cores ? ctx.t("{n} Kerne", { n: d.cpu_cores }) : "—"}</td> },
    { key: "ram", label: "RAM", render: (d) => <td className="muted">{formatBytes(d.memory_bytes)}</td> },
    { key: "checks", label: t("Checks"), render: (d, ctx) => jumpCell(d, ctx, "checks", ctx.t("Zu den Checks springen"), <HealthBadge total={d.checks_total} failing={d.checks_failing} />) },
    { key: "tasks", label: t("Tasks"), render: (d, ctx) => jumpCell(d, ctx, "tasks", ctx.t("Zu den Tasks springen"), <TaskHealthBadge total={d.tasks_total} failing={d.tasks_failing} />) },
    { key: "cve", label: t("CVE"), render: (d, ctx) => jumpCell(d, ctx, "vulns", ctx.t("Zu den Schwachstellen springen"), d.vuln_count ? <span className="badge badge-offline">{d.vuln_count}</span> : dash) },
    { key: "updates", label: t("Updates"), render: (d) => <td><UpdatesBadge count={d.updates_count} /></td> },
    { key: "agent", label: t("Agent"), render: (d) => <td className="muted mono">{d.agent_version || "—"}</td> },
    { key: "last_seen", label: t("Zuletzt gesehen"), render: (d) => <td className="muted">{relTime(d.last_seen)}</td> },
  ];
}

// fieldDeviceColumns: eine Spalte je benutzerdefiniertem Feld (Definitionen aus
// GET /custom-fields?model=device), alphabetisch.
export function fieldDeviceColumns(fields: CustomField[], t: Tr): DevColumn[] {
  return [...fields]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map((f) => ({
      key: CF_PREFIX + f.id,
      label: f.name,
      render: (d, ctx) => {
        const raw = ctx.fieldValue(d.id, f.id);
        if (raw == null || raw === "") return <td>{dash}</td>;
        const text = fieldDisplay(f.type, raw, t);
        const link = f.link && /^https?:\/\//.test(raw);
        return (
          <td className={f.type === "number" || f.type === "datetime" ? "mono" : ""} title={text}>
            {link ? <a href={raw} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}>{text}</a> : text}
          </td>
        );
      },
    }));
}

// resolveColumns ordnet die gespeicherten Schlüssel den Spalten zu; unbekannte
// Schlüssel (gelöschte Felder) fallen weg, Hostname bleibt immer dabei.
export function resolveColumns(keys: string[], all: DevColumn[]): DevColumn[] {
  const byKey = new Map(all.map((c) => [c.key, c]));
  const out = keys.map((k) => byKey.get(k)).filter((c): c is DevColumn => !!c);
  if (!out.some((c) => c.key === "hostname")) {
    const h = byKey.get("hostname");
    if (h) out.splice(Math.min(1, out.length), 0, h);
  }
  return out;
}
