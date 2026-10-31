import { api } from "./api";
import { gt } from "./i18n";
import type { Command } from "./types";

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// pollCmd wartet auf das Ergebnis eines eingereihten Agent-Befehls (GET /commands/{id})
// – gemeinsamer Helfer für alle On-Demand-Panels (Sicherheit, Event-Log, Dateien, Fernsteuerung …).
export async function pollCmd(id: string, tries = 90): Promise<Command> {
  for (let i = 0; i < tries; i++) {
    await sleep(700);
    const cmd = await api.get<Command>(`/commands/${id}`);
    if (cmd.status === "done") return cmd;
  }
  throw new Error(gt("Zeitüberschreitung – Agent offline?"));
}
