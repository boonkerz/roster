import { api } from "./api";
import { gt } from "./i18n";
import type { Command } from "./types";

// Datei-Übertragungen (bis 4 GB): Upload mit Fortschritt und ein Warten auf den Agent,
// das nicht nach fester Zeit aufgibt, solange der Agent Fortschritt meldet.

export const MAX_TRANSFER = 4 * 1024 * 1024 * 1024;

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// uploadFile schickt eine Datei an den Server (XHR statt fetch – nur so gibt es Fortschritt).
export function uploadFile(url: string, file: File, onProgress: (pct: number) => void): Promise<{ command_id: string }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", url);
    xhr.withCredentials = true;
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(Math.floor((e.loaded * 100) / e.total)); };
    xhr.onload = () => {
      let body: { command_id?: string; error?: string } = {};
      try { body = JSON.parse(xhr.responseText); } catch { /* leer */ }
      if (xhr.status >= 200 && xhr.status < 300 && body.command_id) resolve({ command_id: body.command_id });
      else reject(new Error(body.error || gt("Upload fehlgeschlagen") + ` (${xhr.status})`));
    };
    xhr.onerror = () => reject(new Error(gt("Upload fehlgeschlagen")));
    xhr.send(file);
  });
}

// pollTransfer wartet auf einen read_file/write_file-Befehl. Abbruch erst, wenn sich
// zwei Minuten lang nichts tut (kein neuer Fortschritt) – große Dateien dauern.
export async function pollTransfer(id: string, onProgress?: (text: string) => void): Promise<Command> {
  let last = "", idleSince = Date.now();
  for (;;) {
    await sleep(1000);
    const cmd = await api.get<Command>(`/commands/${id}`);
    if (cmd.status === "done") return cmd;
    const out = cmd.output || "";
    if (out !== last) { last = out; idleSince = Date.now(); if (out && onProgress) onProgress(out); }
    const limit = cmd.status === "pending" ? 90_000 : 120_000; // pending: Agent hat ihn noch nicht abgeholt
    if (Date.now() - idleSince > limit) throw new Error(gt("Zeitüberschreitung – Agent offline?"));
  }
}
