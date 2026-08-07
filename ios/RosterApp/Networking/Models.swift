import Foundation

// Decodable-Spiegel der Roster-JSON-Antworten. Der Decoder nutzt
// keyDecodingStrategy = .convertFromSnakeCase, daher camelCase-Properties.
// Zeitstempel bleiben bewusst Strings (kein Datums-Parsing nötig).

struct LoginResponse: Decodable {
    var totpRequired: Bool?
    var pending: String?
    var id: String?
    var username: String?
}

struct APIToken: Decodable {
    var id: String
    var token: String?
    var label: String?
}

struct DashboardSummary: Decodable {
    var devicesTotal: Int
    var devicesOnline: Int
    var devicesOffline: Int
    var devicesUnknown: Int
    var devicesWithFailingChecks: Int
    var failingChecks: Int
    var pendingPatches: Int
    var vulnerabilities: Int
    var recentEvents: [CheckEvent]?
}

struct CheckEvent: Decodable, Identifiable {
    var id: String
    var hostname: String?
    var checkName: String
    var newStatus: String
    var createdAt: String?
}

struct Device: Decodable, Identifiable {
    var id: String
    var hostname: String
    var os: String?
    var osVersion: String?
    var status: String?
    var lastSeen: String?
    var checksTotal: Int?
    var checksFailing: Int?
    var clientName: String?
    var siteName: String?
    var checkResults: [CheckResult]?
    var interfaces: [NetInterface]?
    var dockerContainers: [DockerContainer]?
}

struct DockerContainer: Decodable, Identifiable {
    var containerId: String
    var name: String?
    var image: String?
    var state: String?
    var status: String?
    var health: String?
    var id: String { containerId }
}

struct CheckResult: Decodable, Identifiable {
    var checkId: String
    var name: String?
    var status: String
    var output: String?
    var id: String { checkId }
}

struct NetInterface: Decodable {
    var mac: String?
    var ipv4: String?
}

struct Script: Decodable, Identifiable {
    var id: String
    var name: String
    var shell: String
    var platforms: [String]?
}

// CommandRef ist die Antwort einer eingereihten Aktion (command_id).
struct CommandRef: Decodable {
    var commandId: String?
}

// Command ist der gepollte Ausführungsstand (status = pending|sent|done).
struct Command: Decodable {
    var id: String
    var status: String
    var exitCode: Int?
    var output: String?
}
