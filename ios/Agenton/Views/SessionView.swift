import SwiftUI

struct SessionView: View {
    @ObservedObject var client: AgentonClient
    let sessionID: UInt32
    let title: String
    let onBack: () -> Void

    // Aa cycles font size: larger for readability first, then the small steps
    // that fit more columns so the agent's bottom status line (model / branch /
    // context%) renders instead of truncating with …. Persisted per-device.
    private static let fontSteps: [CGFloat] = [13, 15, 17, 11, 9]
    @AppStorage("termFontSize") private var fontSize: Double = 13

    @State private var input = ""
    // Owns the one terminal UIView for this session screen: mode toggles
    // rebuild the SwiftUI branch, but the cached view keeps the transcript.
    @State private var termHolder = TerminalViewHolder()
    @State private var customLabels: [String: String] = [:]
    @State private var rebindAction: String?
    @FocusState private var inputFocused: Bool
    // .compact vertical = iPhone landscape: dock the pad on the right third so
    // the terminal keeps usable rows; the text bar stays full-width at the bottom.
    @Environment(\.verticalSizeClass) private var vSize

    var body: some View {
        let landscape = vSize == .compact
        VStack(spacing: 8) {
            header
            if client.active {
                // Terminal mode: this phone owns the PTY size. The terminal must
                // keep the same structural position in both orientations — moving
                // it between branches would recreate the UIKit view on rotate and
                // blank the screen. Only the pad moves.
                HStack(spacing: 8) {
                    TerminalSurface(client: client, fontSize: CGFloat(fontSize), holder: termHolder)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        // Edge scroll bar: the terminal surface's own drags are
                        // ambiguous (SwiftTerm turns them into selection once a
                        // double-tap/long-press armed it), so give scrolling a
                        // dedicated lane. Right edge in portrait; left edge in
                        // landscape, away from the docked button pad.
                        .overlay(alignment: landscape ? .leading : .trailing) {
                            TerminalScrollBar(holder: termHolder, model: termHolder.bar)
                        }
                    if landscape {
                        // 3-wide pad in a quarter-width column: fewer columns need
                        // less width, so the terminal keeps 3/4 of the screen.
                        pad(columns: 3).containerRelativeFrame(.horizontal, count: 4, span: 1, spacing: 8)
                    }
                }
                if !landscape { pad(columns: 4) }
                textBar
            } else {
                // Controller mode: a remote for the session another client owns.
                // No terminal and no text field — you drive with the pad or the
                // hold-to-talk button. The button pad fills the screen.
                Spacer(minLength: 0)
                pad(columns: landscape ? 3 : 4)
                Spacer(minLength: 0)
                talkBar
            }
        }
        .background(Theme.bg)
        .overlay(alignment: .top) { if client.state == .disconnected { banner } }
        .sheet(item: Binding(get: { rebindAction.map(RebindID.init) },
                             set: { rebindAction = $0?.id })) { item in
            RebindSheet(action: item.id, initial: customLabels[item.id] ?? "") { value in
                customLabels[item.id] = value
                client.setButton(item.id, value: value)
            }
            .presentationDetents([.medium, .large])
        }
        .onAppear {
            client.connect(role: .session(sessionID))
            // Match orientation to the initial mode (active resets true on attach,
            // so this starts in the current orientation; a parked broadcast flips
            // it to landscape below).
            client.active ? Orientation.restore() : Orientation.lockLandscape()
        }
        .onChange(of: client.active) { _, active in
            // Controller mode (parked) → lock landscape so the pad fills the
            // screen; Terminal mode → restore the orientation from before we
            // forced landscape (the toggle-back the user asked for).
            active ? Orientation.restore() : Orientation.lockLandscape()
        }
        .onChange(of: client.customButtons) { _, bindings in
            // The daemon restores the session's custom bindings on attach, so a
            // rebind survives leaving and re-entering the session.
            customLabels = bindings
        }
        .onDisappear {
            client.onOutput = nil
            Orientation.lockPortrait() // back to the entry screen, which is portrait-only
        }
    }

    private func pad(columns: Int) -> some View {
        ButtonPad(client: client, customLabels: customLabels,
                  onRebind: { rebindAction = $0 },
                  onPrefill: { text in input = text; inputFocused = true },
                  columns: columns)
    }

    // The entry-time title ("repo · Terminal") with its agent half replaced by the
    // agent the daemon says is running now, so launching claude in a zsh session
    // retitles the header "repo · Claude" live (and back when it exits).
    private var headerTitle: String {
        guard !client.liveAgent.isEmpty else { return title }
        let name = SessionInfo.friendlyName(client.liveAgent)
        if let sep = title.range(of: " · ") {
            return String(title[..<sep.lowerBound]) + " · " + name
        }
        return name
    }

    private var header: some View {
        HStack {
            Button(action: onBack) {
                Label("back", systemImage: "chevron.left").labelStyle(.titleAndIcon)
            }
            .foregroundStyle(Theme.accent)
            Spacer()
            Text(headerTitle).font(.headline).foregroundStyle(Theme.fg).lineLimit(1)
            Spacer()
            Button(action: cycleFont) {
                Text("Aa").font(.system(size: 15, weight: fontSize != 13 ? .bold : .regular))
                    .foregroundStyle(fontSize != 13 ? Theme.accent : Theme.dim)
            }
            // Controller mode is a landscape-only, full-screen pad — a portrait
            // controller wastes space, so hand-rotation is disabled there.
            Button { Orientation.toggle() } label: {
                Image(systemName: "rotate.right")
                    .foregroundStyle(client.active ? Theme.accent : Theme.dim)
            }
            .padding(.leading, 10)
            .disabled(!client.active)
            Button(action: toggleActive) {
                Image(systemName: client.active ? "rectangle.on.rectangle" : "keyboard")
                    .foregroundStyle(Theme.accent)
            }
            .padding(.leading, 10)
        }
        .padding(.horizontal, 12)
        .padding(.top, 6)
    }

    private var textBar: some View {
        HStack(spacing: 8) {
            TextField("type a message…", text: $input)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.send)
                .focused($inputFocused)
                .onSubmit(send)
                // Visible dismiss control: submit sends (doesn't close), and the
                // interactive drag-down on the terminal has no on-screen cue, so a
                // chevron on the keyboard accessory bar gives a control you can see.
                .toolbar {
                    ToolbarItemGroup(placement: .keyboard) {
                        Spacer()
                        Button { inputFocused = false } label: {
                            Label("Dismiss", systemImage: "keyboard.chevron.compact.down")
                        }
                    }
                }
                .padding(10)
                .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
            // Terminal mode: hold to dictate; transcript lands in the field
            // above for review, then send. Sending is the keyboard's return key
            // (submitLabel .send above) — no separate on-screen send button.
            TalkButton { text in input = text; inputFocused = true }
        }
        .padding(.horizontal, 10)
        .padding(.bottom, 6)
    }

    // Controller mode has no text field — a single hold-to-talk button. With no
    // field to review in, the transcript is sent straight to the agent.
    private var talkBar: some View {
        TalkButton(label: "Hold to talk") { text in client.text(text) }
            .padding(.horizontal, 10)
            .padding(.bottom, 8)
    }

    private var banner: some View {
        HStack {
            Text("disconnected").fontWeight(.semibold)
            Spacer()
            Button("Reconnect") { client.reconnect() }.fontWeight(.semibold)
        }
        .padding(.horizontal, 14).padding(.vertical, 8)
        .foregroundStyle(Color(hex: 0x14060a))
        .background(Theme.warn)
    }

    private func send() {
        client.text(input)
        input = ""
    }

    private func cycleFont() {
        let i = Self.fontSteps.firstIndex(of: CGFloat(fontSize)) ?? 0
        fontSize = Double(Self.fontSteps[(i + 1) % Self.fontSteps.count])
    }

    private func toggleActive() {
        let take = !client.active
        client.setActive(take)
        if take, let s = client.lastSize {
            // Reclaiming: re-apply this device's last known terminal size right
            // away so the PTY matches the phone before the next redraw, instead
            // of waiting on a fresh sizeChanged from TerminalSurface. If no size
            // has ever been reported (this device was in Controller mode from
            // the start), skip — TerminalSurface reports its real size the
            // moment it mounts into Terminal mode, so there's nothing to fall
            // back to that wouldn't just be a guess.
            client.resize(cols: s.cols, rows: s.rows)
        }
    }
}

// Identifiable wrapper so a String action can drive .sheet(item:).
private struct RebindID: Identifiable { let id: String }
