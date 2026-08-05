import SwiftUI

struct DashboardView: View {
    @EnvironmentObject var app: AppState
    @State private var summary: DashboardSummary?
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                if let s = summary {
                    Section("Geräte") {
                        stat("Online", s.devicesOnline, .green)
                        stat("Offline", s.devicesOffline, .red)
                        stat("Unbekannt", s.devicesUnknown, .gray)
                    }
                    Section("Probleme") {
                        stat("Fehlgeschlagene Checks", s.failingChecks, .orange)
                        stat("Offene Patches", s.pendingPatches, .blue)
                        stat("Schwachstellen", s.vulnerabilities, .purple)
                    }
                    if let events = s.recentEvents, !events.isEmpty {
                        Section("Letzte Ereignisse") {
                            ForEach(events) { e in
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(e.checkName).font(.subheadline)
                                    Text("\(e.hostname ?? "—") · \(e.newStatus)")
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
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
            .navigationTitle("Übersicht")
            .refreshable { await load() }
            .task { await load() }
        }
    }

    private func stat(_ label: String, _ value: Int, _ color: Color) -> some View {
        HStack {
            Text(label)
            Spacer()
            Text("\(value)").bold().foregroundStyle(color)
        }
    }

    @MainActor
    private func load() async {
        guard let api = app.api else { return }
        do {
            summary = try await api.get("api/v1/dashboard")
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}
