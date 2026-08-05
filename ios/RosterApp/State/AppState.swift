import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

// AppState hält den globalen Anmeldezustand und den authentifizierten API-Client.
// Ablauf: Login (Passwort) → ggf. TOTP → API-Token minten → im Keychain sichern.
@MainActor
final class AppState: ObservableObject {
    enum Phase: Equatable {
        case login
        case totp(pending: String)
        case authed
    }

    @Published var phase: Phase = .login
    @Published var serverURL: String = ""
    @Published var busy = false
    @Published var errorMessage: String?

    private(set) var api: APIClient?
    private var pendingClient: APIClient? // hält den Login-Cookie bis TOTP/Mint

    init() {
        if let s = Keychain.load(.serverURL), let tok = Keychain.load(.token), let root = URL(string: s) {
            serverURL = s
            api = APIClient(root: root, token: tok)
            phase = .authed
        }
    }

    func login(username: String, password: String) async {
        errorMessage = nil
        guard let root = URL(string: normalized(serverURL)) else {
            errorMessage = "Ungültige Server-URL"
            return
        }
        busy = true
        defer { busy = false }
        let client = APIClient(root: root, token: nil)
        do {
            let resp: LoginResponse = try await client.post("api/v1/auth/login",
                LoginBody(username: username, password: password))
            if resp.totpRequired == true, let pending = resp.pending {
                pendingClient = client
                phase = .totp(pending: pending)
            } else {
                try await mintAndFinish(using: client, root: root)
            }
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func submitTOTP(code: String) async {
        errorMessage = nil
        guard case let .totp(pending) = phase, let client = pendingClient,
              let root = URL(string: normalized(serverURL)) else { return }
        busy = true
        defer { busy = false }
        do {
            let _: LoginResponse = try await client.post("api/v1/auth/login/totp",
                TOTPBody(pending: pending, code: code))
            try await mintAndFinish(using: client, root: root)
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    // mintAndFinish tauscht die frische Session gegen ein langlebiges API-Token.
    private func mintAndFinish(using client: APIClient, root: URL) async throws {
        let tok: APIToken = try await client.post("api/v1/auth/api-tokens",
            MintBody(label: deviceLabel(), expiresInHours: 0))
        guard let plain = tok.token else { throw APIError.message("Kein Token erhalten") }
        Keychain.save(.serverURL, normalized(serverURL))
        Keychain.save(.token, plain)
        Keychain.save(.tokenID, tok.id)
        api = APIClient(root: root, token: plain)
        pendingClient = nil
        errorMessage = nil
        phase = .authed
    }

    func logout() {
        // Best effort: Token serverseitig widerrufen, dann lokal löschen.
        if let api, let id = Keychain.load(.tokenID) {
            Task { try? await api.deleteVoid("api/v1/auth/api-tokens/\(id)") }
        }
        Keychain.delete(.token)
        Keychain.delete(.tokenID)
        api = nil
        pendingClient = nil
        phase = .login
    }

    private func normalized(_ s: String) -> String {
        var v = s.trimmingCharacters(in: .whitespacesAndNewlines)
        if !v.hasPrefix("http://") && !v.hasPrefix("https://") { v = "https://" + v }
        while v.hasSuffix("/") { v.removeLast() }
        return v
    }

    private func deviceLabel() -> String {
        #if canImport(UIKit)
        return "Roster iOS – \(UIDevice.current.name)"
        #else
        return "Roster iOS"
        #endif
    }
}

// MARK: - Request-Bodies

struct LoginBody: Encodable {
    let username: String
    let password: String
}

struct TOTPBody: Encodable {
    let pending: String
    let code: String
}

struct MintBody: Encodable {
    let label: String
    let expiresInHours: Int
    enum CodingKeys: String, CodingKey {
        case label
        case expiresInHours = "expires_in_hours"
    }
}

struct RunScriptBody: Encodable {
    let scriptId: String
    enum CodingKeys: String, CodingKey {
        case scriptId = "script_id"
    }
}
