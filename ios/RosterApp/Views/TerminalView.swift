import SwiftUI
import SwiftTerm

// TerminalView öffnet ein Live-Terminal zum Gerät über denselben WebSocket wie die
// Web-UI: rohe I/O als Binär-Frames, Steuerung (resize/exit) als Text-Frames. Auth
// erfolgt über den Bearer-API-Token im Handshake-Header (requireUser akzeptiert ihn).
struct TerminalView: View {
    @EnvironmentObject var app: AppState
    let deviceId: String
    let os: String

    @Environment(\.dismiss) private var dismiss
    @State private var runas = "system"
    @State private var status = "verbinde…"
    @State private var reconnectToken = 0

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                Picker("Rechte", selection: $runas) {
                    Text(isWindows ? "als SYSTEM" : "als root").tag("system")
                    Text("als Benutzer").tag("user")
                }
                .pickerStyle(.segmented)
                .padding(8)
                .onChange(of: runas) { _ in reconnectToken += 1 } // neu verbinden

                SwiftTermContainer(
                    deviceId: deviceId,
                    os: os,
                    runas: runas,
                    root: app.api?.root ?? URL(string: "https://invalid")!,
                    token: app.api?.token ?? "",
                    status: $status
                )
                .id(reconnectToken) // Wechsel der runas-Auswahl → View neu aufbauen
                .ignoresSafeArea(.container, edges: .bottom)
            }
            .navigationTitle("Terminal")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Schließen") { dismiss() }
                }
                ToolbarItem(placement: .principal) {
                    Text(status).font(.caption).foregroundStyle(.secondary)
                }
            }
        }
    }

    private var isWindows: Bool { os.lowercased().contains("win") }
}

// SwiftTermContainer bindet SwiftTerms UIKit-Terminal in SwiftUI ein.
struct SwiftTermContainer: UIViewRepresentable {
    let deviceId: String
    let os: String
    let runas: String
    let root: URL
    let token: String
    @Binding var status: String

    func makeCoordinator() -> TerminalBridge {
        TerminalBridge(status: $status)
    }

    func makeUIView(context: Context) -> SwiftTerm.TerminalView {
        let tv = SwiftTerm.TerminalView(frame: .zero)
        tv.terminalDelegate = context.coordinator
        context.coordinator.terminal = tv
        context.coordinator.connect(root: root, deviceId: deviceId, os: os, runas: runas, token: token)
        return tv
    }

    func updateUIView(_ uiView: SwiftTerm.TerminalView, context: Context) {}

    static func dismantleUIView(_ uiView: SwiftTerm.TerminalView, coordinator: TerminalBridge) {
        coordinator.disconnect()
    }
}

// TerminalBridge ist der TerminalViewDelegate und hält die WebSocket-Verbindung.
// Hinweis: Die genauen TerminalViewDelegate-Signaturen können je nach SwiftTerm-
// Version leicht abweichen – Xcode meldet fehlende/abweichende Protokoll-Anforderungen.
final class TerminalBridge: NSObject, TerminalViewDelegate {
    weak var terminal: SwiftTerm.TerminalView?
    private var socket: URLSessionWebSocketTask?
    private var session: URLSession?
    private var open = false
    private let statusBinding: Binding<String>

    init(status: Binding<String>) {
        self.statusBinding = status
    }

    func connect(root: URL, deviceId: String, os: String, runas: String, token: String) {
        let shell = os.lowercased().contains("win") ? "cmd" : "shell"
        guard var comps = URLComponents(
            url: root.appendingPathComponent("api/v1/devices/\(deviceId)/terminal"),
            resolvingAgainstBaseURL: false) else { return }
        comps.scheme = (comps.scheme == "https") ? "wss" : "ws"
        comps.queryItems = [
            URLQueryItem(name: "shell", value: shell),
            URLQueryItem(name: "runas", value: runas),
        ]
        guard let url = comps.url else { return }

        var req = URLRequest(url: url)
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")

        let session = URLSession(configuration: .ephemeral)
        self.session = session
        let task = session.webSocketTask(with: req)
        self.socket = task
        open = true
        setStatus("verbunden")
        task.resume()
        receiveLoop()
    }

    func disconnect() {
        open = false
        socket?.cancel(with: .goingAway, reason: nil)
        socket = nil
        session?.invalidateAndCancel()
        session = nil
    }

    private func receiveLoop() {
        socket?.receive { [weak self] result in
            guard let self else { return }
            switch result {
            case .failure:
                self.setStatus("getrennt")
                self.open = false
            case let .success(message):
                switch message {
                case let .data(data):
                    let bytes = [UInt8](data)
                    DispatchQueue.main.async { self.terminal?.feed(byteArray: bytes[...]) }
                case let .string(text):
                    self.handleControl(text)
                @unknown default:
                    break
                }
                if self.open { self.receiveLoop() }
            }
        }
    }

    private func handleControl(_ text: String) {
        guard let data = text.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              obj["type"] as? String == "exit" else { return }
        let code = obj["code"] as? Int ?? -1
        DispatchQueue.main.async {
            self.terminal?.feed(text: "\r\n[Sitzung beendet – Exit \(code)]\r\n")
            self.setStatus("beendet")
        }
    }

    private func setStatus(_ s: String) {
        DispatchQueue.main.async { self.statusBinding.wrappedValue = s }
    }

    // MARK: - TerminalViewDelegate

    func send(source: SwiftTerm.TerminalView, data: ArraySlice<UInt8>) {
        guard open else { return }
        socket?.send(.data(Data(data))) { _ in }
    }

    func sizeChanged(source: SwiftTerm.TerminalView, newCols: Int, newRows: Int) {
        guard open else { return }
        let json = "{\"type\":\"resize\",\"cols\":\(newCols),\"rows\":\(newRows)}"
        socket?.send(.string(json)) { _ in }
    }

    func setTerminalTitle(source: SwiftTerm.TerminalView, title: String) {}
    func scrolled(source: SwiftTerm.TerminalView, position: Double) {}
    func hostCurrentDirectoryUpdate(source: SwiftTerm.TerminalView, directory: String?) {}
    func clipboardCopy(source: SwiftTerm.TerminalView, content: Data) {}
    func clipboardRead(source: SwiftTerm.TerminalView) -> Data? { nil }
    func requestOpenLink(source: SwiftTerm.TerminalView, link: String, params: [String: String]) {}
    func bell(source: SwiftTerm.TerminalView) {}
    func iTermContent(source: SwiftTerm.TerminalView, content: ArraySlice<UInt8>) {}
    func rangeChanged(source: SwiftTerm.TerminalView, startY: Int, endY: Int) {}
}
