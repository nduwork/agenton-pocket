import SwiftUI
import UIKit
import SwiftTerm

// Output-only terminal: input flows through the button pad and text bar (like
// the web client's disableStdin), so we suppress the on-screen keyboard by
// refusing first-responder. SwiftTerm's TerminalView is a UIScrollView, so
// scrollback panning + momentum come for free — the native rendering win over
// xterm.js in a web view.
final class OutputTerminalView: SwiftTerm.TerminalView {
    override var canBecomeFirstResponder: Bool { false }

    // Never let SwiftTerm attach its own mouse-drag recognizer: on a real device
    // it wins the touch and the UIScrollView's scroll pan never fires, so finger
    // drags stop scrolling scrollback. We report mouse events ourselves, from
    // the pan installed below, on our own terms. (Not calling super is what
    // prevents the attachment; `allowMouseReporting` stays false for the same
    // reason.)
    override func mouseModeChanged(source: Terminal) {}

    // MARK: drag → wheel
    //
    // A phone has no wheel, and an agent that owns the alternate screen leaves
    // the UIScrollView nothing to pan — SwiftTerm sizes contentSize to the
    // visible rows, and the daemon skips scrollback for alt-screen sessions
    // (internal/daemon/replay.go). Such an agent does enable mouse reporting and
    // scrolls its own conversation on wheel events, so that is the only way to
    // scroll it: translate a vertical drag into wheel ticks.
    //
    // Whether this fires is decided per session at drag time, not at build time:
    // the same claude build enables mouse reporting on one machine and runs
    // inline (real scrollback, native panning) on another. Encoding is left to
    // SwiftTerm's `sendEvent`, which uses whatever protocol the session actually
    // negotiated — SGR when it set ?1006h, X10 when it didn't. The reverted
    // first attempt (1a46e8c) hardcoded SGR and would emit garbage into the
    // sessions that never set it.
    weak var wheelClient: AgentonClient?
    private var wheelAccumulator: CGFloat = 0
    private var wheelDelegate: WheelGestureDelegate?

    static let wheelUp = 64
    static let wheelDown = 65

    // Attach the drag→wheel pan. Idempotent — the holder reuses one view across
    // mode toggles, and a second recognizer would double every tick.
    func installWheelGesture() {
        guard wheelDelegate == nil else { return }
        let del = WheelGestureDelegate()
        let g = UIPanGestureRecognizer(target: self, action: #selector(handleWheelPan(_:)))
        g.delegate = del
        addGestureRecognizer(g)
        wheelDelegate = del // recognizer holds its delegate weakly
    }

    @objc private func handleWheelPan(_ pan: UIPanGestureRecognizer) {
        switch pan.state {
        case .began: wheelAccumulator = 0
        case .changed: break
        default: return
        }
        let term = getTerminal()
        // Phone mode only (we own the PTY size), and only while the agent is
        // listening. Otherwise this is a no-op and the native scroll pan — which
        // recognizes simultaneously — pans real scrollback as before.
        guard let client = wheelClient, client.active, term.mouseMode != .off else { return }
        let cols = term.cols, rows = term.rows
        guard cols > 0, rows > 0, bounds.width > 0, bounds.height > 0 else { return }

        let cellH = bounds.height / CGFloat(rows)
        let delta = pan.translation(in: self).y
        pan.setTranslation(.zero, in: self) // translation now reads as a per-callback delta
        let ticks = Self.dragTicks(deltaY: delta, cellHeight: cellH, accumulated: &wheelAccumulator)
        guard !ticks.isEmpty else { return }

        let p = pan.location(in: self)
        let col = min(max(0, Int(p.x / (bounds.width / CGFloat(cols)))), cols - 1)
        let row = min(max(0, Int(p.y / cellH)), rows - 1)
        // ponytail: X10 encodes a coordinate as one byte (32+n+1), so it breaks
        // past column 94. A phone terminal is ~40-60 cols, and the clamp above
        // keeps us on the grid; revisit if this ever runs on a wide iPad.
        for button in ticks {
            term.sendEvent(buttonFlags: button, x: col, y: row)
        }
    }

    // One wheel tick per whole cell of accumulated travel, carrying the sub-cell
    // remainder across calls so a slow drag still adds up. Direction matches
    // native scrollback panning: dragging the finger *down* pulls older content
    // into view, which is a wheel *up*.
    static func dragTicks(deltaY: CGFloat, cellHeight: CGFloat, accumulated: inout CGFloat) -> [Int] {
        guard cellHeight > 0 else { return [] }
        accumulated += deltaY
        // Read the direction before consuming: an exact consume-to-zero would
        // otherwise leave a signless accumulator and flip the next tick.
        let fingerDown = accumulated >= 0
        let ticks = Int(abs(accumulated) / cellHeight)
        guard ticks > 0 else { return [] }
        let consumed = CGFloat(ticks) * cellHeight
        accumulated += fingerDown ? -consumed : consumed
        return Array(repeating: fingerDown ? wheelUp : wheelDown, count: ticks)
    }

    // Give the scroll view's own pan sole ownership of one-finger body drags.
    // SwiftTerm (a UIScrollView) installs long-press / double- / triple-tap
    // recognizers that, once fired, arm a *selection* pan; from then on drags
    // extend a selection instead of scrolling. That's why body scrolling is
    // reliable on the simulator (a trackpad never triggers those recognizers)
    // but dies on a real finger — one stray long-press or double-tap and the
    // terminal stops responding to drags. Disabling the selection triggers on
    // this output-only view removes the competition entirely, so a plain drag
    // always reaches the native scroll pan (which updateScroller already yields
    // to via isTracking). Single-tap stays live for link taps; the trade is
    // in-terminal text selection, which this output-only remote doesn't offer
    // anyway (the edge scroll bar remains as a belt-and-suspenders control).
    func ownBodyScrollGesture() {
        for g in gestureRecognizers ?? [] {
            switch g {
            case is UILongPressGestureRecognizer:
                g.isEnabled = false
            case let tap as UITapGestureRecognizer where tap.numberOfTapsRequired >= 2:
                g.isEnabled = false
            default:
                break
            }
        }
    }

    // True while the edge scroll bar is driving contentOffset. SwiftTerm keys
    // its manual-scroll freeze and follow-bottom logic off isTracking (a live
    // finger on the scroll view), so masquerading as tracking gives bar drags
    // the exact semantics of a native pan: yDisp follows, streaming output
    // doesn't yank the view back to the bottom, and releasing at the bottom
    // re-engages auto-follow.
    var barDragging = false
    override var isTracking: Bool { barDragging || super.isTracking }
}

// Lets the drag→wheel pan run alongside the UIScrollView's own pan, so a drag
// in a session with no mouse reporting still pans native scrollback (and the
// interactive keyboard dismiss still works).
private final class WheelGestureDelegate: NSObject, UIGestureRecognizerDelegate {
    func gestureRecognizer(_ g: UIGestureRecognizer,
                           shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }
}

// Scroll-bar state shared between the terminal coordinator (which observes
// scrolling) and the SwiftUI bar (which renders it and drives the viewport).
@MainActor
final class ScrollBarModel: ObservableObject {
    @Published var fraction: CGFloat = 1   // 0 = top of scrollback … 1 = bottom
    @Published var thumbRatio: CGFloat = 1 // viewport/content; 1 = nothing to scroll
}

// Always-available scroll control: a thin strip on the terminal's edge that
// widens under the finger and drags the viewport. Exists because one-finger
// drags on the terminal surface are ambiguous — SwiftTerm arms a selection pan
// after a double-tap or long-press "Select", and from then on drags extend the
// selection instead of scrolling. The bar always scrolls, whatever the
// selection state.
struct TerminalScrollBar: View {
    let holder: TerminalViewHolder
    @ObservedObject var model: ScrollBarModel
    @GestureState private var touching = false

    var body: some View {
        GeometryReader { geo in
            let trackH = geo.size.height
            let thumbH = max(44, model.thumbRatio * trackH)
            let travel = max(1, trackH - thumbH)
            Capsule()
                .fill(Color.white.opacity(touching ? 0.55 : 0.22))
                .frame(width: touching ? 14 : 4, height: thumbH)
                .frame(maxWidth: .infinity)
                .offset(y: model.fraction * travel)
                .frame(maxHeight: .infinity, alignment: .top)
                .contentShape(Rectangle()) // whole strip is grabbable, not just the thumb
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .updating($touching) { _, s, _ in s = true }
                        .onChanged { v in
                            holder.view?.barDragging = true
                            let f = min(1, max(0, (v.location.y - thumbH / 2) / travel))
                            model.fraction = f
                            scroll(to: f)
                        }
                        .onEnded { _ in holder.view?.barDragging = false }
                )
                .animation(.spring(duration: 0.2), value: touching)
        }
        .frame(width: 28) // generous touch target; the visible bar stays thin
        .opacity(model.thumbRatio < 1 ? 1 : 0)
        // DragGesture.onEnded doesn't fire on system cancellation; GestureState
        // reset does. Mirror it so a cancelled drag can't leave the terminal
        // masquerading as tracked (which would freeze follow-bottom forever).
        .onChange(of: touching) { _, t in
            if !t { holder.view?.barDragging = false }
        }
    }

    private func scroll(to fraction: CGFloat) {
        guard let tv = holder.view else { return }
        let maxOff = max(0, tv.contentSize.height - tv.bounds.height)
        tv.contentOffset = CGPoint(x: 0, y: fraction * maxOff)
    }
}

// Keeps one OutputTerminalView alive for the lifetime of a SessionView.
// SwiftUI tears the surface down on every Controller→Phone toggle (the
// terminal only exists in the phone-mode branch); a recreated view would come
// back empty — the attach replay was already consumed, and the agent's next
// repaint redraws only its live frame, so the transcript history above it
// would be gone. Reusing the same UIKit view keeps the full scrollback (and
// scroll position) across toggles with no network round-trip.
@MainActor
final class TerminalViewHolder {
    fileprivate var view: OutputTerminalView?
    let bar = ScrollBarModel()
}

// SwiftUI bridge. Feeds PTY bytes from the client into SwiftTerm and reports
// cell-size changes back as resize messages.
struct TerminalSurface: UIViewRepresentable {
    let client: AgentonClient
    let fontSize: CGFloat
    let holder: TerminalViewHolder

    func makeCoordinator() -> Coordinator { Coordinator(client: client, holder: holder) }

    func makeUIView(context: Context) -> OutputTerminalView {
        if let tv = holder.view {
            // Remounted (mode toggle): the cached view still holds the whole
            // transcript. Re-point the delegate and output route at it, and
            // catch up on a font change made while it was unmounted.
            tv.terminalDelegate = context.coordinator
            context.coordinator.view = tv
            tv.wheelClient = client // a remount can hand us a different client
            let f = Self.monoFont(fontSize)
            if tv.font != f { tv.font = f }
            client.onOutput = { [weak tv, weak coordinator = context.coordinator] data in
                tv?.feed(byteArray: ArraySlice(data))
                Task { @MainActor in coordinator?.pushBarState() }
            }
            return tv
        }
        let tv = OutputTerminalView(frame: .zero)
        tv.ownBodyScrollGesture() // scroll pan owns one-finger drags; see method
        tv.terminalDelegate = context.coordinator
        tv.font = Self.monoFont(fontSize)
        tv.backgroundColor = Theme.termBg
        tv.nativeBackgroundColor = Theme.termBg
        tv.nativeForegroundColor = Theme.termFg
        // Agents (claude) enable mouse reporting, which makes SwiftTerm feed pan
        // gestures to the PTY instead of scrolling. A phone has no mouse: finger
        // drags should always scroll the scrollback — the iOS twin of the web
        // client's native-scrolling fix (70b450d).
        tv.allowMouseReporting = false
        // …but an alt-screen agent has no scrollback to pan, so a drag there is
        // forwarded to it as wheel events instead. Decided per drag; see
        // OutputTerminalView's drag→wheel section.
        tv.wheelClient = client
        tv.installWheelGesture()
        // SwiftTerm defaults scrollback to 500 lines, so long conversations get
        // trimmed to the tail — you can only scroll back a screen or two. Match
        // the web client's 5000 so the whole conversation stays reachable. Must
        // run before onOutput feeds the attach-time scrollback replay below.
        tv.getTerminal().changeScrollback(5000)
        // The keyboard (raised by the text bar) has no dismiss key of its own, so
        // it stays up. .interactive lets a downward drag on the terminal pull it
        // offscreen with the finger — the native "swipe the keyboard away" gesture.
        tv.keyboardDismissMode = .interactive
        context.coordinator.view = tv
        // Route session output here. Buffered in the client until now, so the
        // scrollback replay that arrives right after attach isn't dropped.
        client.onOutput = { [weak tv, weak coordinator = context.coordinator] data in
            tv?.feed(byteArray: ArraySlice(data))
            // Streaming output grows contentSize even when the view is frozen
            // on history, which scrolled() alone doesn't report.
            Task { @MainActor in coordinator?.pushBarState() }
        }
        holder.view = tv
        return tv
    }

    func updateUIView(_ tv: OutputTerminalView, context: Context) {
        let f = Self.monoFont(fontSize)
        if tv.font != f { tv.font = f } // triggers a reflow → sizeChanged → resize
    }

    static func monoFont(_ size: CGFloat) -> UIFont {
        UIFont.monospacedSystemFont(ofSize: size, weight: .regular)
    }

    final class Coordinator: NSObject, TerminalViewDelegate {
        let client: AgentonClient
        let holder: TerminalViewHolder
        weak var view: OutputTerminalView?
        init(client: AgentonClient, holder: TerminalViewHolder) {
            self.client = client
            self.holder = holder
        }

        func sizeChanged(source: SwiftTerm.TerminalView, newCols: Int, newRows: Int) {
            Task { @MainActor in
                client.resize(cols: newCols, rows: newRows)
                pushBarState()
            }
        }

        // Keep the edge scroll bar honest: recompute its thumb from the real
        // scroll geometry whenever SwiftTerm scrolls (finger pans, streaming
        // output following the bottom, bar drags). Async because this fires
        // from UIKit layout/draw passes — publishing mid-update trips SwiftUI.
        @MainActor func pushBarState() {
            guard let tv = view else { return }
            let content = tv.contentSize.height
            let maxOff = content - tv.bounds.height
            holder.bar.thumbRatio = content > 0 ? min(1, tv.bounds.height / content) : 1
            holder.bar.fraction = maxOff > 0 ? min(1, max(0, tv.contentOffset.y / maxOff)) : 1
        }

        // The terminal originates exactly one kind of input: the mouse events
        // the drag→wheel pan feeds through Terminal.sendEvent, already encoded
        // in the session's negotiated mouse protocol. Typing still goes through
        // the button pad and text bar.
        // assumeIsolated, not `Task { @MainActor }`: this is always called from
        // the gesture handler on the main thread, and hopping would let a burst
        // of wheel ticks reach the daemon out of order.
        nonisolated func send(source: SwiftTerm.TerminalView, data: ArraySlice<UInt8>) {
            let s = String(decoding: data, as: UTF8.self)
            MainActor.assumeIsolated { client.textInputRaw(s) }
        }
        func setTerminalTitle(source: SwiftTerm.TerminalView, title: String) {}
        func hostCurrentDirectoryUpdate(source: SwiftTerm.TerminalView, directory: String?) {}
        func scrolled(source: SwiftTerm.TerminalView, position: Double) {
            Task { @MainActor in pushBarState() }
        }
        func requestOpenLink(source: SwiftTerm.TerminalView, link: String, params: [String: String]) {}
        func clipboardCopy(source: SwiftTerm.TerminalView, content: Data) {}
        func rangeChanged(source: SwiftTerm.TerminalView, startY: Int, endY: Int) {}
    }
}
