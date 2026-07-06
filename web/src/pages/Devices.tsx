import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { Device, ClientTree as Tree } from "../types";
import { StatusBadge, UpdatesBadge, HealthBadge, TaskHealthBadge, relTime } from "../components/StatusBadge";
import { ClientTree, OrgFilter } from "../components/ClientTree";
import { AddComputerDialog } from "../components/AddComputerDialog";
import { DevicePanel } from "../components/DevicePanel";
import { CopyText } from "../components/CopyText";
import { useAuth } from "../auth";
import { useI18n } from "../i18n";

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

function matchesOrg(d: Device, f: OrgFilter): boolean {
  switch (f.kind) {
    case "all": return true;
    case "unassigned": return !d.site_id;
    case "client": return d.client_id === f.id;
    case "site": return d.site_id === f.id;
  }
}

export function Devices() {
  const { t } = useI18n();
  const [q, setQ] = useState("");
  const [showAdd, setShowAdd] = useState(false);
  // Filterauswahl über Navigation hinweg merken (z.B. Gerätedetail -> zurück).
  const [org, setOrg] = useState<OrgFilter>(() => {
    try {
      const s = sessionStorage.getItem("pcinv-org");
      if (s) return JSON.parse(s) as OrgFilter;
    } catch { /* ignore */ }
    return { kind: "all" };
  });
  useEffect(() => {
    sessionStorage.setItem("pcinv-org", JSON.stringify(org));
  }, [org]);
  // Ausgewähltes Gerät (für das untere Detail-Panel) ebenfalls merken.
  const [selectedId, setSelectedId] = useState<string | null>(() => sessionStorage.getItem("pcinv-selected") || null);
  useEffect(() => {
    if (selectedId) sessionStorage.setItem("pcinv-selected", selectedId);
    else sessionStorage.removeItem("pcinv-selected");
  }, [selectedId]);
  // Direktsprung in einen Panel-Tab (z.B. Klick auf Checks/Tasks in der Liste).
  const [jump, setJump] = useState<{ tab: string; n: number }>({ tab: "", n: 0 });
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";

  // Suche serverseitig (debounced) – deckt Hostname, IP/MAC, OS, Seriennr.,
  // installierte Software und Custom-Field-Werte ab.
  const [dq, setDq] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDq(q.trim()), 300);
    return () => clearTimeout(t);
  }, [q]);

  const { data, isLoading, error } = useQuery({
    queryKey: ["devices", dq],
    queryFn: () => api.get<Device[]>(`/devices${dq ? `?q=${encodeURIComponent(dq)}` : ""}`),
    refetchInterval: 15000,
  });
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<Tree>("/clients") });

  // Zustands-Filter über die URL (z. B. Klick auf eine Dashboard-Kachel).
  const [params, setParams] = useSearchParams();
  const health = params.get("filter") || "";
  const matchesHealth = (d: Device) => {
    switch (health) {
      case "failing-checks": return (d.checks_failing ?? 0) > 0;
      case "failing-tasks": return (d.tasks_failing ?? 0) > 0;
      case "vulns": return (d.vuln_count ?? 0) > 0;
      default: return true;
    }
  };
  const healthLabel: Record<string, string> = {
    "failing-checks": t("Nur Geräte mit fehlerhaften Checks"),
    "failing-tasks": t("Nur Geräte mit fehlerhaften Tasks"),
    "vulns": t("Nur Geräte mit Schwachstellen"),
  };
  const clearHealth = () => { const p = new URLSearchParams(params); p.delete("filter"); setParams(p, { replace: true }); };

  const devices = (data ?? []).filter((d) => matchesOrg(d, org) && matchesHealth(d));

  const online = devices.filter((d) => d.status === "online").length;

  return (
    <div className="page devices-layout">
      <aside>
        {tree && (
          <ClientTree tree={tree} total={data?.length ?? 0} selected={org} onSelect={setOrg} isAdmin={isAdmin} />
        )}
      </aside>

      <div className="devices-main">
        <header className="page-head">
          <div>
            <h1>{t("Geräte")}</h1>
            <p className="muted">
              {t("{n} angezeigt · {online} online", { n: devices.length, online })}
              {health && healthLabel[health] && (
                <button className="filter-chip" onClick={clearHealth} title={t("Filter entfernen")}>
                  {healthLabel[health]} ✕
                </button>
              )}
            </p>
          </div>
          <div className="head-actions">
            <input className="search" placeholder={t("Suche: Hostname, IP, OS, Software, Custom Fields…")} value={q} onChange={(e) => setQ(e.target.value)} style={{ minWidth: 280 }} />
            {isAdmin && <button className="btn primary" onClick={() => setShowAdd(true)}>{t("+ Neuer Computer")}</button>}
          </div>
        </header>

        {showAdd && <AddComputerDialog onClose={() => setShowAdd(false)} />}
        {isLoading && <div className="muted">{t("Lädt…")}</div>}
        {error && <div className="form-error">{t("Fehler beim Laden.")}</div>}

        {data && (
          <div className="devices-split">
            <div className="devices-table-wrap">
              <table className="table selectable">
                <thead>
                  <tr>
                    <th>{t("Status")}</th>
                    <th>Hostname</th>
                    <th>{t("Benutzer")}</th>
                    <th>{t("Betriebssystem")}</th>
                    <th>{t("IP-Adresse")}</th>
                    <th>CPU</th>
                    <th>RAM</th>
                    <th>{t("Checks")}</th>
                    <th>{t("Tasks")}</th>
                    <th>{t("CVE")}</th>
                    <th>{t("Updates")}</th>
                    <th>{t("Agent")}</th>
                    <th>{t("Zuletzt gesehen")}</th>
                  </tr>
                </thead>
                <tbody>
                  {devices.map((d) => (
                    <tr key={d.id} className={selectedId === d.id ? "row-selected" : ""} onClick={() => setSelectedId(d.id)}>
                      <td><StatusBadge status={d.status} /></td>
                      <td>
                        <span className="link-strong">{d.hostname || t("(unbenannt)")}</span>
                        {d.revoked && <span className="badge badge-offline" style={{ marginLeft: 8 }}>{t("widerrufen")}</span>}
                        {d.site_id && <div className="muted small">{d.client_name} › {d.site_name}</div>}
                      </td>
                      <td className="muted">{d.logged_in_users?.join(", ") || "—"}</td>
                      <td>{d.os} {d.os_version}</td>
                      <td className="mono"><CopyText value={primaryIPv4(d)} /></td>
                      <td className="muted">{d.cpu_cores ? t("{n} Kerne", { n: d.cpu_cores }) : "—"}</td>
                      <td className="muted">{formatBytes(d.memory_bytes)}</td>
                      <td style={{ cursor: "pointer" }} title={t("Zu den Checks springen")}
                        onClick={(e) => { e.stopPropagation(); setSelectedId(d.id); setJump({ tab: "checks", n: jump.n + 1 }); }}>
                        <HealthBadge total={d.checks_total} failing={d.checks_failing} />
                      </td>
                      <td style={{ cursor: "pointer" }} title={t("Zu den Tasks springen")}
                        onClick={(e) => { e.stopPropagation(); setSelectedId(d.id); setJump({ tab: "tasks", n: jump.n + 1 }); }}>
                        <TaskHealthBadge total={d.tasks_total} failing={d.tasks_failing} />
                      </td>
                      <td style={{ cursor: "pointer" }} title={t("Zu den Schwachstellen springen")}
                        onClick={(e) => { e.stopPropagation(); setSelectedId(d.id); setJump({ tab: "vulns", n: jump.n + 1 }); }}>
                        {d.vuln_count ? <span className="badge badge-offline">{d.vuln_count}</span> : <span className="muted">—</span>}
                      </td>
                      <td><UpdatesBadge count={d.updates_count} /></td>
                      <td className="muted mono">{d.agent_version || "—"}</td>
                      <td className="muted">{relTime(d.last_seen)}</td>
                    </tr>
                  ))}
                  {devices.length === 0 && (
                    <tr><td colSpan={13} className="empty">{t("Keine Geräte gefunden.")}</td></tr>
                  )}
                </tbody>
              </table>
            </div>

            {selectedId && data.some((d) => d.id === selectedId) ? (
              <div className="devices-detail-wrap">
                <DevicePanel id={selectedId} focusTab={jump.tab} focusKey={jump.n} />
              </div>
            ) : (
              <div className="devices-detail-wrap empty-panel muted">{t("Gerät auswählen, um Details zu sehen.")}</div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
