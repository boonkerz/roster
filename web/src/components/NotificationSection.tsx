import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { Policy, PolicyNotifications } from "../types";
import { TRIGGER_LABEL } from "./SecuritySection";

// Benachrichtigungs-Bereich einer Richtlinie: ob Software-Änderungen auf den
// zugewiesenen Geräten gemeldet werden (Standard = globale Alarm-Einstellung), und
// welche davon. Bei mehreren Richtlinien gewinnt die am Gerät zugewiesene vor
// Standort vor Client; das Gerät selbst kann den Schalter zuletzt überschreiben.

const KINDS: [string, string][] = [["added", "installiert"], ["removed", "entfernt"], ["updated", "aktualisiert"]];
// Vorfallsarten der Sicherheitsüberwachung (Reihenfolge wie im Server).
const SEC_KINDS = ["brute_force", "brute_force_success", "scan", "new_source_ip", "account_change", "lockout", "fim_change"];

export function NotificationSection({ policy }: { policy: Policy }) {
  const { t } = useI18n();
  const qc = useQueryClient();
  const n = policy.notifications;
  const [mode, setMode] = useState<PolicyNotifications["software_alerts"]>("");
  const [kinds, setKinds] = useState<string[]>([]);
  const [ignore, setIgnore] = useState("");
  const [secKinds, setSecKinds] = useState<string[]>(SEC_KINDS);
  const [skipBlocked, setSkipBlocked] = useState(false);
  const [err, setErr] = useState("");

  useEffect(() => {
    setMode(n?.software_alerts ?? "");
    setKinds(n?.software_alert_kinds ?? KINDS.map(([k]) => k));
    setIgnore((n?.software_ignore ?? []).join("\n"));
    setSecKinds(n?.security_alert_kinds ?? SEC_KINDS);
    setSkipBlocked(n?.security_alert_skip_blocked ?? false);
    setErr("");
  }, [policy.id, n?.software_alerts, n?.software_alert_kinds, n?.software_ignore, n?.security_alert_kinds, n?.security_alert_skip_blocked]);

  const save = useMutation({
    mutationFn: () => api.put<PolicyNotifications>(`/policies/${policy.id}/notifications`, {
      software_alerts: mode,
      software_alert_kinds: kinds,
      software_ignore: ignore.split("\n").map((l) => l.trim()).filter(Boolean),
      security_alert_kinds: secKinds,
      security_alert_skip_blocked: skipBlocked,
    }),
    onSuccess: () => { setErr(""); qc.invalidateQueries({ queryKey: ["policies"] }); },
    onError: (e) => setErr((e as Error).message),
  });

  const dirty = mode !== (n?.software_alerts ?? "")
    || kinds.join(",") !== (n?.software_alert_kinds ?? KINDS.map(([k]) => k)).join(",")
    || ignore.trim() !== (n?.software_ignore ?? []).join("\n").trim()
    || secKinds.join(",") !== (n?.security_alert_kinds ?? SEC_KINDS).join(",")
    || skipBlocked !== (n?.security_alert_skip_blocked ?? false);
  const toggleSec = (k: string, on: boolean) => setSecKinds((cur) => (on ? SEC_KINDS.filter((x) => x === k || cur.includes(x)) : cur.filter((x) => x !== k)));
  const filtersActive = mode === "on";
  const toggleKind = (k: string, on: boolean) =>
    setKinds((cur) => (on ? KINDS.map(([x]) => x).filter((x) => x === k || cur.includes(x)) : cur.filter((x) => x !== k)));

  return (
    <section className="card">
      <h2>{t("Benachrichtigungen")}</h2>
      <p className="muted small">
        {t("Ob Software-Änderungen auf den zugewiesenen Geräten gemeldet werden. „Standard“ folgt der globalen Alarm-Einstellung. Bei mehreren Richtlinien gilt: Gerät vor Standort vor Client; das Gerät selbst kann zuletzt überschreiben.")}
      </p>
      <div className="inline-form">
        <label style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>{t("Software-Änderungen melden")}
          <select value={mode} onChange={(e) => setMode(e.target.value as PolicyNotifications["software_alerts"])}>
            <option value="">{t("Standard (globale Einstellung)")}</option>
            <option value="on">{t("Ja")}</option>
            <option value="off">{t("Nein")}</option>
          </select>
        </label>
        {KINDS.map(([k, label]) => (
          <label key={k} className="chip" title={filtersActive ? "" : t("Filter gelten nur bei „Ja“.")}>
            <input type="checkbox" disabled={!filtersActive} checked={kinds.includes(k)} onChange={(e) => toggleKind(k, e.target.checked)} /> {t(label)}
          </label>
        ))}
      </div>
      <textarea
        className="notes-input"
        rows={3}
        disabled={!filtersActive}
        placeholder={t("Ignorierte Programme (ein Muster je Zeile, z. B. Microsoft Edge*)")}
        value={ignore}
        onChange={(e) => setIgnore(e.target.value)}
        title={t("Glob-Muster auf den Programmnamen, Groß-/Kleinschreibung egal. * steht für beliebige Zeichen.")}
      />
      {filtersActive && kinds.length === 0 && <p className="muted small">{t("Keine Änderungsart gewählt – es wird nichts gemeldet.")}</p>}
      {!filtersActive && <p className="muted small">{t("Filter gelten nur bei „Ja“.")}</p>}

      <h3 className="muted small" style={{ marginTop: 18 }}>{t("Sicherheitsvorfälle melden")}</h3>
      <p className="muted small">{t("Welche Vorfälle der Sicherheitsüberwachung über die Alarmkanäle gemeldet werden (Push, E-Mail …). Nur Warnungen und kritische Vorfälle werden gemeldet; die Vorfälle selbst erscheinen immer auf der Seite „Sicherheit“. Bei mehreren Richtlinien gilt die erste mit eigener Auswahl: Gerät vor Standort vor Client.")}</p>
      <div className="chips">
        {SEC_KINDS.map((k) => (
          <label key={k} className={`chip ${secKinds.includes(k) ? "chip-on" : ""}`}>
            <input type="checkbox" checked={secKinds.includes(k)} onChange={(e) => toggleSec(k, e.target.checked)} /> {t(TRIGGER_LABEL[k] ?? k)}
          </label>
        ))}
      </div>
      <label className="chip" style={{ display: "inline-flex", marginTop: 8 }} title={t("Hat Active Response oder die automatische Sperre die Quell-IP bereits gesperrt, bleibt die Meldung aus – der Vorfall wird trotzdem gespeichert.")}>
        <input type="checkbox" checked={skipBlocked} onChange={(e) => setSkipBlocked(e.target.checked)} /> {t("nicht melden, wenn die Quell-IP automatisch gesperrt wurde")}
      </label>
      {secKinds.length === 0 && <p className="muted small">{t("Keine Vorfallsart gewählt – Sicherheitsvorfälle werden nicht gemeldet.")}</p>}
      {err && <p className="error small">{err}</p>}
      {dirty && (
        <div className="inline-form">
          <button className="btn primary sm" disabled={save.isPending} onClick={() => save.mutate()}>{t("Speichern")}</button>
        </div>
      )}
    </section>
  );
}
