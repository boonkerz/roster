import type { Temperature } from "./types";

// Bewertung eines Temperatursensors – dieselbe Logik wie im Agent
// (shared.Temperature.WarnAt/Status) und in der Taskleisten-App.

export type TempStatus = "ok" | "warn" | "critical" | "unknown";

// warnAt: Warnschwelle in °C (0 = unbekannt). Melden Treiber max = crit (z. B. Intel
// coretemp: beides TjMax), gilt 10 °C darunter als Warnung.
export function warnAt(t: Temperature): number {
  const high = t.high ?? 0, crit = t.critical ?? 0;
  if (high > 0 && (crit === 0 || high < crit)) return high;
  if (crit > 10) return crit - 10;
  return 0;
}

export function tempStatus(t: Temperature): TempStatus {
  const crit = t.critical ?? 0, warn = warnAt(t);
  if (crit > 0 && t.celsius >= crit) return "critical";
  if (warn > 0 && t.celsius >= warn) return "warn";
  if (warn === 0 && crit === 0) return "unknown";
  return "ok";
}

const rank: Record<TempStatus, number> = { critical: 3, warn: 2, ok: 1, unknown: 0 };

// headlineTemp: der auffälligste Sensor – zuerst nach Status (kritisch vor Warnung),
// sonst der heißeste CPU-Sensor, ohne CPU der heißeste überhaupt.
export function headlineTemp(temps: Temperature[] = []): { t: Temperature; status: TempStatus } | null {
  if (temps.length === 0) return null;
  const worst = [...temps].sort((a, b) => rank[tempStatus(b)] - rank[tempStatus(a)] || b.celsius - a.celsius)[0];
  const ws = tempStatus(worst);
  if (ws === "critical" || ws === "warn") return { t: worst, status: ws };
  const cpu = temps.filter((t) => t.class === "cpu");
  const hottest = [...(cpu.length ? cpu : temps)].sort((a, b) => b.celsius - a.celsius)[0];
  return { t: hottest, status: tempStatus(hottest) };
}
