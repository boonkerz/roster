import SwiftUI

// RootView schaltet je nach Anmeldezustand zwischen Login, TOTP und der Haupt-App
// und legt bei aktivierter App-Sperre den Sperrbildschirm darüber.
struct RootView: View {
    @EnvironmentObject var app: AppState
    @EnvironmentObject var lock: AppLock
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        ZStack {
            switch app.phase {
            case .login:
                LoginView()
            case .totp:
                TOTPView()
            case .authed:
                MainTabView()
            }

            if app.phase == .authed && lock.enabled && lock.locked {
                LockView()
            }
        }
        .onChange(of: scenePhase) { phase in
            if phase == .background { lock.lockIfEnabled() }
        }
    }
}

// LockView verdeckt die App, bis biometrisch entsperrt wurde. Beim Erscheinen wird
// die Abfrage automatisch ausgelöst.
struct LockView: View {
    @EnvironmentObject var lock: AppLock

    var body: some View {
        ZStack {
            Color(.systemBackground).ignoresSafeArea()
            VStack(spacing: 18) {
                Image(systemName: "lock.fill")
                    .font(.system(size: 44))
                    .foregroundStyle(.secondary)
                Text("Roster ist gesperrt")
                    .font(.headline)
                Button {
                    Task { await lock.unlock() }
                } label: {
                    Label("Mit \(Biometrics.typeName()) entsperren", systemImage: "faceid")
                }
                .buttonStyle(.borderedProminent)
            }
        }
        .task { await lock.unlock() }
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
    @EnvironmentObject var lock: AppLock

    var body: some View {
        NavigationStack {
            List {
                Section("Server") {
                    Text(app.serverURL)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Section("Sicherheit") {
                    if Biometrics.available() {
                        Toggle("App mit \(Biometrics.typeName()) sperren", isOn: Binding(
                            get: { lock.enabled },
                            set: { lock.setEnabled($0) }
                        ))
                    } else {
                        Text("Biometrie auf diesem Gerät nicht verfügbar")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
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
