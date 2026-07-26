import Foundation

// Mirrors internal/protocol/message.go. Only the fields the client sends or reads
// are modeled; JSON keys match the Go `json:"..."` tags exactly.

enum MsgType: String, Codable {
    case listSessions = "list_sessions"
    case sessionList = "session_list"
    case newSessionCmd = "new_session_cmd"
    case attach
    case detach
    case killSession = "kill_session"
    case action
    case setButton = "set_button"
    case textInput = "text_input"
    case resize
    case sessionState = "session_state"
    case setActive = "set_active"
    case error
    case listDir = "list_dir"
    case dirList = "dir_list"
}

struct SessionInfo: Codable, Identifiable, Equatable {
    let id: UInt32
    var name: String = ""
    var agent: String = ""
    var status: String = ""
    var cwd: String = ""
    var lastActivity: Int64 = 0
    var repo: String = ""
    var commandLine: String = ""

    enum CodingKeys: String, CodingKey {
        case id, name, agent, status, cwd
        case lastActivity = "last_activity"
        case repo
        case commandLine = "command_line"
    }

    // Synthesized Decodable throws keyNotFound for a missing key rather than
    // using the property default — but the daemon omits empty fields (repo,
    // command_line are `omitempty`). Decode every non-id field defensively so
    // one empty field can't drop the whole session_list.
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(UInt32.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        agent = try c.decodeIfPresent(String.self, forKey: .agent) ?? ""
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
        cwd = try c.decodeIfPresent(String.self, forKey: .cwd) ?? ""
        lastActivity = try c.decodeIfPresent(Int64.self, forKey: .lastActivity) ?? 0
        repo = try c.decodeIfPresent(String.self, forKey: .repo) ?? ""
        commandLine = try c.decodeIfPresent(String.self, forKey: .commandLine) ?? ""
    }

    var isRunning: Bool { status == "running" }

    // Short working-directory label: daemon-provided repo, with a client-side
    // folder-name fallback so it still works against older daemons.
    var repoLabel: String {
        if !repo.isEmpty { return repo }
        if !cwd.isEmpty { return cwd.split(separator: "/").last.map(String.init) ?? cwd }
        return "—"
    }

    // Friendly terminal name from the agent path: "/usr/local/bin/claude" → "Claude".
    var terminalName: String { Self.friendlyName(agent) }

    // Capitalized basename of an agent path/command; shared with the live-agent
    // title updates the daemon pushes (session_state.agent_label).
    static func friendlyName(_ agent: String) -> String {
        guard let base = agent.split(separator: "/").last, !base.isEmpty else { return "—" }
        return base.prefix(1).uppercased() + base.dropFirst()
    }
}

// One envelope type covers every control message. Optional fields are omitted
// when nil so we never send a key the daemon doesn't expect (matches omitempty).
struct Envelope: Codable {
    var type: MsgType
    var sessionID: UInt32?
    var action: String?
    var text: String?
    var status: String?
    var message: String?
    var sessions: [SessionInfo]?
    var unmanaged: [SessionInfo]?
    var command: String?
    var args: [String]?
    var cwd: String?
    var agentLabel: String?
    var cols: Int?
    var rows: Int?
    var path: String?
    var dirs: [String]?
    var active: Bool?

    enum CodingKeys: String, CodingKey {
        case type
        case sessionID = "session_id"
        case action, text, status, message, sessions, unmanaged, command, args, cwd
        case agentLabel = "agent_label"
        case cols, rows, path, dirs, active
    }

    init(type: MsgType) { self.type = type }
}
