import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { useI18n } from "../i18n";
import type { ClientTree, Group, Script, DeployPackage } from "../types";

type TargetType = "all" | "client" | "site" | "group";

// BulkActions führt Skripte oder Update-Scans auf mehreren Geräten (Ziel) aus.
export function BulkActions() {
  const { t } = useI18n();
  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const { data: groups } = useQuery({ queryKey: ["groups"], queryFn: () => api.get<Group[]>("/groups") });
  const { data: scripts } = useQuery({ queryKey: ["scripts"], queryFn: () => api.get<Script[]>("/scripts") });
  const { data: packages } = useQuery({ queryKey: ["software-packages"], queryFn: () => api.get<DeployPackage[]>("/software-packages") });

  const [tType, setTType] = useState<TargetType>("client");
  const [target, setTarget] = useState("");
  const [action, setAction] = useState<"script" | "scan" | "install" | "package">("script");
  const [scriptId, setScriptId] = useState("");
  const [packageId, setPackageId] = useState("");
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);

  const targets =
    tType === "client" ? (tree?.clients ?? []).map((c) => ({ id: c.id, name: c.name }))
    : tType === "site" ? (tree?.clients ?? []).flatMap((c) => (c.sites ?? []).map((s) => ({ id: s.id, name: `${c.name} › ${s.name}` })))
    : tType === "group" ? (groups ?? []).map((g) => ({ id: g.id, name: g.name }))
    : [];

  const run = useMutation({
    mutationFn: () => {
      const body = { target_type: tType, target_id: tType === "all" ? "" : target, script_id: scriptId, package_id: packageId };
      const url = action === "script" ? "/bulk/run-script"
        : action === "install" ? "/bulk/install-updates"
        : action === "package" ? "/bulk/install-package"
        : "/bulk/scan-updates";
      return api.post<{ queued: number }>(url, body);
    },
    onSuccess: (d) => setMsg({ ok: true, text: t("Auf {n} Gerät(en) eingereiht. Ergebnisse erscheinen je Gerät unter „Ausführen“ bzw. „Patches“.", { n: d.queued }) }),
    onError: (e) => setMsg({ ok: false, text: e instanceof ApiError ? e.message : t("Fehler") }),
  });

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    setMsg(null);
    if (tType !== "all" && !target) { setMsg({ ok: false, text: t("Bitte ein Ziel wählen.") }); return; }
    if (action === "script" && !scriptId) { setMsg({ ok: false, text: t("Bitte ein Skript wählen.") }); return; }
    if (action === "package" && !packageId) { setMsg({ ok: false, text: t("Bitte ein Paket wählen.") }); return; }
    if (action === "install" && !window.confirm(t("Updates auf allen Geräten des Ziels installieren? Das kann Neustarts auslösen."))) return;
    run.mutate();
  };

  return (
    <div className="page">
      <header className="page-head"><h1>{t("Sammelaktion")}</h1></header>
      <section className="card">
        <p className="muted small">{t("Führt eine Aktion auf allen Geräten des gewählten Ziels aus. Offline-Geräte holen den Befehl beim nächsten Checkin ab.")}</p>
        <form className="form-col" onSubmit={submit} style={{ maxWidth: 460 }}>
          <label className="field">
            <span>{t("Ziel")}</span>
            <div className="inline-form">
              <select value={tType} onChange={(e) => { setTType(e.target.value as TargetType); setTarget(""); }}>
                <option value="client">{t("Client")}</option>
                <option value="site">{t("Standort")}</option>
                <option value="group">{t("Tag/Gruppe")}</option>
                <option value="all">{t("Alle Geräte")}</option>
              </select>
              {tType !== "all" && (
                <select value={target} onChange={(e) => setTarget(e.target.value)} style={{ flex: 1 }}>
                  <option value="">{t("— wählen —")}</option>
                  {targets.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                </select>
              )}
            </div>
          </label>
          <label className="field">
            <span>{t("Aktion")}</span>
            <select value={action} onChange={(e) => setAction(e.target.value as "script" | "scan" | "install" | "package")}>
              <option value="script">{t("Skript ausführen")}</option>
              <option value="scan">{t("Updates prüfen")}</option>
              <option value="install">{t("Updates durchführen")}</option>
              <option value="package">{t("Software installieren")}</option>
            </select>
          </label>
          {action === "script" && (
            <label className="field">
              <span>{t("Skript")}</span>
              <select value={scriptId} onChange={(e) => setScriptId(e.target.value)}>
                <option value="">{t("— Skript wählen —")}</option>
                {(scripts ?? []).filter((s) => !s.check_only).map((s) => <option key={s.id} value={s.id}>{s.name} ({s.shell})</option>)}
              </select>
            </label>
          )}
          {action === "package" && (
            <label className="field">
              <span>{t("Paket")}</span>
              <select value={packageId} onChange={(e) => setPackageId(e.target.value)}>
                <option value="">{t("— Paket wählen —")}</option>
                {(packages ?? []).map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </select>
              {(packages ?? []).length === 0 && <span className="muted small">{t("Erst unter Einstellungen → Software-Pakete anlegen.")}</span>}
            </label>
          )}
          {msg && <p className={msg.ok ? "form-ok" : "form-err"}>{msg.text}</p>}
          <div>
            <button className="btn primary" type="submit" disabled={run.isPending}>
              {run.isPending ? t("Reiht ein…") : t("Ausführen")}
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}
