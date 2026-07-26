import SwiftUI

// Owns the single client and swaps between the entry list and a session, the
// way app.js flips #screen-entry / #screen-session. Launching a session waits
// for the daemon's session_state (its assigned id) before navigating in.
struct RootView: View {
    @StateObject private var client = AgentonClient()
    @State private var route = Route.entry
    @State private var pendingTitle = ""

    enum Route: Equatable { case entry, session(UInt32, String) }

    var body: some View {
        Group {
            switch route {
            case .entry:
                EntryView(client: client,
                          onEnter: { id, title in route = .session(id, title) },
                          onCreate: { pendingTitle = $0 })
            case let .session(id, title):
                SessionView(client: client, sessionID: id, title: title,
                            onBack: { route = .entry })
            }
        }
        .preferredColorScheme(.dark)
        .tint(Theme.accent)
        .overlay(alignment: .bottom) { toast }
        .onAppear {
            client.onSessionCreated = { id in
                route = .session(id, pendingTitle.isEmpty ? "session \(id)" : pendingTitle)
                pendingTitle = ""
            }
            // Session ended (process exited or killed elsewhere) — pop back to the
            // list if we're currently viewing that session.
            client.onSessionEnded = { id in
                if case let .session(cur, _) = route, cur == id { route = .entry }
            }
            // debug hook: `simctl launch ... com.example.agentonpocket -autoAttach 2` jumps
            // straight into that session — lets headless flight tests reach the
            // terminal without UI taps. Launch-argument defaults don't persist.
            let auto = UserDefaults.standard.integer(forKey: "autoAttach")
            if auto > 0 { route = .session(UInt32(auto), "session \(auto)") }
            // debug hook: `simctl launch ... com.example.agentonpocket -demo 1` starts the
            // offline demo and jumps into its canned session — lets headless tests
            // reach the demo terminal without UI taps.
            if UserDefaults.standard.integer(forKey: "demo") > 0 {
                client.startDemo()
                route = .session(AgentonClient.demoSessionID, "Demo · Claude")
            }
        }
        .onChange(of: client.toast) { _, msg in
            guard msg != nil else { return }
            Task { try? await Task.sleep(for: .seconds(4)); if client.toast == msg { client.toast = nil } }
        }
    }

    @ViewBuilder private var toast: some View {
        if let msg = client.toast {
            Text(msg)
                .font(.subheadline).foregroundStyle(Theme.fg)
                .padding(.horizontal, 16).padding(.vertical, 10)
                .background(Theme.panel, in: Capsule())
                .overlay(Capsule().strokeBorder(Theme.rule))
                .padding(.bottom, 24)
                .transition(.opacity)
        }
    }
}
