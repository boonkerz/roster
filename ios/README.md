# Roster iOS

Native SwiftUI-App für Roster: Statusüberblick + Eingreifen unterwegs (Neustart,
Skript, Check/Task, Wake-on-LAN, Update-Scan, Live-**Terminal**). Push-Alarme laufen
weiter über Pushover (kein eigenes APNs in v1).

## Architektur

- **Auth:** Login (Passwort → ggf. TOTP) erzeugt serverseitig eine Session; die App
  tauscht sie sofort gegen ein langlebiges **User-API-Token** (`POST /api/v1/auth/api-tokens`,
  seit Server 0.10.0) und legt es im **Keychain** ab. Danach jeder Request per
  `Authorization: Bearer <token>`. Kein 12‑Stunden-Relogin.
- **Netzwerk:** `APIClient` (async/await, `URLSession`). Aktionen werden eingereiht
  (`{command_id}`) und über `GET /api/v1/commands/{id}` bis `status=done` gepollt.
- **Terminal:** `URLSessionWebSocketTask` gegen `…/devices/{id}/terminal?shell=&runas=`
  mit Bearer-Header. Rohe I/O als Binär-Frames, `resize`/`exit` als Text-Frames –
  identisches Protokoll wie die Web-UI (`web/src/components/DeviceTerminal.tsx`).
  UI über **SwiftTerm**.

## Dateien

```
RosterApp/
  RosterApp.swift          @main App
  State/AppState.swift      Anmeldezustand + Login/TOTP/Token-Mint
  Networking/APIClient.swift REST-Client + Command-Polling
  Networking/Models.swift    Decodable-Modelle
  Support/Keychain.swift     Token/Server im Keychain
  Views/RootView.swift       Login vs. Tabs (Übersicht/Geräte/Mehr)
  Views/LoginView.swift      Login + TOTP
  Views/DashboardView.swift  Kacheln + letzte Ereignisse
  Views/DevicesView.swift    Geräteliste + Suche
  Views/DeviceDetailView.swift  Detail + Aktionen + Skriptauswahl
  Views/TerminalView.swift   SwiftTerm + WebSocket-Bridge
```

## Build

Voraussetzung: Xcode 15+, iOS 16+.

1. Projekt erzeugen (empfohlen via [XcodeGen](https://github.com/yonaskolb/XcodeGen)):
   ```sh
   brew install xcodegen
   cd ios && xcodegen generate && open Roster.xcodeproj
   ```
   Ohne XcodeGen: neues iOS-App-Target anlegen, `RosterApp/` hinzufügen und die
   SwiftTerm-SPM-Abhängigkeit (`https://github.com/migueldeicaza/SwiftTerm`) einbinden.
2. In den Target-Einstellungen **Signing/Team** setzen (Bundle-ID
   `com.printshopcreator.roster` in `project.yml` anpassbar).
3. Bauen/ausführen. Beim Start: Server-URL (`https://…`), Benutzer, Passwort, ggf. TOTP.

> **SwiftTerm-Hinweis:** Die `TerminalViewDelegate`-Signaturen können je nach
> SwiftTerm-Version minimal abweichen. Falls der Compiler fehlende/abweichende
> Protokoll-Anforderungen meldet, in `Views/TerminalView.swift` (Bridge) angleichen.
> Version in `project.yml` (`from: "1.2.0"`) bei Bedarf pinnen.

## TestFlight / CI

Der Server ist öffentlich per HTTPS erreichbar → App funktioniert unterwegs ohne VPN.
Für den Upload die vorhandene GitHub-Action→TestFlight-Pipeline übernehmen; im
Monorepo als eigener Workflow, der nur auf `ios/**` triggert. Secrets (App Store
Connect API-Key etc.) wie bei der bestehenden App hinterlegen.

## Bewusst nicht in v1

- Remote-Desktop/VNC (eigener RFB-Client) – separat.
- Eigenes APNs-Push – Alarme kommen über Pushover; die App ist zum Nachschauen/Eingreifen.
- Token-Verwaltung im Web-Frontend – die App mintet/​widerruft ihr Token selbst
  (Abmelden widerruft es serverseitig).
