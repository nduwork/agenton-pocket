import SwiftUI
import UIKit

// Tokyo Night palette carried over from the web client's style.css so the two
// clients read as one product.
enum Theme {
    static let bg = Color(hex: 0x0f1115)
    static let fg = Color(hex: 0xd8dee9)
    static let dim = Color(hex: 0x6b7280)
    static let accent = Color(hex: 0x7aa2f7)
    static let warn = Color(hex: 0xf7768e)
    static let ok = Color(hex: 0x9ece6a)
    static let rule = Color(hex: 0x262b36)
    static let panel = Color(hex: 0x161923)

    // Terminal colors as UIKit values for SwiftTerm's background/foreground.
    static let termBg = UIColor(red: 0x0f / 255, green: 0x11 / 255, blue: 0x15 / 255, alpha: 1)
    static let termFg = UIColor(red: 0xd8 / 255, green: 0xde / 255, blue: 0xe9 / 255, alpha: 1)
}

extension Color {
    init(hex: UInt32) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xff) / 255,
            green: Double((hex >> 8) & 0xff) / 255,
            blue: Double(hex & 0xff) / 255
        )
    }
}
