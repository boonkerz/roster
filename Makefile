# PC-Inventory – Build- und Entwicklungs-Tasks.
# modernc-SQLite + gopsutil sind CGo-frei, daher CGO_ENABLED=0 für statische Cross-Builds.

# Versionsnummer aus der VERSION-Datei (Single Source of Truth); überschreibbar via `make VERSION=x`.
VERSION ?= $(shell cat $(CURDIR)/VERSION 2>/dev/null || echo 0.0.0-dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOFLAGS := CGO_ENABLED=0
BIN := bin
AGENT_EMBED := internal/server/agentdist/bin

.PHONY: help web server agent agents-embed viewer build test vet tidy clean run-server cross

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

server: web agents-embed ## Server-Binary bauen (inkl. Frontend + Agent-Downloads)
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o $(BIN)/server ./cmd/server

agent: ## Agent-Binary für die aktuelle Plattform bauen
	$(GOFLAGS) go build -ldflags "$(LDFLAGS)" -o $(BIN)/agent ./cmd/agent

viewer: ## Nativer Fernsteuerungs-Viewer (Linux, braucht SDL2 + cgo)
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BIN)/pcinv-viewer ./cmd/viewer

build: server agent ## Server und Agent bauen

test: ## Tests ausführen
	go test ./...

vet: ## go vet
	go vet ./...

tidy: ## go mod tidy
	go mod tidy

run-server: ## Server lokal starten (SQLite, ohne TLS – nur Entwicklung)
	PCINV_DB=sqlite://./inventory.db PCINV_ADDR=:8443 PCINV_SECURE_COOKIE=false go run ./cmd/server run

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
