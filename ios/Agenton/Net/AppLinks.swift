import UIKit

// External links opened in Safari, shared by SupportCard and ServerSettings so
// there's one place to edit them. These point at the public agenton-pocket repo
// — they 404 until it's published, then just work. No feature flag.
enum AppLinks {
    enum Target {
        case feedback        // Support card → issue tracker
        case daemonInstall   // Settings → agenton-pocket README (how to run the daemon)
        var url: URL {
            switch self {
            case .feedback:      return URL(string: "https://github.com/nduwork/agenton-pocket/issues/new")!
            case .daemonInstall: return URL(string: "https://github.com/nduwork/agenton-pocket")!
            }
        }
    }

    static func open(_ t: Target) { UIApplication.shared.open(t.url) }
}
