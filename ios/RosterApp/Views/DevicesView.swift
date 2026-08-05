import SwiftUI

struct DevicesView: View {
    @EnvironmentObject var app: AppState
    @State private var devices: [Device] = []
    @State private var query = ""
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
            .refreshable { await load() }
            .task { await load() }
            .overlay {
                if let error, devices.isEmpty {
                    Text(error).foregroundStyle(.red)
                }
            }
        }
    }

    private var filtered: [Device] {
        query.isEmpty ? devices : devices.filter { $0.hostname.localizedCaseInsensitiveContains(query) }
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
