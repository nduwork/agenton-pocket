import Foundation
import Combine

// Connection settings + per-device command history. The browser client rode
// the page origin and localStorage; a standalone app has neither, so the user
// tells us where the daemon's web bridge lives and we persist it (+ recents)
// in UserDefaults.
final class AppSettings: ObservableObject {
    static let shared = AppSettings()

    @Published var host: String { didSet { d.set(host, forKey: "host") } }
    @Published var port: Int { didSet { d.set(port, forKey: "port") } }
    // Epoch seconds until which the "Support agenton" card stays hidden. Showing
    // the card (or dismissing it) pushes this out by `supportInterval`, so the
    // card reappears on a gentle cadence — on app open / foreground once the
    // interval has passed — instead of nagging every launch or vanishing forever.
    @Published var supportSnoozeUntil: Double { didSet { d.set(supportSnoozeUntil, forKey: "supportSnoozeUntil") } }

    // How long to hide the card after each showing/dismissal. Tune this one line
    // to change how often the tip prompt resurfaces (3 days ≈ typical review-nudge
    // spacing — present but not naggy).
    static let supportInterval: TimeInterval = 3 * 24 * 3600

    private let d = UserDefaults.standard

    private init() {
        host = d.string(forKey: "host") ?? ""
        port = d.object(forKey: "port") as? Int ?? 9787
        supportSnoozeUntil = d.double(forKey: "supportSnoozeUntil")
    }

    var isConfigured: Bool { !host.trimmingCharacters(in: .whitespaces).isEmpty }

    // The card is due when the snooze window has elapsed.
    var supportDue: Bool { Date().timeIntervalSince1970 >= supportSnoozeUntil }

    // Consume this showing and schedule the next appearance.
    func snoozeSupport() {
        supportSnoozeUntil = Date().timeIntervalSince1970 + Self.supportInterval
    }

    // Apply a scanned QR payload — "agenton://connect?host=…&port=…", the string
    // `agenton qr` emits. Returns false (leaving settings untouched) if it isn't
    // ours or carries no host. A `tls` param from an older agenton is ignored:
    // the bridge is plain HTTP over the tailnet.
    @discardableResult
    func apply(url raw: String) -> Bool {
        guard let c = URLComponents(string: raw), c.scheme == "agenton",
              let items = c.queryItems,
              let h = items.first(where: { $0.name == "host" })?.value?
                  .trimmingCharacters(in: .whitespaces), !h.isEmpty
        else { return false }
        host = h
        if let p = items.first(where: { $0.name == "port" })?.value, let n = Int(p) { port = n }
        return true
    }

    // ws://host:port/ws — the same endpoint web/server.go serves.
    var wsURL: URL? { url(scheme: "ws", path: "/ws") }

    // http://host:port/healthz — identity probe to sanity-check the address.
    var healthURL: URL? { url(scheme: "http", path: "/healthz") }

    private func url(scheme: String, path: String) -> URL? {
        var c = URLComponents()
        c.scheme = scheme
        c.host = host.trimmingCharacters(in: .whitespaces)
        c.port = port
        c.path = path
        return c.url
    }

    // --- command history (mirrors the web's localStorage recents) ---
    private let histKey = "cmdHistory"
    private let maxHist = 20

    func history() -> [String] { d.stringArray(forKey: histKey) ?? [] }

    func remember(_ command: String) {
        var h = history().filter { $0 != command }
        h.insert(command, at: 0)
        d.set(Array(h.prefix(maxHist)), forKey: histKey)
    }
}
