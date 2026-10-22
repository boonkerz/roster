import type { Device } from "./types";
import { primaryIPv4, formatBytes } from "./devutil";

// Feldwert eines Geräts aus dem Sammelabruf GET /custom-field-values?model=device.
export interface EntityFieldValue {
  entity_id: string;
  field: { id: string; name: string; type: string };
  value: string;
}

type Tr = (key: string) => string;

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

// devicesCSV baut die Exportdatei: Stammdaten je Gerät plus eine Spalte je
// benutzerdefiniertem Feld (nur Felder, die bei mindestens einem Gerät gesetzt sind).
// Semikolon als Trennzeichen, damit Excel (de) die Datei per Doppelklick öffnet.
export function devicesCSV(devices: Device[], fields: EntityFieldValue[], t: Tr): string {
  const ids = new Set(devices.map((d) => d.id));
  const fieldDefs = new Map<string, { name: string; type: string }>();
  const values = new Map<string, string>(); // deviceID + "\0" + fieldID -> Wert
  for (const v of fields) {
    if (!ids.has(v.entity_id) || v.value === "") continue;
    fieldDefs.set(v.field.id, { name: v.field.name, type: v.field.type });
    values.set(v.entity_id + "\0" + v.field.id, v.value);
  }
  const fieldIDs = [...fieldDefs.keys()].sort((a, b) => fieldDefs.get(a)!.name.localeCompare(fieldDefs.get(b)!.name));

  const head = [
    "Hostname", t("Status"), t("Firma"), t("Standort"), t("Betriebssystem"), t("Version"),
    t("Hersteller"), t("Modell"), t("Seriennummer"), "CPU", t("Kerne"), "RAM",
    t("IP-Adresse"), t("Öffentliche IP"), "MAC", t("Benutzer"), t("Agent"),
    t("Updates"), t("Checks fehlerhaft"), t("Tasks fehlerhaft"), t("CVE"), t("Zuletzt gesehen"),
    ...fieldIDs.map((id) => fieldDefs.get(id)!.name),
  ];
  const rows = devices.map((d) => {
    const macs = (d.interfaces ?? []).map((i) => i.mac).filter(Boolean).join(", ");
    const base = [
      d.hostname, d.status, d.client_name ?? "", d.site_name ?? "", d.os, d.os_version,
      d.vendor, d.model, d.serial, d.cpu_model, d.cpu_cores ? String(d.cpu_cores) : "",
      d.memory_bytes ? formatBytes(d.memory_bytes) : "",
      primaryIPv4(d).replace("—", ""), d.public_ip ?? "", macs, (d.logged_in_users ?? []).join(", "), d.agent_version,
      d.updates_count == null ? "" : String(d.updates_count),
      String(d.checks_failing ?? 0), String(d.tasks_failing ?? 0), String(d.vuln_count ?? 0),
      d.last_seen ?? "",
    ];
    const extra = fieldIDs.map((id) => {
      const raw = values.get(d.id + "\0" + id);
      return raw == null ? "" : fieldDisplay(fieldDefs.get(id)!.type, raw, t);
    });
    return [...base, ...extra];
  });
  // BOM, damit Excel UTF-8 (Umlaute) erkennt.
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
