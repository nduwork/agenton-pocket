import SwiftUI

struct EntryView: View {
    @ObservedObject var client: AgentonClient
    @ObservedObject var settings = AppSettings.shared
    let onEnter: (UInt32, String) -> Void   // tapped an existing session
    let onCreate: (String) -> Void           // launched a new one; carries its title

    @State private var command = ""
    @State private var cwd = ""
    @State private var showSettings = false
    @State private var showDirPicker = false
    @State private var showSupport = false
    @Environment(\.scenePhase) private var scenePhase

    // quick-start defaults shown as tappable chips. `ollama launch` starts
    // claude/codex wired to an ollama model — `ollama run` is just a chat REPL.
    private static let placeholders = [
        "claude", "codex",
        "ollama launch claude --model glm-5.2:cloud",
        "ollama launch codex --model gemma4:12b-mlx",
    ]

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    sessionList
                    newSessionForm
                }
                .padding()
            }
            .background(Theme.bg)
            // Pinned to the very bottom of the screen (docked above the home
            // indicator), so it reads as a quiet footer instead of floating in
            // the scroll content.
            .safeAreaInset(edge: .bottom) {
                if AppConfig.storeKitEnabled && showSupport {
                    SupportCard(client: client) { showSupport = false }
                        .padding(.horizontal).padding(.top, 8)
                        .background(Theme.bg)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) { brandMark }
                ToolbarItem(placement: .topBarTrailing) {
                    Button { showSettings = true } label: { Image(systemName: "gearshape") }
                }
                ToolbarItem(placement: .topBarLeading) {
                    Button { client.listSessions() } label: { Image(systemName: "arrow.clockwise") }
                }
            }
            // Only a real, configured connection can be "disconnected" — don't
            // flash the banner on first run (welcome state) or in demo mode.
            .overlay(alignment: .top) {
                if client.state == .disconnected && settings.isConfigured && !client.demo { banner }
            }
            .sheet(isPresented: $showSettings, onDismiss: reconnect) { ServerSettings() }
            .sheet(isPresented: $showDirPicker) {
                DirPicker(client: client) { cwd = $0 }
            }
            .onAppear {
                Orientation.lockPortrait() // the new-session screen is always vertical
                if settings.isConfigured { client.connect(role: .list) }
                refreshSupport()
            }
            // Re-check when the app returns to the foreground, so the tip card
            // resurfaces after the snooze window on a fresh session.
            .onChange(of: scenePhase) { _, phase in
                if phase == .active { refreshSupport() }
            }
            // Re-list every few seconds so a row's title follows what's running
            // inside its shell — the daemon retitles "Zsh" → "Claude" when you
            // launch an agent, but only reports it on the next list_sessions.
            .onReceive(Timer.publish(every: 3, on: .main, in: .common).autoconnect()) { _ in
                if client.state == .connected { client.listSessions() }
            }
        }
    }

    // Wordmark that sells the pitch: your session stays *on* after you walk away
    // from the desktop. "Agent" is the app; "On" is lit green with a glowing dot
    // — the same green as a running session's status dot below.
    private var brandMark: some View {
        HStack(spacing: 0) {
            Text("Agent").foregroundStyle(Theme.fg)
            Text("On")
                .foregroundStyle(Theme.ok)
                .shadow(color: Theme.ok.opacity(0.6), radius: 5)
            Circle().fill(Theme.ok).frame(width: 6, height: 6)
                .shadow(color: Theme.ok, radius: 4)
                .padding(.leading, 4).padding(.bottom, 8)
        }
        .font(.system(size: 19, weight: .heavy, design: .rounded))
    }

    private var sessionList: some View {
        VStack(alignment: .leading, spacing: 8) {
            if !settings.isConfigured && !client.demo { welcomeCard }
            if client.sessions.isEmpty {
                Text("no sessions — start one below").foregroundStyle(Theme.dim).font(.subheadline)
            } else {
                ForEach(client.sessions) { s in sessionRow(s) }
            }
        }
    }

    // First-run, no server yet: offer the offline demo or opening settings, so
    // the app is usable (and reviewable) with nothing running behind it.
    private var welcomeCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Welcome to agenton").foregroundStyle(Theme.fg).font(.headline)
            Text("Connect the daemon running on your computer, or try a canned offline demo — no server needed.")
                .foregroundStyle(Theme.dim).font(.subheadline)
            HStack(spacing: 8) {
                Button {
                    client.startDemo()
                    onEnter(AgentonClient.demoSessionID, "Demo · Claude")
                } label: {
                    Text("Try the demo").fontWeight(.semibold).frame(maxWidth: .infinity)
                        .padding(.vertical, 11)
                        .background(Theme.accent, in: RoundedRectangle(cornerRadius: 10))
                        .foregroundStyle(Theme.bg)
                }.buttonStyle(.plain)
                Button { showSettings = true } label: {
                    Text("Connect a server").fontWeight(.semibold).frame(maxWidth: .infinity)
                        .padding(.vertical, 11)
                        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.rule))
                        .foregroundStyle(Theme.fg)
                }.buttonStyle(.plain)
            }
        }
        .padding(14)
        .background(Theme.panel.opacity(0.5), in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.rule))
    }

    private func sessionRow(_ s: SessionInfo) -> some View {
        // Not a Button: nesting the kill Button inside a row Button makes SwiftUI
        // route the tap ambiguously. The row navigates via a tap gesture; the X
        // stays the only Button, so its tap is unambiguous.
        HStack(spacing: 10) {
            Circle().fill(s.isRunning ? Theme.ok : Theme.dim).frame(width: 9, height: 9)
            VStack(alignment: .leading, spacing: 2) {
                Text("\(s.repoLabel) · \(s.terminalName)")
                    .foregroundStyle(Theme.fg).font(.system(size: 15, weight: .medium))
                Text(s.status).foregroundStyle(Theme.dim).font(.caption)
            }
            Spacer()
            Button {
                client.kill(s.id)
            } label: { Image(systemName: "xmark.circle.fill").foregroundStyle(Theme.dim) }
            .buttonStyle(.plain)
        }
        .padding(12)
        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 12))
        .contentShape(RoundedRectangle(cornerRadius: 12))
        .onTapGesture { onEnter(s.id, "\(s.repoLabel) · \(s.terminalName)") }
    }

    private var newSessionForm: some View {
        VStack(alignment: .leading, spacing: 10) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 8) {
                    ForEach(chips, id: \.cmd) { chip in
                        Button { command = chip.cmd } label: {
                            HStack(spacing: 5) {
                                Image(systemName: chip.recent ? "clock.arrow.circlepath" : "bolt")
                                    .font(.system(size: 10))
                                Text(chip.cmd).font(.system(size: 13)).lineLimit(1)
                            }
                            .padding(.horizontal, 12).padding(.vertical, 7)
                            .foregroundStyle(chip.recent ? Theme.accent : Theme.dim)
                            .background(chip.recent ? Theme.accent.opacity(0.15) : Theme.panel, in: Capsule())
                            .overlay(Capsule().strokeBorder(chip.recent ? Theme.accent.opacity(0.4) : Theme.rule))
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            TextField("command (empty = shell; e.g. claude, ollama launch …)", text: $command)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
                .padding(10).background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
            HStack(spacing: 8) {
                TextField("cwd (optional, e.g. ~/repos/api)", text: $cwd)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                    .padding(10).background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                Button { showDirPicker = true } label: {
                    Image(systemName: "folder").font(.system(size: 18))
                        .foregroundStyle(Theme.accent)
                        .frame(width: 44, height: 44)
                        .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                }
                .buttonStyle(.plain)
            }
            Button(action: launch) {
                // Empty command → a plain shell on the host (the daemon fills in
                // $SHELL); the row title then follows any agent you run inside.
                Text(command.trimmingCharacters(in: .whitespaces).isEmpty ? "New shell" : "New session")
                    .fontWeight(.semibold).frame(maxWidth: .infinity)
                    .padding(.vertical, 12)
                    .background(Theme.accent, in: RoundedRectangle(cornerRadius: 10))
                    .foregroundStyle(Theme.bg)
            }
        }
    }

    // This device's recently typed commands (up to 6, most recent first),
    // then the claude/codex placeholders — deduped. Live-session command lines
    // are deliberately excluded: they carry session-specific args (full binary
    // path, --session-id) and aren't reusable as new-session templates.
    private var chips: [(cmd: String, recent: Bool)] {
        var seen = Set<String>(), out: [(cmd: String, recent: Bool)] = []
        for c in settings.history().prefix(6) where !c.isEmpty && seen.insert(c).inserted {
            out.append((c, true))
        }
        for c in Self.placeholders where seen.insert(c).inserted {
            out.append((c, false))
        }
        return out
    }

    private var banner: some View {
        HStack {
            Text("disconnected").fontWeight(.semibold)
            Spacer()
            Button("Reconnect") { reconnect() }.fontWeight(.semibold)
        }
        .padding(.horizontal, 14).padding(.vertical, 8)
        .foregroundStyle(Color(hex: 0x14060a)).background(Theme.warn)
    }

    // Show the tip card at most once per snooze window; showing it schedules the
    // next appearance. Once visible it stays for the session until dismissed.
    private func refreshSupport() {
        guard AppConfig.storeKitEnabled, settings.supportDue else { return }
        settings.snoozeSupport()
        showSupport = true
    }

    private func reconnect() {
        // Leaving demo behind once a real server is configured.
        if settings.isConfigured { client.demo = false; client.connect(role: .list) }
    }

    private func launch() {
        let line = command.trimmingCharacters(in: .whitespaces)
        let dir = cwd.trimmingCharacters(in: .whitespaces)
        if line.isEmpty {
            // No command → default login shell (daemon resolves $SHELL).
            client.newSession(command: "", args: [], cwd: dir)
            onCreate("shell")
            return
        }
        // ponytail: whitespace split — quoted args unsupported, same as web v1.1
        let parts = line.split(separator: " ").map(String.init)
        client.newSession(command: parts[0], args: Array(parts.dropFirst()), cwd: dir)
        settings.remember(line)
        command = ""
        onCreate(parts[0])
    }
}
