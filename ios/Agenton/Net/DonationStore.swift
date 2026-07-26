import Foundation
import Combine
import StoreKit

// Optional donations for a free app: three StoreKit 2 consumable tips. The app
// is free and these unlock nothing — they're a "buy me a coffee" path only.
//
// Renders fully offline: if App Store Connect products aren't loaded (Simulator,
// or before the IAPs exist), `tiers` falls back to static price strings so the
// card still draws; tapping a fallback tier just toasts that StoreKit is
// unavailable. Toasting is delegated to the caller (client.toast) via a closure
// so this stays independent of AgentonClient.
@MainActor
final class DonationStore: ObservableObject {
    static let ids = [
        "com.example.agentonpocket.tip.small",   // $2.99
        "com.example.agentonpocket.tip.medium",  // $5.99
        "com.example.agentonpocket.tip.large",   // $9.99
    ]

    private static let fallbackPrice = [
        "com.example.agentonpocket.tip.small": "$2.99",
        "com.example.agentonpocket.tip.medium": "$5.99",
        "com.example.agentonpocket.tip.large": "$9.99",
    ]

    struct Tier: Identifiable {
        let id: String
        let price: String
        let product: Product?   // nil = fallback (StoreKit not available)
    }

    @Published private(set) var products: [Product] = []

    private let toast: (String) -> Void
    private var updates: Task<Void, Never>?

    init(toast: @escaping (String) -> Void) {
        self.toast = toast
        // Catch out-of-band purchases (Ask to Buy approvals, other devices).
        updates = Task { [weak self] in
            for await result in Transaction.updates {
                guard case .verified(let txn) = result else { continue }
                await txn.finish()
                self?.toast("Thank you ☕")
            }
        }
    }

    deinit { updates?.cancel() }

    // Small → medium → large, real products when loaded else static fallbacks.
    var tiers: [Tier] {
        Self.ids.map { id in
            if let p = products.first(where: { $0.id == id }) {
                return Tier(id: id, price: p.displayPrice, product: p)
            }
            return Tier(id: id, price: Self.fallbackPrice[id] ?? "", product: nil)
        }
    }

    func load() async {
        guard AppConfig.storeKitEnabled else { return } // no StoreKit traffic while the feature is off
        products = (try? await Product.products(for: Self.ids)) ?? []
    }

    func purchase(_ tier: Tier) async {
        guard let product = tier.product else {
            toast("StoreKit unavailable in this build")
            return
        }
        do {
            switch try await product.purchase() {
            case .success(let verification):
                if case .verified(let txn) = verification {
                    await txn.finish()
                    toast("Thank you ☕")
                }
            case .userCancelled, .pending:
                break
            @unknown default:
                break
            }
        } catch {
            toast("Purchase failed")
        }
    }
}
