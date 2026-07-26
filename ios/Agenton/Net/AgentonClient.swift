import Foundation
import Combine

// One WebSocket to the daemon's /ws bridge, driven exactly like the browser
// client: an "entry" connection lists/creates sessions; a "session" connection
// attaches and streams PTY output. Switching screens tears down and rebuilds
// the socket (mirrors entryWS / sessWS in app.js). Native URLSessionWebSocketTask
// — no third-party WebSocket dependency.
@MainActor
final class AgentonClient: NSObject, ObservableObject {
    enum ConnState { case connecting, connected, disconnected }
    enum Role: Equatable { case list, session(UInt32) }

    @Published var state: ConnState = .disconnected
    // Offline demo mode: no socket, canned session + scrollback. Lets App Review
    // (and curious first-run users) exercise the app with no daemon. See DemoMode.swift.
    // Contract: set true ONLY via startDemo(); cleared ONLY when a real server is
    // configured (EntryView.reconnect). While true, connect() routes to
    // connectDemo — any new real-connect entry point must clear it first.
    @Published var demo = false
    @Published var sessions: [SessionInfo] = []
    @Published var unmanaged: [SessionInfo] = []
    @Published var toast: String?
    // Whether this client currently owns the PTY size (true for a lone client
    // until told otherwise); false means another client is driving the terminal.
    @Published var active: Bool = true
    // Agent running inside the attached session right now, pushed by the daemon
    // as it changes (shell → claude → shell). Empty until the first push; the
    // session header falls back to its entry-time title. Reset on each attach.
    @Published var liveAgent: String = ""
    // Custom-button bindings the daemon restores on attach (set_button push), so a
    // rebind survives detaching and re-attaching. Keyed by custom_1/custom_2; only
    // non-empty values are kept. Reset on each attach, then repopulated.
    @Published var customButtons: [String: String] = [:]
    // Latest directory listing for the cwd browser (resolved path + subdirs).
    @Published var dirListing: DirListing?

    struct DirListing: Equatable { let path: String; let dirs: [String] }

    // Set by the terminal view: raw PTY bytes for the attached session. Output
    // that arrives before the terminal is ready (the scrollback replay fired the
    // instant we attach) is buffered and flushed when the handler is installed.
    private var outBuffer: [Data] = []
    var onOutput: ((Data) -> Void)? {
        didSet {
            guard let cb = onOutput else { return }
            // Demo mode: the terminal is recreated on every Controller→Phone
            // toggle and comes back empty (no live agent to repaint it, unlike a
            // real session). Re-render the canned transcript on each (re)mount.
            // It begins with a clear-screen, so re-feeding is idempotent.
            if demo {
                outBuffer.removeAll()
                if case .session = role { cb(Data(Self.demoTranscript.utf8)) }
                return
            }
            let pending = outBuffer; outBuffer = []
            pending.forEach(cb)
        }
    }
    // Deliver PTY bytes to the terminal, buffering until it installs onOutput.
    // Used by the live stream (handle) and by offline demo mode (connectDemo).
    func feedOutput(_ b: Data) {
        if let cb = onOutput { cb(b) } else { outBuffer.append(b) }
    }
    // Fired when a session we just created reports its id (session_state).
    var onSessionCreated: ((UInt32) -> Void)?
    // Fired when the attached session's process exits (or it's killed elsewhere),
    // so the UI can leave the session view and return to the list.
    var onSessionEnded: ((UInt32) -> Void)?

    private lazy var session = URLSession(configuration: .default, delegate: self, delegateQueue: nil)
    private var task: URLSessionWebSocketTask?
    private var role: Role = .list
    private var generation = 0 // stale read-loops from a prior socket exit quietly

    var attachedID: UInt32? { if case let .session(id) = role { return id } else { return nil } }

    // MARK: connection lifecycle

    // Demo counterpart to connect(): no socket. On attach, feed the canned
    // scrollback through the same onOutput/outBuffer path a real attach uses.
    // Lives here (not the DemoMode extension) because `role` is file-private.
    private func connectDemo(_ role: Role) {
        self.role = role
        state = .connected
        outBuffer.removeAll() // don't stack one attach's replay onto the next
        if case .session = role {
            active = true
            feedOutput(Data(Self.demoTranscript.utf8))
        }
    }

    func connect(role: Role) {
        if demo { connectDemo(role); return }
        guard let url = AppSettings.shared.wsURL else { toast = "set a server address first"; return }
        disconnect()
        self.role = role
        outBuffer.removeAll() // don't leak one session's replay into the next
        generation += 1
        let gen = generation
        state = .connecting
        let t = session.webSocketTask(with: url)
        t.maximumMessageSize = 1 << 20
        task = t
        t.resume()
        readLoop(gen: gen)
        // The daemon expects the first frame to say what we want. URLSession
        // queues sends until the socket opens, so this is safe pre-open.
        switch role {
        case .list: send(Envelope(type: .listSessions))
        case let .session(id):
            active = true // fresh attach defaults to lone-client active; #1's
            // unicast corrects this to false if the session is already owned.
            liveAgent = "" // header falls back to the entry-time title until the agent next changes
            customButtons = [:] // the daemon re-pushes the session's bindings right after attach
            var e = Envelope(type: .attach); e.sessionID = id
            send(e)
            // replay the terminal size so the PTY refits to this screen even if
            // SwiftTerm reported it before this connection existed
            if let s = lastSize { resize(cols: s.cols, rows: s.rows) }
        }
    }

    func disconnect() {
        generation += 1 // orphan any in-flight read loop
        task?.cancel(with: .goingAway, reason: nil)
        task = nil
    }

    func reconnect() { connect(role: role) }

    private func readLoop(gen: Int) {
        Task { @MainActor [weak self] in
            while true {
                guard let self, self.generation == gen, let task = self.task else { return }
                do {
                    let msg = try await task.receive()
                    guard self.generation == gen else { return } // superseded by a newer connect
                    if case let .data(d) = msg { self.handle(d) }
                } catch {
                    if self.generation == gen { self.state = .disconnected }
                    return
                }
            }
        }
    }

    // MARK: outgoing

    func send(_ env: Envelope) {
        guard let data = try? Wire.encodeControl(env) else { return }
        task?.send(.data(data)) { _ in } // best-effort, like the web client
    }

    func listSessions() { send(Envelope(type: .listSessions)) }

    // Browse the daemon host's filesystem for a cwd. Empty path = home.
    func listDir(_ path: String) {
        var e = Envelope(type: .listDir)
        e.path = path.isEmpty ? nil : path
        send(e)
    }

    func newSession(command: String, args: [String], cwd: String) {
        var e = Envelope(type: .newSessionCmd)
        e.command = command; e.args = args
        e.cwd = cwd.isEmpty ? nil : cwd
        e.agentLabel = command
        send(e)
    }

    func kill(_ id: UInt32) {
        if demo {
            // No socket in demo mode — drop the row locally. Closing the demo
            // session leaves demo mode (back to the welcome/unconfigured state).
            sessions.removeAll { $0.id == id }
            if sessions.isEmpty { demo = false }
            return
        }
        var e = Envelope(type: .killSession); e.sessionID = id
        send(e)
        listSessions()
    }

    func action(_ name: String) {
        guard case let .session(id) = role else { return }
        var e = Envelope(type: .action); e.sessionID = id; e.action = name
        send(e)
    }

    func text(_ s: String) {
        guard !s.isEmpty else { return }
        if demo { onOutput?(Data((s + "\r\n").utf8)); return } // echo so the demo feels live
        guard case let .session(id) = role else { return }
        // trailing CR submits in raw-mode agent TUIs, matching the web text bar.
        var e = Envelope(type: .textInput); e.sessionID = id; e.text = s + "\r"
        send(e)
    }

    // Raw text_input with no appended CR, for byte payloads that must reach the
    // PTY verbatim — the mouse-event escapes SwiftTerm emits for the drag→wheel
    // gesture. `text(_:)` appends a CR, which a mouse mode reads as a stray
    // Enter. Mirrors the local TUI's raw client.TextInput path.
    func textInputRaw(_ s: String) {
        guard !s.isEmpty else { return }
        if demo { return } // no agent to scroll in demo mode
        guard case let .session(id) = role else { return }
        var e = Envelope(type: .textInput); e.sessionID = id; e.text = s
        send(e)
    }

    func setActive(_ take: Bool) {
        // The mode button is the sole driver of our mode, so set it optimistically:
        // take=true enters Phone mode now (broadcasts never do — see handle()),
        // take=false drops to Controller immediately. The daemon claims/releases
        // the PTY size to match and tells the other clients.
        active = take
        guard case let .session(id) = role else { return }
        var e = Envelope(type: .setActive); e.sessionID = id; e.active = take
        send(e)
    }

    func setButton(_ action: String, value: String) {
        guard case let .session(id) = role else { return }
        var e = Envelope(type: .setButton); e.sessionID = id; e.action = action; e.text = value
        send(e)
    }

    // SwiftTerm reports its size during first layout, which can race ahead of
    // connect() — dropping that one-shot report leaves the PTY at its creation
    // width (the agent keeps drawing 80 cols on a 46-col phone). Cache the last
    // size and flush it on connect, like the web's sizeTerminal() in ws.onopen.
    // Exposed read-only so the Terminal/Controller toggle can re-send it on reclaim.
    private(set) var lastSize: (cols: Int, rows: Int)?

    func resize(cols: Int, rows: Int) {
        guard cols > 0, rows > 0 else { return }
        lastSize = (cols, rows)
        guard case let .session(id) = role, task != nil else { return }
        var e = Envelope(type: .resize); e.sessionID = id; e.cols = cols; e.rows = rows
        send(e)
    }

    // MARK: incoming

    private func handle(_ data: Data) {
        guard let f = Wire.decodeFrame(data) else { return }
        if f.type == FrameType.output.rawValue {
            guard attachedID == f.sessionID else { return }
            feedOutput(f.payload)
            return
        }
        guard let env = try? JSONDecoder().decode(Envelope.self, from: f.payload) else { return }
        switch env.type {
        case .sessionList:
            sessions = (env.sessions ?? []).sorted { $0.id < $1.id }
            unmanaged = env.unmanaged ?? []
        case .sessionState:
            // The attached session's process exited (or was killed elsewhere):
            // leave the session view. Checked before anything else so a terminal
            // state never gets mistaken for a park/ownership change.
            if env.status == "exited" || env.status == "killed",
               case let .session(id) = role, env.sessionID == id {
                onSessionEnded?(id)
                return
            }
            // Live agent push: the agent running inside the attached session
            // changed (shell → claude → shell). Retitle the header via liveAgent.
            if case let .session(id) = role, env.sessionID == id,
               let a = env.agentLabel, !a.isEmpty {
                liveAgent = a
            }
            // One-directional. A desk taking the PTY size parks us into
            // Controller mode (active=false), and we always honor that. But we
            // NEVER auto-enter Phone mode from a broadcast: Phone mode is only
            // ever entered by tapping the mode button (see setActive), so a
            // stray active=true (e.g. ownership reassigned when the desk
            // detaches) must not flip us into the terminal on its own.
            if env.active == false { self.active = false }
            // Only the entry connection creates sessions; on a session socket a
            // session_state means "detached" and must not trigger navigation.
            if case .list = role, let id = env.sessionID, id != 0 { onSessionCreated?(id) }
        case .setButton:
            // Daemon restoring a custom binding on attach (see the daemon's attach
            // handler). Keep non-empty values so the pad shows the label; an empty
            // value means the button is back to its default.
            if let a = env.action {
                if let t = env.text, !t.isEmpty { customButtons[a] = t }
                else { customButtons[a] = nil }
            }
        case .dirList:
            dirListing = DirListing(path: env.path ?? "", dirs: env.dirs ?? [])
        case .error:
            toast = env.message ?? "error"
        default:
            break
        }
    }
}

extension AgentonClient: URLSessionWebSocketDelegate {
    nonisolated func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask,
                               didOpenWithProtocol protocol: String?) {
        Task { @MainActor in self.state = .connected }
    }

    nonisolated func urlSession(_ session: URLSession, webSocketTask: URLSessionWebSocketTask,
                               didCloseWith closeCode: URLSessionWebSocketTask.CloseCode, reason: Data?) {
        Task { @MainActor in if self.task === webSocketTask { self.state = .disconnected } }
    }
}
