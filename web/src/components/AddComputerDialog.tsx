import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api";
import { useI18n } from "../i18n";
import type { ClientTree, EnrollmentToken } from "../types";

type OS = "linux" | "windows" | "mac";

const OS_LABEL: Record<OS, string> = { linux: "Linux", windows: "Windows", mac: "macOS" };

// buildScript erzeugt das fertige Install-Skript mit eingesetzten Variablen.
function buildScript(os: OS, server: string, token: string): string {
  if (os === "windows") {
    return `# PC-Inventory Agent für Windows – in einer PowerShell als Administrator ausführen
$Server = "${server}"
$Token  = "${token}"

$dir = "$env:ProgramFiles\\PC-Inventory"
New-Item -ItemType Directory -Force $dir, "$env:ProgramData\\PC-Inventory" | Out-Null
Invoke-WebRequest "$Server/api/v1/agents/windows-amd64" -OutFile "$dir\\agent.exe"

@"
server_url: "$Server"
enrollment_token: "$Token"
interval: "5m"
state_path: "C:/ProgramData/PC-Inventory/agent-state.json"
"@ | Set-Content "$env:ProgramData\\PC-Inventory\\agent.yaml" -Encoding ascii

& "$dir\\agent.exe" -config "$env:ProgramData\\PC-Inventory\\agent.yaml" install
& "$dir\\agent.exe" -config "$env:ProgramData\\PC-Inventory\\agent.yaml" start`;
  }

  const archCase =
    os === "mac"
      ? `  arm64) PLAT=darwin-arm64 ;;\n  x86_64) PLAT=darwin-amd64 ;;`
      : `  x86_64) PLAT=linux-amd64 ;;\n  aarch64|arm64) PLAT=linux-arm64 ;;`;
  return `#!/usr/bin/env bash
# PC-Inventory Agent für ${OS_LABEL[os]} – mit Root-Rechten ausführen
set -euo pipefail
SERVER="${server}"
TOKEN="${token}"

case "$(uname -m)" in
${archCase}
  *) echo "Nicht unterstützte Architektur: $(uname -m)" >&2; exit 1 ;;
esac

sudo curl -fsSL "$SERVER/api/v1/agents/$PLAT" -o /usr/local/bin/pc-inventory-agent
sudo chmod +x /usr/local/bin/pc-inventory-agent
sudo mkdir -p /etc/pc-inventory /var/lib/pc-inventory
sudo tee /etc/pc-inventory/agent.yaml >/dev/null <<EOF
server_url: "$SERVER"
enrollment_token: "$TOKEN"
interval: "5m"
state_path: "/var/lib/pc-inventory/agent-state.json"
EOF
sudo /usr/local/bin/pc-inventory-agent -config /etc/pc-inventory/agent.yaml install
sudo /usr/local/bin/pc-inventory-agent -config /etc/pc-inventory/agent.yaml start`;
}

// buildOneLiner liefert den One-Liner, der das Install-Skript direkt vom Server holt
// und ausführt (kein Datei-Speichern/-Kopieren nötig).
function buildOneLiner(os: OS, server: string, token: string): string {
  if (os === "windows") return `irm ${server}/i/w/${token} | iex`;
  const p = os === "mac" ? "m" : "l";
  return `curl -fsSL ${server}/i/${p}/${token} | sudo bash`;
}

export function AddComputerDialog({ onClose }: { onClose: () => void }) {
  const { t } = useI18n();
  const [clientID, setClientID] = useState("");
  const [siteID, setSiteID] = useState("");
  const [os, setOs] = useState<OS>("linux");
  const [token, setToken] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  const { data: tree } = useQuery({ queryKey: ["clients"], queryFn: () => api.get<ClientTree>("/clients") });
  const sites = (tree?.clients ?? []).find((c) => c.id === clientID)?.sites ?? [];

  const generate = async () => {
    setError("");
    setBusy(true);
    try {
      const t = await api.post<EnrollmentToken>("/enrollment-tokens", {
        label: "Setup-Assistent",
        max_uses: 0,
        expires_in_hours: 24,
        site_id: siteID || null,
      });
      setToken(t.token ?? null);
    } catch {
      setError(t("Enrollment-Token konnte nicht erzeugt werden."));
    } finally {
      setBusy(false);
    }
  };

  const script = token ? buildScript(os, window.location.origin, token) : "";
  const oneLiner = token ? buildOneLiner(os, window.location.origin, token) : "";
  const [copiedLine, setCopiedLine] = useState(false);
  const copy = () => {
    navigator.clipboard.writeText(script);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  const copyLine = () => {
    navigator.clipboard.writeText(oneLiner);
    setCopiedLine(true);
    setTimeout(() => setCopiedLine(false), 1500);
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <header className="modal-head">
          <h2>{t("Neuen Computer hinzufügen")}</h2>
          <button className="modal-close" onClick={onClose} aria-label={t("Schließen")}>×</button>
        </header>

        {!token ? (
          <>
            <p className="muted">
              {t("Optional einem Standort zuordnen – der Agent landet dann automatisch dort. Danach das erzeugte Skript auf dem Zielrechner ausführen.")}
            </p>
            <div className="inline-form">
              <label>
                {t("Client")}
                <select value={clientID} onChange={(e) => { setClientID(e.target.value); setSiteID(""); }}>
                  <option value="">{t("— kein Standort —")}</option>
                  {(tree?.clients ?? []).map((c) => (
                    <option key={c.id} value={c.id}>{c.name}</option>
                  ))}
                </select>
              </label>
              <label>
                {t("Standort")}
                <select value={siteID} onChange={(e) => setSiteID(e.target.value)} disabled={!clientID}>
                  <option value="">{t("— gesamter Client —")}</option>
                  {sites.map((s) => (
                    <option key={s.id} value={s.id}>{s.name}</option>
                  ))}
                </select>
              </label>
              <button className="btn primary" onClick={generate} disabled={busy}>
                {busy ? t("Erzeuge…") : t("Skript erzeugen")}
              </button>
            </div>
            {error && <div className="form-error">{error}</div>}
          </>
        ) : (
          <>
            <div className="tabs">
              {(Object.keys(OS_LABEL) as OS[]).map((o) => (
                <button key={o} className={`tab ${os === o ? "tab-on" : ""}`} onClick={() => setOs(o)}>
                  {OS_LABEL[o]}
                </button>
              ))}
            </div>
            <label className="muted small" style={{ display: "block", marginBottom: 4 }}>
              {os === "windows"
                ? t("Schnell: in einer PowerShell als Administrator ausführen:")
                : t("Schnell: mit Root-Rechten ausführen:")}
            </label>
            <div className="code-block">
              <button className="btn ghost sm copy-btn" onClick={copyLine}>{copiedLine ? t("Kopiert ✓") : t("Kopieren")}</button>
              <pre className="one-liner">{oneLiner}</pre>
            </div>
            <label className="muted small" style={{ display: "block", margin: "10px 0 4px" }}>
              {t("Oder das vollständige Skript:")}
            </label>
            <div className="code-block">
              <button className="btn ghost sm copy-btn" onClick={copy}>{copied ? t("Kopiert ✓") : t("Kopieren")}</button>
              <pre>{script}</pre>
            </div>
            <p className="muted small">
              {siteID ? t("Gerät wird dem gewählten Standort zugeordnet.") + " " : ""}
              {t("Enthält ein Enrollment-Token (24 h gültig, mehrfach nutzbar).")}
            </p>
          </>
        )}
      </div>
    </div>
  );
}
