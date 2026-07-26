import SwiftUI

// Rebind a custom button to a single key chord — modifiers + one base key
// ("Ctrl+Space", "Ctrl+K", "Alt+a") — or literal text ("/compact"). Build it
// with the modifier toggles + a base key (typed or tapped), or type the whole
// chord directly ("ctrl_space" / "Ctrl+Space"); both normalize to the same
// value the daemon's encodeChord understands. No key sequences.
struct RebindSheet: View {
    let action: String
    let initial: String
    let onSave: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    @State private var mods: Set<String> = []          // subset of modOrder
    @State private var base = ""                        // base key or literal text

    private static let modOrder = ["Ctrl", "Alt", "Shift"]
    private static let namedKeys = ["Esc", "Enter", "Tab", "Shift+Tab", "Space", "Up", "Down", "Left", "Right",
                                    "Backspace", "Delete"]
    private static let quickKeys = ["Space", "Tab", "Enter", "Esc", "Up", "Down", "Left", "Right",
                                    "Backspace", "Delete"]
    // ⌘/Super/Win have no terminal byte encoding (the OS/terminal swallows them),
    // so a chord using one would reach the agent as literal text. Flag it instead.
    private static let unsendableMods: Set<String> = ["cmd", "command", "⌘", "super", "win", "windows", "gui", "hyper"]
    private let grid = Array(repeating: GridItem(.flexible(), spacing: 8), count: 4)

    // Display label for a modifier token (the token itself stays canonical).
    private static func modLabel(_ m: String) -> String { m == "Alt" ? "Alt / ⌥" : m }

    private var composed: String {
        let b = base.trimmingCharacters(in: .whitespaces)
        guard !b.isEmpty else { return "" }
        let picked = Self.modOrder.filter { mods.contains($0) }
        return Self.normalize((picked + [b]).joined(separator: "+"))
    }

    // True when the typed chord carries a modifier that can't be sent to a
    // terminal (⌘/Super/Win) — such chords can only be entered via the text field.
    private var unsendable: Bool {
        let c = composed
        guard c.contains("+") else { return false }
        return c.split(separator: "+").dropLast().contains { Self.unsendableMods.contains($0.lowercased()) }
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    section("Modifiers") {
                        HStack(spacing: 8) {
                            ForEach(Self.modOrder, id: \.self) { m in modChip(m) }
                        }
                    }

                    section("Key") {
                        TextField("base key or text (e.g. Space, k, /compact)", text: $base)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .padding(10)
                            .background(Theme.panel, in: RoundedRectangle(cornerRadius: 10))
                        LazyVGrid(columns: grid, spacing: 8) {
                            ForEach(Self.quickKeys, id: \.self) { k in
                                keyChip(k) { base = k }
                            }
                        }
                    }

                    HStack(spacing: 6) {
                        Text("will send:").foregroundStyle(Theme.dim)
                        Text(composed.isEmpty ? "—" : composed)
                            .font(.system(.body, design: .monospaced)).foregroundStyle(Theme.accent)
                    }
                    .font(.subheadline)

                    if unsendable {
                        Text("⌘/Super can’t be sent to a terminal — use Ctrl, Alt/Option, or Shift.")
                            .font(.caption).foregroundStyle(Theme.warn)
                    }
                }
                .padding()
            }
            .background(Theme.bg)
            .navigationTitle("Rebind \(action == "custom_1" ? "Custom 1" : "Custom 2")")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        if !composed.isEmpty && !unsendable { onSave(composed) }
                        dismiss()
                    }
                    .disabled(composed.isEmpty || unsendable)
                }
            }
            .onAppear {
                let (m, b) = Self.parse(initial)
                mods = m; base = b
            }
        }
    }

    @ViewBuilder private func section<Content: View>(_ title: String, @ViewBuilder _ body: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title).font(.subheadline).foregroundStyle(Theme.dim)
            body()
        }
    }

    private func modChip(_ m: String) -> some View {
        let on = mods.contains(m)
        return Button {
            if on { mods.remove(m) } else { mods.insert(m) }
        } label: {
            Text(Self.modLabel(m))
                .font(.system(size: 14, weight: .semibold))
                .frame(maxWidth: .infinity, minHeight: 40)
                .foregroundStyle(on ? Theme.bg : Theme.fg)
                .background(on ? Theme.accent : Theme.panel, in: RoundedRectangle(cornerRadius: 10))
        }
        .buttonStyle(.plain)
    }

    private func keyChip(_ k: String, _ tap: @escaping () -> Void) -> some View {
        let selected = base == k
        return Button(action: tap) {
            Text(k)
                .font(.system(size: 12, weight: .medium))
                .lineLimit(1).minimumScaleFactor(0.7)
                .frame(maxWidth: .infinity, minHeight: 34)
                .foregroundStyle(selected ? Theme.bg : Theme.fg)
                .background(selected ? Theme.accent : Theme.panel, in: RoundedRectangle(cornerRadius: 8))
        }
        .buttonStyle(.plain)
    }

    // MARK: value <-> UI

    // A binding fires as a keystroke (chord or named key), vs. literal text that
    // prefills the message bar. Shared with ButtonPad so both agree.
    static func isKeyBinding(_ s: String) -> Bool {
        s.contains("+") || namedKeys.contains(s)
    }

    // Canonicalize a raw chord: "_"→"+", modifier + base casing fixed, so
    // "ctrl_space" and "Ctrl+Space" and (mods{Ctrl}+base "space") all agree.
    static func normalize(_ s: String) -> String {
        let tokens = s.replacingOccurrences(of: "_", with: "+")
            .split(separator: "+").map(String.init)
        guard tokens.count > 1 else { return canonicalBase(tokens.first ?? s) }
        return (tokens.dropLast().map(canonicalMod) + [canonicalBase(tokens.last!)]).joined(separator: "+")
    }

    // Split a stored value back into (modifiers, base) for editing. Non-chords
    // (literal text, bare keys) come back as ([], value).
    static func parse(_ s: String) -> (Set<String>, String) {
        guard isKeyBinding(s), s.contains("+"), !namedKeys.contains(s) else { return ([], s) }
        let tokens = s.split(separator: "+").map(String.init)
        let mods = Set(tokens.dropLast().map(canonicalMod).filter { modOrder.contains($0) })
        return (mods, tokens.last ?? "")
    }

    private static func canonicalMod(_ m: String) -> String {
        switch m.lowercased() {
        case "ctrl", "control", "ctl", "c": return "Ctrl"
        case "alt", "opt", "option", "meta", "m": return "Alt"
        case "shift": return "Shift"
        default: return m
        }
    }

    private static func canonicalBase(_ b: String) -> String {
        switch b.lowercased() {
        case "space": return "Space"
        case "enter", "return": return "Enter"
        case "tab": return "Tab"
        case "esc", "escape": return "Esc"
        case "up": return "Up"
        case "down": return "Down"
        case "left": return "Left"
        case "right": return "Right"
        case "backspace", "bs": return "Backspace"
        case "delete", "del", "fn+delete": return "Delete" // forward delete (fn+delete)
        default: return b // single letters, slash commands, arbitrary text
        }
    }
}
