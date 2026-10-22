import type { Device } from "./types";

// Kleine Helfer für Geräteanzeige und -export (ohne React-Abhängigkeit).

// isPrivateIPv4 erkennt interne Adressen (RFC1918, Link-Local, Loopback, CGNAT).
function isPrivateIPv4(ip: string): boolean {
  const p = ip.split(".").map(Number);
  if (p.length !== 4 || p.some((n) => Number.isNaN(n))) return true;
  const [a, b] = p;
  if (a === 10 || a === 127) return true;
  if (a === 172 && b >= 16 && b <= 31) return true;
  if (a === 192 && b === 168) return true;
  if (a === 169 && b === 254) return true; // Link-Local
  if (a === 100 && b >= 64 && b <= 127) return true; // CGNAT
  return false;
}

// primaryIPv4 liefert bevorzugt die erste öffentliche (nicht-interne) IPv4,
// sonst die erste interne. Schnittstellen können mehrere IPs (komma-getrennt) haben.
export function primaryIPv4(d: Device): string {
  const all: string[] = [];
  for (const i of d.interfaces ?? []) {
    if (!i.ipv4) continue;
    for (const ip of i.ipv4.split(",").map((s) => s.trim()).filter(Boolean)) all.push(ip);
  }
  return all.find((ip) => !isPrivateIPv4(ip)) ?? all[0] ?? "—";
}

export function formatBytes(n: number): string {
  if (!n) return "—";
  const gb = n / 1024 ** 3;
  return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(n / 1024 ** 2).toFixed(0)} MB`;
}
