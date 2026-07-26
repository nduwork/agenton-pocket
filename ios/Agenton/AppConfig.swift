import Foundation

// Compile-time feature switches. One flag per capability that isn't ready to
// ship, so turning something on is a one-line change in a known place.
enum AppConfig {
    // Master switch for the in-app tip jar (StoreKit). OFF until the IAPs exist
    // in App Store Connect and we're ready to ship them — while off, the Support
    // card never appears and no StoreKit call is made. Flip to `true` to enable
    // it. In the Simulator, Agenton.storekit (wired into the scheme in
    // project.yml) drives the whole purchase flow locally, so this can be tested
    // end-to-end before any Apple setup exists.
    static let storeKitEnabled = true
}
