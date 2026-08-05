import Foundation

extension Notification.Name {
    // Wird gepostet, wenn ein authentifizierter Request 401 liefert (Token abgelaufen/
    // widerrufen) – AppState meldet den Nutzer dann ab und zeigt den Login.
    static let rosterUnauthorized = Notification.Name("de.thomas-peterson.roster.unauthorized")
}

enum APIError: LocalizedError {
    case network
    case status(Int, String)
    case message(String)

    var errorDescription: String? {
        switch self {
        case .network:
            return "Netzwerkfehler"
        case let .status(code, body):
            return APIError.serverMessage(body) ?? "Serverfehler (\(code))"
        case let .message(m):
            return m
        }
    }

    // Der Server liefert Fehler als {"error": "..."} – daraus die Klartextmeldung ziehen.
    private static func serverMessage(_ body: String) -> String? {
        guard let data = body.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let msg = obj["error"] as? String else { return nil }
        return msg
    }
}

// APIClient kapselt die REST-Aufrufe gegen einen Roster-Server. Auth erfolgt entweder
// über den Session-Cookie (nur während des Logins, isolierter Cookie-Speicher) oder –
// im Normalbetrieb – über den Bearer-API-Token.
final class APIClient {
    let root: URL          // z. B. https://roster.example.com
    let token: String?
    private let session: URLSession
    private let decoder: JSONDecoder

    init(root: URL, token: String?) {
        self.root = root
        self.token = token
        let cfg = URLSessionConfiguration.ephemeral
        self.session = URLSession(configuration: cfg)
        let d = JSONDecoder()
        d.keyDecodingStrategy = .convertFromSnakeCase
        self.decoder = d
    }

    private func makeRequest(_ method: String, _ path: String) -> URLRequest {
        var r = URLRequest(url: root.appendingPathComponent(path))
        r.httpMethod = method
        r.setValue("application/json", forHTTPHeaderField: "Accept")
        if let token {
            r.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        return r
    }

    private func send<T: Decodable>(_ req: URLRequest) async throws -> T {
        let (data, resp) = try await session.data(for: req)
        guard let http = resp as? HTTPURLResponse else { throw APIError.network }
        // Abgelaufenes/widerrufenes Token: nur bei authentifizierten Requests melden
        // (beim Login liefert 401 „falsche Anmeldedaten", das ist kein Session-Ablauf).
        if http.statusCode == 401, token != nil {
            NotificationCenter.default.post(name: .rosterUnauthorized, object: nil)
        }
        guard (200..<300).contains(http.statusCode) else {
            throw APIError.status(http.statusCode, String(data: data, encoding: .utf8) ?? "")
        }
        return try decoder.decode(T.self, from: data)
    }

    // MARK: - Verben

    func get<T: Decodable>(_ path: String) async throws -> T {
        try await send(makeRequest("GET", path))
    }

    func post<T: Decodable, B: Encodable>(_ path: String, _ body: B) async throws -> T {
        var r = makeRequest("POST", path)
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        r.httpBody = try JSONEncoder().encode(body)
        return try await send(r)
    }

    func postEmpty<T: Decodable>(_ path: String) async throws -> T {
        try await send(makeRequest("POST", path))
    }

    func deleteVoid(_ path: String) async throws {
        let (_, resp) = try await session.data(for: makeRequest("DELETE", path))
        guard let http = resp as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
            throw APIError.network
        }
    }

    // MARK: - Async-Aktionen (einreihen → auf Ergebnis pollen)

    func runAndWait(_ path: String) async throws -> Command {
        let ref: CommandRef = try await postEmpty(path)
        return try await waitForCommand(ref.commandId)
    }

    func runAndWait<B: Encodable>(_ path: String, _ body: B) async throws -> Command {
        let ref: CommandRef = try await post(path, body)
        return try await waitForCommand(ref.commandId)
    }

    func waitForCommand(_ id: String?) async throws -> Command {
        guard let id, !id.isEmpty else { throw APIError.message("Keine Command-ID erhalten") }
        for _ in 0..<150 { // ~60 s bei 400 ms Intervall
            let cmd: Command = try await get("api/v1/commands/\(id)")
            if cmd.status == "done" { return cmd }
            try await Task.sleep(nanoseconds: 400_000_000)
        }
        throw APIError.message("Zeitüberschreitung – Gerät antwortet nicht")
    }
}
