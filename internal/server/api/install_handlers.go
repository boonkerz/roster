package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Öffentliche Install-Endpoints: liefern ein fertiges Install-Skript mit
// eingebettetem Server-URL + Enrollment-Token, sodass ein Rechner per One-Liner
// bereitgestellt werden kann, z.B.:
//
//	PowerShell (als Admin):  irm https://host/i/w/<token> | iex
//	Linux/macOS (root):      curl -fsSL https://host/i/l/<token> | sudo bash
//
// Kein Login nötig – das Enrollment-Token ist das Geheimnis (wie im Skript selbst).
// Es wird beim Enrollment validiert; ungültige Tokens führen lediglich zu einem
// Agent, der sich nicht registrieren kann.

func (s *Server) handleInstallScript(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	plat := strings.ToLower(chi.URLParam(r, "plat"))
	server := s.externalBaseURL(r)

	var script string
	switch plat {
	case "w", "windows", "win":
		script = installScriptWindows(server, token)
	case "l", "linux":
		script = installScriptUnix("linux", server, token)
	case "m", "mac", "darwin":
		script = installScriptUnix("mac", server, token)
	default:
		s.writeErr(w, http.StatusNotFound, "unbekannte plattform")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
}

// externalBaseURL rekonstruiert die von außen sichtbare Basis-URL (hinter einem
// TLS-terminierenden Reverse-Proxy via X-Forwarded-*).
func (s *Server) externalBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

func installScriptWindows(server, token string) string {
	return fmt.Sprintf(`# PC-Inventory Agent für Windows – in einer PowerShell als Administrator ausführen
$Server = "%s"
$Token  = "%s"

$dir = "$env:ProgramFiles\PC-Inventory"
New-Item -ItemType Directory -Force $dir, "$env:ProgramData\PC-Inventory" | Out-Null
Invoke-WebRequest "$Server/api/v1/agents/windows-amd64" -OutFile "$dir\agent.exe"

@"
server_url: "$Server"
enrollment_token: "$Token"
interval: "5m"
state_path: "C:/ProgramData/PC-Inventory/agent-state.json"
"@ | Set-Content "$env:ProgramData\PC-Inventory\agent.yaml" -Encoding ascii

& "$dir\agent.exe" -config "$env:ProgramData\PC-Inventory\agent.yaml" install
& "$dir\agent.exe" -config "$env:ProgramData\PC-Inventory\agent.yaml" start
`, server, token)
}

func installScriptUnix(osKind, server, token string) string {
	archCase := "  x86_64) PLAT=linux-amd64 ;;\n  aarch64|arm64) PLAT=linux-arm64 ;;"
	if osKind == "mac" {
		archCase = "  arm64) PLAT=darwin-arm64 ;;\n  x86_64) PLAT=darwin-amd64 ;;"
	}
	return fmt.Sprintf(`#!/usr/bin/env bash
# PC-Inventory Agent – mit Root-Rechten ausführen
set -euo pipefail
SERVER="%s"
TOKEN="%s"

case "$(uname -m)" in
%s
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
sudo /usr/local/bin/pc-inventory-agent -config /etc/pc-inventory/agent.yaml start
`, server, token, archCase)
}
