import SwiftUI
import UIKit

// Haptic tap — the web pad had no tactile feedback; on iOS a light impact makes
// each action feel like a physical key.
enum Haptics {
    static func tap() { UIImpactFeedbackGenerator(style: .light).impactOccurred() }
    static func rebind() { UIImpactFeedbackGenerator(style: .rigid).impactOccurred() }
}

// A pad action mirrors one web `data-action`. Fixed set + the two editable
// custom buttons (hold to rebind), matching the daemon's actionSequence().
private struct PadItem {
    let action: String
    let label: String
    let symbol: String
    let tint: Color
    var editable = false
}

struct ButtonPad: View {
    let client: AgentonClient
    let customLabels: [String: String]   // custom_1 / custom_2 -> bound value
    let onRebind: (String) -> Void       // open the rebind sheet for an action
    let onPrefill: (String) -> Void      // put a text binding into the text bar
    var columns = 4                      // 4x3 portrait; 3x4 landscape (narrower column)

    // Which key is momentarily flashing, so a tap is visibly acknowledged (the
    // phone has no terminal in Controller mode, so the flash is the only proof
    // the tap landed). Haptics fire alongside it.
    @State private var flash: String?

    private static let items: [String: PadItem] = [
        // ⏎ (SF "return"): accept and plain Enter are the same keystroke, so the
        // icon reads as both — label keeps the "Accept" intent.
        "accept": .init(action: "accept", label: "Enter", symbol: "return", tint: Theme.ok),
        "mode_switch": .init(action: "mode_switch", label: "Mode", symbol: "slider.horizontal.3", tint: Theme.accent),
        // Stop sends Esc on claude/codex/opencode (Ctrl+C on shells) — the same
        // key that interrupts generation and backs out of a permission prompt.
        "interrupt": .init(action: "interrupt", label: "Esc", symbol: "stop.fill", tint: Theme.warn),
        // Exit sends Ctrl+C, the universal quit for every agent + shell. Native
        // behavior: one tap per press (claude/codex need two to fully exit).
        "exit": .init(action: "exit", label: "Exit(Ctrl+C)", symbol: "power", tint: Theme.warn),
        "up": .init(action: "up", label: "Up", symbol: "chevron.up", tint: Theme.fg),
        "down": .init(action: "down", label: "Down", symbol: "chevron.down", tint: Theme.fg),
        "left": .init(action: "left", label: "Left", symbol: "chevron.left", tint: Theme.fg),
        "right": .init(action: "right", label: "Right", symbol: "chevron.right", tint: Theme.fg),
        // per-agent fresh conversation: the daemon types /clear (claude) or
        // /new (codex), Ctrl+L on plain shells. Esc stays reachable via Rewind
        // (Esc Esc) or a custom-key binding.
        "new_chat": .init(action: "new_chat", label: "New", symbol: "square.and.pencil", tint: Theme.accent),
        "rewind": .init(action: "rewind", label: "Rewind", symbol: "arrow.uturn.backward", tint: Theme.dim),
        "custom_1": .init(action: "custom_1", label: "Custom 1", symbol: "star", tint: Theme.accent, editable: true),
        "custom_2": .init(action: "custom_2", label: "Custom 2", symbol: "star", tint: Theme.accent, editable: true),
    ]

    // Arrows form a keyboard-style inverted T in both grids: Up directly above
    // Down, Left/Right flanking it on the bottom row; customs flank Up.
    private static let order4 = [ // portrait, 4 wide
        "accept", "mode_switch", "interrupt", "exit",
        "new_chat", "custom_1", "up", "custom_2",
        "rewind", "left", "down", "right",
    ]
    private static let order3 = [ // landscape, 3 wide
        "accept", "mode_switch", "interrupt",
        "exit", "new_chat", "rewind",
        "custom_1", "up", "custom_2",
        "left", "down", "right",
    ]

    var body: some View {
        let order = columns == 3 ? Self.order3 : Self.order4
        VStack(spacing: 8) {
            LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 8), count: columns), spacing: 8) {
                ForEach(order.compactMap { Self.items[$0] }, id: \.action) { item in key(item) }
            }
        }
        .padding(.horizontal, 10)
    }

    @ViewBuilder private func key(_ item: PadItem) -> some View {
        let bound = item.editable ? customLabels[item.action] : nil
        let lit = flash == item.action
        let face = VStack(spacing: 3) {
            Image(systemName: item.symbol)
                .font(.system(size: 16, weight: .semibold))
            Text(bound.map { $0.count > 10 ? String($0.prefix(9)) + "…" : $0 } ?? item.label)
                .font(.system(size: 11, weight: .medium))
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity, minHeight: 46)
        .foregroundStyle(item.tint)
        .background(lit ? item.tint.opacity(0.28) : Theme.panel, in: RoundedRectangle(cornerRadius: 12))
        .overlay(
            RoundedRectangle(cornerRadius: 12)
                .strokeBorder(item.editable ? Theme.accent.opacity(0.5) : Theme.rule,
                              style: StrokeStyle(lineWidth: 1, dash: item.editable ? [4, 3] : []))
        )
        .contentShape(RoundedRectangle(cornerRadius: 12))

        // tap fires the action; on the two custom keys, a 0.5s hold opens rebind
        // instead — onTapGesture + onLongPressGesture are mutually exclusive, so
        // a hold never also sends the action (the web client's `fired` guard).
        if item.editable {
            face
                .onTapGesture { Haptics.tap(); flashTap(item.action); fireCustom(item.action, bound: bound) }
                .onLongPressGesture(minimumDuration: 0.5) { Haptics.rebind(); onRebind(item.action) }
        } else {
            face
                .onTapGesture { Haptics.tap(); flashTap(item.action); client.action(item.action) }
        }
    }

    // Light up the tapped key briefly, then fade back — the visible half of the
    // haptic tap.
    private func flashTap(_ action: String) {
        withAnimation(.easeOut(duration: 0.08)) { flash = action }
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.18) {
            withAnimation(.easeIn(duration: 0.22)) { if flash == action { flash = nil } }
        }
    }

    // A custom key bound to literal text (a slash command like "/compact") only
    // types into the agent's input — it can't carry arguments. Prefill the text
    // bar with it instead, so the user can append info ("/compact focus on X")
    // and submit the whole line. Key chords ("Ctrl+Space") and named keys fire
    // immediately, and bindings we don't know about (daemon-side defaults) fall
    // back to the action.
    private func fireCustom(_ action: String, bound: String?) {
        if let text = bound, !RebindSheet.isKeyBinding(text) {
            onPrefill(text.hasSuffix(" ") ? text : text + " ")
        } else {
            client.action(action)
        }
    }
}
