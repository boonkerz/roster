import SwiftUI

struct ActionResult: Identifiable {
    let id = UUID()
    let title: String
    let message: String
}

struct DeviceDetailView: View {
    @EnvironmentObject var app: AppState
    let deviceId: String

    @State private var device: Device?
    @State private var scripts: [Script] = []
    @State private var showScriptPicker = false
    @State private var showTerminal = false
    @State private var runningAction = false
    @State private var actionResult: ActionResult?
    @State private var error: String?

    var body: some View {
        List {
            if let d = device {
                Section {
                    LabeledContent("Status", value: d.status ?? "—")
                    LabeledContent("System", value: "\(d.os ?? "") \(d.osVersion ?? "")")
                    if let ls = d.lastSeen { LabeledContent("Zuletzt gesehen", value: ls) }
                    if let c = d.clientName { LabeledContent("Kunde", value: c) }
                }

                Section("Aktionen") {
                    asyncButton("Neustart", "arrow.clockwise") {
                        try await app.api!.runAndWait("api/v1/devices/\(deviceId)/reboot")
                    }
                    Button {
                        showScriptPicker = true
                    } label: {
                        Label("Skript ausführen", systemImage: "terminal")
                    }
                    asyncButton("Updates suchen", "arrow.down.circle") {
                        try await app.api!.runAndWait("api/v1/devices/\(deviceId)/scan-updates")
                    }
                    asyncButton("Wake-on-LAN", "power") {
                        // WOL läuft evtl. als Server-Broadcast ohne command_id → nicht pollen.
                        let _: CommandRef = try await app.api!.postEmpty("api/v1/devices/\(deviceId)/wake")
                        return nil
                    }
                    Button {
                        showTerminal = true
                    } label: {
                        Label("Terminal", systemImage: "chevron.left.forwardslash.chevron.right")
                    }
                }

                if let checks = d.checkResults, !checks.isEmpty {
                    Section("Checks") {
                        ForEach(checks) { c in
                            HStack(alignment: .top, spacing: 10) {
                                Image(systemName: c.status == "passing" ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
                                    .foregroundStyle(c.status == "passing" ? .green : .orange)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(c.name ?? c.checkId)
                                    if let o = c.output, !o.isEmpty {
                                        Text(o).font(.caption).foregroundStyle(.secondary).lineLimit(3)
                                    }
                                }
                                Spacer()
                                Button {
                                    Task { await perform("Check: \(c.name ?? c.checkId)") {
                                        try await app.api!.runAndWait("api/v1/devices/\(deviceId)/checks/\(c.checkId)/run")
                                    } }
                                } label: {
                                    Image(systemName: "arrow.clockwise")
                                }
                                .buttonStyle(.borderless)
                            }
                        }
                    }
                }
            } else if let error {
                Text(error).foregroundStyle(.red)
            } else {
                HStack { Spacer(); ProgressView(); Spacer() }
            }
        }
        .navigationTitle(device?.hostname ?? "Gerät")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
        .refreshable { await load() }
        .overlay {
            if runningAction {
                ProgressView("Aktion läuft …")
                    .padding()
                    .background(.ultraThinMaterial)
                    .clipShape(RoundedRectangle(cornerRadius: 12))
            }
        }
        .sheet(isPresented: $showScriptPicker) { scriptPicker }
        .fullScreenCover(isPresented: $showTerminal) {
            if let d = device {
                TerminalView(deviceId: deviceId, os: d.os ?? "")
            }
        }
        .alert(item: $actionResult) { r in
            Alert(title: Text(r.title), message: Text(r.message), dismissButton: .default(Text("OK")))
        }
    }

    // MARK: - Skriptauswahl

    private var scriptPicker: some View {
        NavigationStack {
            List(applicableScripts) { s in
                Button(s.name) {
                    showScriptPicker = false
                    Task {
                        await perform("Skript: \(s.name)") {
                            try await app.api!.runAndWait("api/v1/devices/\(deviceId)/run", RunScriptBody(scriptId: s.id))
                        }
                    }
                }
            }
            .navigationTitle("Skript wählen")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Abbrechen") { showScriptPicker = false }
                }
            }
            .task { await loadScripts() }
        }
    }

    private var applicableScripts: [Script] {
        let os = (device?.os ?? "").lowercased()
        return scripts.filter { s in
            guard let p = s.platforms, !p.isEmpty else { return true }
            return p.contains { os.contains($0.lowercased()) }
        }
    }

    // MARK: - Aktionen

    private func asyncButton(_ title: String, _ icon: String,
                             _ run: @escaping () async throws -> Command?) -> some View {
        Button {
            Task { await perform(title, run) }
        } label: {
            Label(title, systemImage: icon)
        }
    }

    @MainActor
    private func perform(_ title: String, _ run: @escaping () async throws -> Command?) async {
        runningAction = true
        defer { runningAction = false }
        do {
            let cmd = try await run()
            if let cmd {
                let code = cmd.exitCode ?? 0
                let head = code == 0 ? "Erfolgreich" : "Fehlgeschlagen (Exit \(code))"
                let out = (cmd.output ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                actionResult = ActionResult(title: title, message: out.isEmpty ? head : "\(head)\n\n\(out)")
            } else {
                actionResult = ActionResult(title: title, message: "Ausgelöst.")
            }
            await load()
        } catch {
            actionResult = ActionResult(title: title, message: error.localizedDescription)
        }
    }

    @MainActor
    private func load() async {
        guard let api = app.api else { return }
        do {
            device = try await api.get("api/v1/devices/\(deviceId)")
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    @MainActor
    private func loadScripts() async {
        guard let api = app.api else { return }
        scripts = (try? await api.get("api/v1/scripts")) ?? []
    }
}
