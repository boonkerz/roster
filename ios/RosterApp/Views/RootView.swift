import SwiftUI

// RootView schaltet je nach Anmeldezustand zwischen Login, TOTP und der Haupt-App.
struct RootView: View {
    @EnvironmentObject var app: AppState

    var body: some View {
        switch app.phase {
        case .login:
            LoginView()
        case .totp:
            TOTPView()
        case .authed:
            MainTabView()
        }
    }
}

struct MainTabView: View {
    var body: some View {
        TabView {
            DashboardView()
                .tabItem { Label("Übersicht", systemImage: "gauge") }
            DevicesView()
                .tabItem { Label("Geräte", systemImage: "desktopcomputer") }
            SettingsView()
                .tabItem { Label("Mehr", systemImage: "ellipsis.circle") }
        }
    }
}

struct SettingsView: View {
    @EnvironmentObject var app: AppState

    var body: some View {
        NavigationStack {
            List {
                Section("Server") {
                    Text(app.serverURL)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Section {
                    Button("Abmelden", role: .destructive) { app.logout() }
                }
                Section {
                    Text("Roster iOS · 0.1")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .navigationTitle("Mehr")
        }
    }
}
