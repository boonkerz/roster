# Roster – Build- und Entwicklungs-Tasks.
# modernc-SQLite + gopsutil sind CGo-frei, daher CGO_ENABLED=0 für statische Cross-Builds.

# Versionsnummer aus der VERSION-Datei (Single Source of Truth); überschreibbar via `make VERSION=x`.
VERSION ?= $(shell cat $(CURDIR)/VERSION 2>/dev/null || echo 0.0.0-dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := CGO_ENABLED=0
BIN := bin
AGENT_EMBED := internal/server/agentdist/bin

.PHONY: help web server agent agents-embed viewer tray install-tray install-tray-autostart uninstall-tray build test vet tidy clean run-server cross

help: ## Diese Hilfe anzeigen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n",$$1,$$2}'

web: ## Frontend bauen (nach web/dist, wird ins Server-Binary eingebettet)
	cd web && npm install && npm run build

agents-embed: ## Agent-Binaries für alle Plattformen ins Server-Embed cross-kompilieren
	mkdir -p $(AGENT_EMBED)
	$(GOFLAGS) GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(AGENT_EMBED)/agent-linux-amd64       ./cmd/agent
	$(GOFLAGS) GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(AGENT_EMBED)/agent-linux-arm64       ./cmd/agent
	$(GOFLAGS) GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(AGENT_EMBED)/agent-darwin-amd64      ./cmd/agent
	$(GOFLAGS) GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(AGENT_EMBED)/agent-darwin-arm64      ./cmd/agent
	$(GOFLAGS) GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(AGENT_EMBED)/agent-windows-amd64.exe ./cmd/agent
	$(GOFLAGS) GOOS=freebsd GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(AGENT_EMBED)/agent-freebsd-amd64      ./cmd/agent

server: web agents-embed ## Server-Binary bauen (inkl. Frontend + Agent-Downloads)
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o $(BIN)/server ./cmd/server

agent: ## Agent-Binary für die aktuelle Plattform bauen
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o $(BIN)/agent ./cmd/agent

viewer: ## Nativer Fernsteuerungs-Viewer für die aktuelle Plattform (SDL3, cgo-frei)
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-viewer ./cmd/viewer

tray: ## Taskleisten-App für die aktuelle Plattform bauen (SDL3, cgo-frei)
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-tray ./cmd/tray

tray-cross: ## Taskleisten-App für Linux/Windows/macOS bauen (SDL3 wird zur Laufzeit geladen)
	$(GOFLAGS) GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-tray-linux-amd64      ./cmd/tray
	$(GOFLAGS) GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-tray-windows-amd64.exe ./cmd/tray
	$(GOFLAGS) GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-tray-darwin-arm64     ./cmd/tray

# Benutzer-Installation der Taskleisten-App (kein root nötig) – dünne Hülle um
# `roster-tray --install`, das auch ohne Quellbaum funktioniert (Download aus den
# Einstellungen). PREFIX überschreibbar, z. B. `sudo make install-tray PREFIX=/usr/local`.
PREFIX ?= $(HOME)/.local

install-tray: tray ## roster-tray + Desktop-Eintrag/Symbol installieren (Anwendungsmenü)
	$(BIN)/roster-tray --install --prefix "$(PREFIX)"

install-tray-autostart: tray ## Zusätzlich beim Anmelden ins Tray starten
	$(BIN)/roster-tray --install --autostart --prefix "$(PREFIX)"

uninstall-tray: ## Taskleisten-App und Desktop-Eintrag wieder entfernen
	$(BIN)/roster-tray --uninstall --prefix "$(PREFIX)"

viewer-embed: ## Linux-Viewer ins Server-Embed bauen (cgo-frei; SDL3 aus dem System)
	mkdir -p internal/server/viewerdist/bin
	$(GOFLAGS) GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o internal/server/viewerdist/bin/roster-viewer-linux-amd64 ./cmd/viewer

viewer-embed-arm64: ## Linux-ARM64-Viewer ins Server-Embed bauen (Raspberry Pi & Co.)
	mkdir -p internal/server/viewerdist/bin
	$(GOFLAGS) GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o internal/server/viewerdist/bin/roster-viewer-linux-arm64 ./cmd/viewer

# SDL3-Laufzeitbibliotheken zum Bündeln für Windows/macOS (der Viewer selbst ist
# cgo-frei; SDL3 wird zur Laufzeit geladen). Pfade überschreibbar via `make VAR=…`.
SDL3_WIN_DLL   ?= third_party/sdl3/SDL3.dll
SDL3_MAC_DYLIB ?= third_party/sdl3/libSDL3.dylib
SDL3_VERSION   ?= 3.4.12

fetch-sdl3: ## SDL3-Windows-DLL (zum Bündeln) herunterladen und entpacken
	mkdir -p third_party/sdl3
	curl -fsSL -o /tmp/sdl3-win.zip "https://github.com/libsdl-org/SDL/releases/download/release-$(SDL3_VERSION)/SDL3-$(SDL3_VERSION)-win32-x64.zip"
	unzip -o -j /tmp/sdl3-win.zip SDL3.dll -d third_party/sdl3
	rm -f /tmp/sdl3-win.zip

viewer-embed-windows: ## Windows-Viewer (cgo-frei) + gebündelte SDL3.dll als ZIP ins Embed
	mkdir -p internal/server/viewerdist/bin
	$(GOFLAGS) GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-viewer.exe ./cmd/viewer
	cp "$(SDL3_WIN_DLL)" $(BIN)/SDL3.dll
	cd $(BIN) && rm -f roster-viewer-windows-amd64.zip && zip -j roster-viewer-windows-amd64.zip roster-viewer.exe SDL3.dll
	mv $(BIN)/roster-viewer-windows-amd64.zip internal/server/viewerdist/bin/roster-viewer-windows-amd64.zip
	rm -f $(BIN)/roster-viewer.exe $(BIN)/SDL3.dll

viewer-embed-darwin: ## macOS-Viewer (cgo-frei) + gebündelte libSDL3.dylib als ZIP ins Embed
	mkdir -p internal/server/viewerdist/bin
	$(GOFLAGS) GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-viewer ./cmd/viewer
	cp "$(SDL3_MAC_DYLIB)" $(BIN)/libSDL3.dylib
	cd $(BIN) && rm -f roster-viewer-darwin-arm64.zip && zip -j roster-viewer-darwin-arm64.zip roster-viewer libSDL3.dylib
	mv $(BIN)/roster-viewer-darwin-arm64.zip internal/server/viewerdist/bin/roster-viewer-darwin-arm64.zip
	rm -f $(BIN)/roster-viewer $(BIN)/libSDL3.dylib

# Taskleisten-App (roster-tray) ins Server-Embed – gleiches Schema wie der Viewer,
# Download in den Einstellungen unter „Downloads“.
tray-embed: ## Linux-Taskleisten-App ins Server-Embed bauen (cgo-frei; SDL3 aus dem System)
	mkdir -p internal/server/traydist/bin
	$(GOFLAGS) GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o internal/server/traydist/bin/roster-tray-linux-amd64 ./cmd/tray

tray-embed-arm64: ## Linux-ARM64-Taskleisten-App ins Server-Embed bauen (Raspberry Pi & Co.)
	mkdir -p internal/server/traydist/bin
	$(GOFLAGS) GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o internal/server/traydist/bin/roster-tray-linux-arm64 ./cmd/tray


tray-embed-windows: ## Windows-Taskleisten-App (cgo-frei) + gebündelte SDL3.dll als ZIP ins Embed
	mkdir -p internal/server/traydist/bin
	$(GOFLAGS) GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-tray.exe ./cmd/tray
	cp "$(SDL3_WIN_DLL)" $(BIN)/SDL3.dll
	cd $(BIN) && rm -f roster-tray-windows-amd64.zip && zip -j roster-tray-windows-amd64.zip roster-tray.exe SDL3.dll
	mv $(BIN)/roster-tray-windows-amd64.zip internal/server/traydist/bin/roster-tray-windows-amd64.zip
	rm -f $(BIN)/roster-tray.exe $(BIN)/SDL3.dll

tray-embed-darwin: ## macOS-Taskleisten-App (cgo-frei) + gebündelte libSDL3.dylib als ZIP ins Embed
	mkdir -p internal/server/traydist/bin
	$(GOFLAGS) GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/roster-tray ./cmd/tray
	cp "$(SDL3_MAC_DYLIB)" $(BIN)/libSDL3.dylib
	cd $(BIN) && rm -f roster-tray-darwin-arm64.zip && zip -j roster-tray-darwin-arm64.zip roster-tray libSDL3.dylib
	mv $(BIN)/roster-tray-darwin-arm64.zip internal/server/traydist/bin/roster-tray-darwin-arm64.zip
	rm -f $(BIN)/roster-tray $(BIN)/libSDL3.dylib

build: server agent ## Server und Agent bauen

test: ## Tests ausführen
	go test ./...

vet: ## go vet
	go vet ./...

tidy: ## go mod tidy
	go mod tidy

run-server: ## Server lokal starten (SQLite, ohne TLS – nur Entwicklung)
	ROSTER_DB=sqlite://./inventory.db ROSTER_ADDR=:8443 ROSTER_SECURE_COOKIE=false go run ./cmd/server run

# Cross-Compile des Agents für alle Zielplattformen.
cross: web ## Agent für Windows/Linux/macOS und Server für Linux/Windows bauen
	$(GOFLAGS) GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/agent-windows-amd64.exe ./cmd/agent
	$(GOFLAGS) GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/agent-linux-amd64       ./cmd/agent
	$(GOFLAGS) GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/agent-darwin-arm64      ./cmd/agent
	$(GOFLAGS) GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/server-linux-amd64      ./cmd/server
	$(GOFLAGS) GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/server-windows-amd64.exe ./cmd/server

clean: ## Build-Artefakte entfernen
	rm -rf $(BIN) web/dist/assets
	rm -f $(AGENT_EMBED)/agent-*
