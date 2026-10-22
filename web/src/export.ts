import type { Device } from "./types";
import { primaryIPv4, formatBytes } from "./devutil";

// Feldwert eines Geräts aus dem Sammelabruf GET /custom-field-values?model=device.
export interface EntityFieldValue {
  entity_id: string;
  field: { id: string; name: string; type: string };
  value: string;
}

type Tr = (key: string) => string;

// ExportColumn ist eine wählbare Spalte: stabiler Schlüssel (für die gemerkte
// Auswahl), Überschrift und Zellwert je Gerät.
export interface ExportColumn {
  key: string;
  label: string;
  value: (d: Device) => string;
}

// csvCell maskiert einen Wert für CSV: Anführungszeichen verdoppeln, bei
// Trennzeichen/Zeilenumbruch/Anführungszeichen in Anführungszeichen setzen.
export function csvCell(v: string): string {
  return /[";\n\r]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v;
}

// fieldDisplay macht einen gespeicherten Feldwert lesbar: Listen/Mehrfachauswahlen
// (JSON-Array) mit Komma verbinden, Checkboxen als Ja/Nein.
export function fieldDisplay(type: string, value: string, t: Tr): string {
  if (type === "checkbox") return value === "true" ? t("Ja") : t("Nein");
  if (type === "list" || type === "multiselect") {
    try {
      const arr = JSON.parse(value);
      if (Array.isArray(arr)) return arr.map(String).join(", ");
    } catch { /* kein JSON -> roh */ }
  }
  return value;
}

const num = (n?: number | null) => (n == null ? "" : String(n));

// baseColumns sind die Stammdaten-Spalten aus der Geräteliste.
export function baseColumns(t: Tr): ExportColumn[] {
  return [
    { key: "hostname", label: "Hostname", value: (d) => d.hostname },
    { key: "status", label: t("Status"), value: (d) => d.status },
    { key: "client", label: t("Firma"), value: (d) => d.client_name ?? "" },
    { key: "site", label: t("Standort"), value: (d) => d.site_name ?? "" },
    { key: "os", label: t("Betriebssystem"), value: (d) => d.os },
    { key: "os_version", label: t("Version"), value: (d) => d.os_version },
    { key: "vendor", label: t("Hersteller"), value: (d) => d.vendor },
    { key: "model", label: t("Modell"), value: (d) => d.model },
    { key: "serial", label: t("Seriennummer"), value: (d) => d.serial },
    { key: "cpu", label: "CPU", value: (d) => d.cpu_model },
    { key: "cores", label: t("Kerne"), value: (d) => (d.cpu_cores ? String(d.cpu_cores) : "") },
    { key: "ram", label: "RAM", value: (d) => (d.memory_bytes ? formatBytes(d.memory_bytes) : "") },
    { key: "ip", label: t("IP-Adresse"), value: (d) => primaryIPv4(d).replace("—", "") },
    { key: "public_ip", label: t("Öffentliche IP"), value: (d) => d.public_ip ?? "" },
    { key: "mac", label: "MAC", value: (d) => (d.interfaces ?? []).map((i) => i.mac).filter(Boolean).join(", ") },
    { key: "users", label: t("Benutzer"), value: (d) => (d.logged_in_users ?? []).join(", ") },
    { key: "agent", label: t("Agent"), value: (d) => d.agent_version },
    { key: "updates", label: t("Updates"), value: (d) => num(d.updates_count) },
    { key: "checks_failing", label: t("Checks fehlerhaft"), value: (d) => String(d.checks_failing ?? 0) },
    { key: "tasks_failing", label: t("Tasks fehlerhaft"), value: (d) => String(d.tasks_failing ?? 0) },
    { key: "vulns", label: t("CVE"), value: (d) => String(d.vuln_count ?? 0) },
    { key: "last_seen", label: t("Zuletzt gesehen"), value: (d) => d.last_seen ?? "" },
  ];
}

// fieldColumns liefert eine Spalte je benutzerdefiniertem Feld, das bei mindestens
// einem der Geräte gesetzt ist (alphabetisch nach Feldname).
export function fieldColumns(devices: Device[], fields: EntityFieldValue[], t: Tr): ExportColumn[] {
  const ids = new Set(devices.map((d) => d.id));
  const defs = new Map<string, { name: string; type: string }>();
  const values = new Map<string, string>(); // deviceID + "\0" + fieldID -> Wert
  for (const v of fields) {
    if (!ids.has(v.entity_id) || v.value === "") continue;
    defs.set(v.field.id, { name: v.field.name, type: v.field.type });
    values.set(v.entity_id + "\0" + v.field.id, v.value);
  }
  return [...defs.entries()]
    .sort((a, b) => a[1].name.localeCompare(b[1].name))
    .map(([id, f]) => ({
      key: "cf:" + id,
      label: f.name,
      value: (d) => {
        const raw = values.get(d.id + "\0" + id);
        return raw == null ? "" : fieldDisplay(f.type, raw, t);
      },
    }));
}

// devicesCSV baut die Exportdatei aus den gewählten Spalten. Semikolon als
// Trennzeichen und BOM, damit Excel (de) die Datei per Doppelklick samt Umlauten öffnet.
export function devicesCSV(devices: Device[], columns: ExportColumn[]): string {
  const head = columns.map((c) => c.label);
  const rows = devices.map((d) => columns.map((c) => c.value(d)));
  return "﻿" + [head, ...rows].map((r) => r.map(csvCell).join(";")).join("\r\n") + "\r\n";
}

// downloadText reicht einen Text als Datei an den Browser (Download-Dialog bzw. Ordner).
export function downloadText(filename: string, text: string, mime = "text/csv;charset=utf-8") {
  const url = URL.createObjectURL(new Blob([text], { type: mime }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
