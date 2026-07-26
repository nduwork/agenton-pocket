import Foundation

// Offline demo mode. No daemon, no network: a canned session and a scripted
// scrollback so the app can be exercised with nothing running behind it — the
// path App Review takes, and a zero-friction preview for first-run users.

extension SessionInfo {
    // Build a canned session without a server. SessionInfo's only initializer is
    // the JSON decoder (which suppresses the memberwise init), so we set the
    // stored fields directly here.
    init(demoID: UInt32, agent: String, status: String, cwd: String, repo: String) {
        self.id = demoID
        self.name = ""
        self.agent = agent
        self.status = status
        self.cwd = cwd
        self.lastActivity = 0
        self.repo = repo
        self.commandLine = agent
    }
}

extension AgentonClient {
    static let demoSessionID: UInt32 = 1

    // Enter demo mode: mark it, look "connected", and show one canned session.
    func startDemo() {
        disconnect()
        demo = true
        state = .connected
        // Labeled "Demo" (not a repo name) with a plainly non-"running" status so
        // it never reads as a real live session in the list.
        sessions = [SessionInfo(demoID: Self.demoSessionID,
                                agent: "/usr/local/bin/claude",
                                status: "offline demo — no server",
                                cwd: "~/repos/agenton",
                                repo: "Demo")]
    }

    // A representative claude-in-terminal transcript. \r\n line ends + a few ANSI
    // colors so it renders like a live agent session (SwiftTerm interprets them).
    static let demoTranscript = """
    \u{1b}[2J\u{1b}[H\u{1b}[1;36magenton demo\u{1b}[0m — offline preview (no server connected)\r
    \u{1b}[90m─────────────────────────────────────────────\u{1b}[0m\r
    \r
    \u{1b}[32m❯\u{1b}[0m claude\r
    \r
    \u{1b}[1mClaude Code\u{1b}[0m v2.0 — \u{1b}[90m~/repos/agenton\u{1b}[0m\r
    \r
    \u{1b}[36m│\u{1b}[0m What does the daemon do when the TUI detaches?\r
    \r
    The daemon keeps every session alive independently of any client. When the\r
    TUI detaches (\u{1b}[33mctrl+t\u{1b}[0m), the PTY and the agent process keep running; the\r
    session just loses its attached viewer. Re-attaching replays scrollback and\r
    resumes the live stream — from the TUI or the phone, or both at once.\r
    \r
    \u{1b}[32m✓\u{1b}[0m This is a canned demo. Connect a server (⚙︎) to drive real sessions.\r
    \r
    \u{1b}[36m│\u{1b}[0m \u{1b}[7m \u{1b}[0m\r
    """
}
