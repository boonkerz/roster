import SwiftUI

enum DeviceFilter: String, CaseIterable, Identifiable {
    case all, failing, offline, online
    var id: String { rawValue }
    var label: String {
        switch self {
        case .all: return "Alle"
        case .failing: return "Check-Fehler"
        case .offline: return "Offline"
        case .online: return "Online"
        }
    }
}

struct DevicesView: View {
    @EnvironmentObject var app: AppState
    @State private var devices: [Device] = []
    @State private var query = ""
    @State private var filter: DeviceFilter = .all
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List(filtered) { d in
                NavigationLink(value: d.id) {
                    HStack(spacing: 10) {
                        Circle().fill(color(d.status)).frame(width: 10, height: 10)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(d.hostname)
                            Text([d.os, d.siteName].compactMap { $0 }.joined(separator: " · "))
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        if let f = d.checksFailing, f > 0 {
                            Text("\(f)")
                                .font(.caption).bold()
                                .padding(.horizontal, 7).padding(.vertical, 2)
                                .background(Color.red.opacity(0.2))
                                .clipShape(Capsule())
                        }
                    }
                }
            }
            .navigationTitle("Geräte")
            .navigationDestination(for: String.self) { id in
                DeviceDetailView(deviceId: id)
            }
            .searchable(text: $query)
            .safeAreaInset(edge: .top) { filterBar }
            .refreshable { await load() }
            .task { await load() }
            .overlay {
                if let error, devices.isEmpty {
                    Text(error).foregroundStyle(.red)
                } else if devices.isEmpty == false && filtered.isEmpty {
                    Text("Keine Geräte in diesem Filter")
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    // Schnellfilter-Leiste (horizontal scrollbare Chips mit Anzahl).
    private var filterBar: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(DeviceFilter.allCases) { f in
                    let active = filter == f
                    Button {
                        filter = f
                    } label: {
                        HStack(spacing: 5) {
                            Text(f.label)
                            Text("\(count(for: f))")
                                .font(.caption2)
                                .opacity(0.75)
                        }
                        .font(.subheadline)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(active ? Color.accentColor : Color(.secondarySystemBackground))
                        .foregroundStyle(active ? Color.white : Color.primary)
                        .clipShape(Capsule())
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 6)
        }
        .background(.bar)
    }

    private var filtered: [Device] {
        devices.filter { d in
            let matchesSearch = query.isEmpty || d.hostname.localizedCaseInsensitiveContains(query)
            return matchesSearch && matches(d, filter)
        }
    }

    private func matches(_ d: Device, _ f: DeviceFilter) -> Bool {
        switch f {
        case .all: return true
        case .failing: return (d.checksFailing ?? 0) > 0
        case .offline: return d.status == "offline"
        case .online: return d.status == "online"
        }
    }

    private func count(for f: DeviceFilter) -> Int {
        devices.filter { matches($0, f) }.count
    }

    private func color(_ status: String?) -> Color {
        switch status {
        case "online": return .green
        case "offline": return .red
        case "unmanaged": return .gray
        default: return .yellow
        }
    }

    @MainActor
    private func load() async {
        guard let api = app.api else { return }
        do {
            devices = try await api.get("api/v1/devices")
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}
