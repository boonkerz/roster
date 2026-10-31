import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { api } from "../api";
import type { DashboardSummary } from "../types";
import { useI18n } from "../i18n";
import { DashWidgets } from "../components/DashWidgets";

// Tile ist eine Kennzahl-Kachel; klickbar führt sie z.B. zur Geräteliste.
function Tile({ label, value, sub, tone, onClick }: {
  label: string; value: number | string; sub?: string; tone?: "ok" | "warn" | "bad"; onClick?: () => void;
}) {
  return (
    <div className={`dash-tile${tone ? ` dash-${tone}` : ""}${onClick ? " dash-click" : ""}`} onClick={onClick}>
      <div className="dash-value">{value}</div>
      <div className="dash-label">{label}</div>
      {sub && <div className="muted small">{sub}</div>}
    </div>
  );
}

export function Dashboard() {
  const nav = useNavigate();
  const { t } = useI18n();
  const { data, isLoading } = useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api.get<DashboardSummary>("/dashboard"),
    refetchInterval: 15000,
  });

  if (isLoading || !data) return <div className="page wide"><p className="muted">{t("Lädt…")}</p></div>;

  const onlinePct = data.devices_total ? Math.round((data.devices_online / data.devices_total) * 100) : 0;

  return (
    <div className="page wide">
      <header className="page-head">
        <div>
          <h1>{t("Übersicht")}</h1>
          <p className="muted">{t("Zusammenfassung über alle Geräte. Aktualisiert sich automatisch.")}</p>
        </div>
      </header>

      <div className="dash-grid">
        <Tile label={t("Geräte gesamt")} value={data.devices_total} onClick={() => nav("/devices")} />
        <Tile label={t("Online")} value={data.devices_online} sub={`${onlinePct}%`} tone="ok" onClick={() => nav("/devices")} />
        <Tile label={t("Offline")} value={data.devices_offline} tone={data.devices_offline > 0 ? "bad" : undefined} onClick={() => nav("/devices")} />
        <Tile label={t("Checks fehlerhaft")} value={data.failing_checks} sub={t("{n} Gerät(e)", { n: data.devices_with_failing_checks })} tone={data.failing_checks > 0 ? "bad" : "ok"} onClick={() => nav("/devices?filter=failing-checks")} />
        <Tile label={t("Tasks fehlerhaft")} value={data.failing_tasks} sub={t("{n} Gerät(e)", { n: data.devices_with_failing_tasks })} tone={data.failing_tasks > 0 ? "warn" : "ok"} onClick={() => nav("/devices?filter=failing-tasks")} />
        <Tile label={t("Ausstehende Patches")} value={data.pending_patches} sub={t("{n} Gerät(e)", { n: data.devices_with_pending_patches })} tone={data.pending_patches > 0 ? "warn" : "ok"} onClick={() => nav("/patches")} />
        <Tile label={t("Sicherheits-Updates")} value={data.security_patches ?? 0} sub={t("{n} Gerät(e)", { n: data.devices_with_security_patches ?? 0 })} tone={(data.security_patches ?? 0) > 0 ? "bad" : "ok"} onClick={() => nav("/patches?security=1")} />
        <Tile label={t("Support-Ende")} value={(data.devices_eol ?? 0) + (data.devices_eol_soon ?? 0)} sub={t("{n} erreicht · {m} bald", { n: data.devices_eol ?? 0, m: data.devices_eol_soon ?? 0 })} tone={(data.devices_eol ?? 0) > 0 ? "bad" : (data.devices_eol_soon ?? 0) > 0 ? "warn" : "ok"} onClick={() => nav("/patches?tab=eol")} />
        <Tile label={t("Offene Vorfälle")} value={data.open_incidents ?? 0} sub={t("{n} Fehlversuche (24 h)", { n: data.failed_logins_24h ?? 0 })} tone={(data.open_incidents ?? 0) > 0 ? "bad" : "ok"} onClick={() => nav("/security")} />
        {(data.devices_compliance_checked ?? 0) > 0 && <Tile label={t("Compliance Ø")} value={`${data.compliance_avg ?? 0}%`} sub={t("{n} unter 80 %", { n: data.devices_low_compliance ?? 0 })} tone={(data.compliance_avg ?? 0) >= 90 ? "ok" : (data.compliance_avg ?? 0) >= 70 ? "warn" : "bad"} onClick={() => nav("/security?tab=compliance")} />}
        <Tile label={t("Schwachstellen")} value={data.vulnerabilities} sub={t("{n} Gerät(e)", { n: data.devices_with_vulns })} tone={data.vulnerabilities > 0 ? "bad" : "ok"} onClick={() => nav("/devices?filter=vulns")} />
      </div>

      {/* Alles unterhalb der Kennzahlen ist das konfigurierbare Kachel-Raster (Geräte-Status
          und letzte Status-Wechsel sind dort die Standard-Kacheln). */}
      <DashWidgets events={data.recent_events ?? []} />
    </div>
  );
}
