import SwiftUI
import StoreKit

// A quiet, dismissable "Support agenton" card for the free app: three tip tiers
// plus a like-gated rate/feedback path and web links. Matches welcomeCard's
// panel/rule styling so it reads as part of the entry screen, not an ad.
struct SupportCard: View {
    @ObservedObject var client: AgentonClient
    @StateObject private var store: DonationStore
    @Environment(\.requestReview) private var requestReview
    @State private var askRate = false
    let onDismiss: () -> Void

    init(client: AgentonClient, onDismiss: @escaping () -> Void) {
        self.client = client
        self.onDismiss = onDismiss
        // Route donation toasts through the shared client toast.
        _store = StateObject(wrappedValue: DonationStore(toast: { [weak client] in client?.toast = $0 }))
    }

    private static let pad: CGFloat = 16

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 6) {
                Image(systemName: "cup.and.saucer.fill")
                    .font(.system(size: 12, weight: .semibold)).foregroundStyle(Theme.accent)
                Text("Support agenton")
                    .foregroundStyle(Theme.fg).font(.subheadline.weight(.semibold))
                Text("keeps it free")
                    .foregroundStyle(Theme.dim).font(.caption)
                Spacer()
                Button { onDismiss() } label: {
                    Image(systemName: "xmark").font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(Theme.dim).padding(6).contentShape(Rectangle())
                }.buttonStyle(.plain)
            }
            HStack(spacing: 8) {
                ForEach(store.tiers) { tier in
                    Button { Task { await store.purchase(tier) } } label: {
                        Text(tier.price).font(.callout.weight(.semibold)).frame(maxWidth: .infinity)
                            .padding(.vertical, 11)
                            .background(Theme.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 11))
                            .overlay(RoundedRectangle(cornerRadius: 11).strokeBorder(Theme.accent.opacity(0.35)))
                            .foregroundStyle(Theme.accent)
                    }.buttonStyle(.plain)
                }
            }
            // Full-bleed hairline separating "give money" from the lighter links.
            Rectangle().fill(Theme.rule).frame(height: 1).padding(.horizontal, -Self.pad)
            HStack(spacing: 0) {
                linkItem("Rate", "star.fill") { askRate = true }
                linkItem("Feedback", "exclamationmark.bubble.fill") { LinkOpener.open(.feedback) }
                linkItem("Website", "globe") { LinkOpener.open(.website) }
                linkItem("Sponsor", "heart.fill") { LinkOpener.open(.sponsor) }
            }
        }
        .padding(Self.pad)
        .background(Theme.panel.opacity(0.55), in: RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(Theme.rule))
        .task { await store.load() }
        // Like-gate: only happy users reach Apple's review sheet (capped 3/year,
        // never auto-shown); everyone else is steered to GitHub issues.
        .confirmationDialog("Enjoying agenton?", isPresented: $askRate, titleVisibility: .visible) {
            Button("Yes, rate it") { requestReview() }
            Button("Not really — send feedback") { LinkOpener.open(.feedback) }
            Button("Cancel", role: .cancel) {}
        }
    }

    // A link is an icon over its label, tinted accent, filling an equal share of
    // the row so the four spread evenly with a comfortable tap target — a clear
    // step up from the old row of tiny gray words.
    private func linkItem(_ label: String, _ symbol: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            VStack(spacing: 5) {
                Image(systemName: symbol).font(.system(size: 16, weight: .medium))
                    .foregroundStyle(Theme.accent).frame(height: 20)
                Text(label).font(.system(size: 11, weight: .medium)).foregroundStyle(Theme.dim)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 4)
            .contentShape(Rectangle())
        }.buttonStyle(.plain)
    }
}

// External links opened in Safari. Sponsor + website URLs are owner-owned and
// may change; keep them here so there's one place to edit.
enum LinkOpener {
    enum Target {
        case feedback, sponsor, website
        var url: URL {
            switch self {
            case .feedback: return URL(string: "https://github.com/nduwork/agenton/issues/new")!
            // ponytail: GitHub Sponsors default; owner to confirm/replace the URL.
            case .sponsor: return URL(string: "https://github.com/sponsors/nduwork")!
            // ponytail: GitHub Pages guess for the marketing site; owner to set the real domain.
            case .website: return URL(string: "https://nduwork.github.io/agenton/#start")!
            }
        }
    }

    static func open(_ t: Target) { UIApplication.shared.open(t.url) }
}
