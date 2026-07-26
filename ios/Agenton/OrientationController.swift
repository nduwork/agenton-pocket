import SwiftUI

// Runtime interface-orientation control. Controller mode (phone parked) locks to
// landscape so the button pad fills the screen — the portrait pad floats with
// dead space. Every other screen allows all orientations. iOS asks the app
// delegate for the allowed mask, so we drive that mask and then ask the window
// scene to rotate to match.
final class AppDelegate: NSObject, UIApplicationDelegate {
    static var orientationMask: UIInterfaceOrientationMask = .all

    func application(_ application: UIApplication,
                     supportedInterfaceOrientationsFor window: UIWindow?) -> UIInterfaceOrientationMask {
        AppDelegate.orientationMask
    }
}

enum Orientation {
    // Inside a session the orientation is LOCKED (no gravity auto-rotation) —
    // the user rotates by hand with the header button. `current` is that locked
    // choice; `beforeController` is what to return to after Controller mode
    // force-flips to landscape.
    private static var current: UIInterfaceOrientationMask = .portrait
    private static var beforeController: UIInterfaceOrientationMask = .portrait

    // Header rotate button: flip portrait <-> landscape by hand.
    static func toggle() {
        current = (current == .portrait) ? .landscapeRight : .portrait
        apply(current)
    }

    // Controller mode: force landscape so the pad fills the screen, remembering
    // the current choice to restore afterward.
    static func lockLandscape() {
        beforeController = current
        current = .landscapeRight
        apply(current)
    }

    // Terminal mode: go back to the locked orientation from before Controller.
    static func restore() {
        current = beforeController
        apply(current)
    }

    // Entry / new-session screen: always portrait. The list + new-session form
    // are a vertical layout; landscape just adds dead side margins.
    static func lockPortrait() {
        current = .portrait
        beforeController = .portrait
        apply(.portrait)
    }

    private static func activeScene() -> UIWindowScene? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        return scenes.first(where: { $0.activationState == .foregroundActive }) ?? scenes.first
    }

    private static func apply(_ mask: UIInterfaceOrientationMask) {
        AppDelegate.orientationMask = mask
        guard let scene = activeScene() else { return }
        scene.windows.first?.rootViewController?.setNeedsUpdateOfSupportedInterfaceOrientations()
        // A single orientation is a lock: request it so the UI snaps there and
        // stays (gravity can't move it). .all re-enables free rotation.
        if mask != .all {
            scene.requestGeometryUpdate(.iOS(interfaceOrientations: mask)) { _ in }
        }
    }
}
