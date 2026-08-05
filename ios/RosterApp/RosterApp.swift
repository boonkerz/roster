import SwiftUI

// Einstiegspunkt der Roster-iOS-App. Der globale App-Zustand (Anmeldung + API-Client)
// wird als EnvironmentObject bereitgestellt.
@main
struct RosterApp: App {
    @StateObject private var app = AppState()
    @StateObject private var lock = AppLock()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(app)
                .environmentObject(lock)
        }
    }
}
