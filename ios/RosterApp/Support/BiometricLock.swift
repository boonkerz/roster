import Foundation
import LocalAuthentication

// Biometrie-Hilfen (Face ID / Touch ID) über das LocalAuthentication-Framework.
// Es ist keine Capability nötig – nur NSFaceIDUsageDescription in der Info.plist.
enum Biometrics {
    static func available() -> Bool {
        var err: NSError?
        return LAContext().canEvaluatePolicy(.deviceOwnerAuthentication, error: &err)
    }

    static func typeName() -> String {
        let ctx = LAContext()
        _ = ctx.canEvaluatePolicy(.deviceOwnerAuthentication, error: nil)
        switch ctx.biometryType {
        case .faceID: return "Face ID"
        case .touchID: return "Touch ID"
        default: return "Code"
        }
    }

    // Fragt Biometrie ab (mit Gerätecode als Fallback). Liefert true bei Erfolg.
    static func authenticate(reason: String) async -> Bool {
        let ctx = LAContext()
        ctx.localizedFallbackTitle = "Code eingeben"
        return await withCheckedContinuation { cont in
            ctx.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason) { ok, _ in
                cont.resume(returning: ok)
            }
        }
    }
}

// AppLock steuert die optionale App-Sperre: bei Aktivierung ist die App nach Start
// und nach jedem Wechsel in den Hintergrund gesperrt, bis biometrisch entsperrt wird.
@MainActor
final class AppLock: ObservableObject {
    private static let key = "lock.enabled"

    @Published private(set) var enabled: Bool
    @Published var locked: Bool

    init() {
        let on = UserDefaults.standard.bool(forKey: Self.key)
        enabled = on
        locked = on // beim Start gesperrt, falls aktiviert
    }

    func setEnabled(_ on: Bool) {
        enabled = on
        UserDefaults.standard.set(on, forKey: Self.key)
        locked = false // beim Umschalten nicht sofort aussperren
    }

    func lockIfEnabled() {
        if enabled { locked = true }
    }

    func unlock() async {
        if await Biometrics.authenticate(reason: "Roster entsperren") {
            locked = false
        }
    }
}
