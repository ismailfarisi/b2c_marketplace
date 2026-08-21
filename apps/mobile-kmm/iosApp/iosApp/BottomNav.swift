import SwiftUI

/// The five destinations.
///
/// The mirror of `Destination` in BottomNav.kt, and a plain Swift enum rather
/// than a Kotlin one: nothing in `:shared` needs to know which tab is on screen.
/// Navigation is the one thing that genuinely is platform-local — the shared
/// module owns paging, playback and the clock, and pushing tab state into it
/// would buy nothing and cost an interop boundary.
///
/// Only `feed` is built. The rest are listed because the bar's job is to show
/// what the product *is*, and a bar with one item does not do that. What it must
/// not do is lie: tapping Search reaches a screen that says Search is not built,
/// rather than a tab that swallows the tap and reads as a freeze.
///
/// `sell` sits in the middle because it is the supply side of a marketplace
/// whose binding constraint is supply, and the middle slot is the one a thumb
/// reaches without looking.
enum Destination: String, CaseIterable {
    case feed
    case search
    case sell
    case saved
    case me

    var labelKey: String {
        switch self {
        case .feed: return "nav_feed"
        case .search: return "nav_search"
        case .sell: return "nav_sell"
        case .saved: return "nav_saved"
        case .me: return "nav_me"
        }
    }

    /// The SF Symbol for this destination, filled when it is the one on screen.
    ///
    /// Two symbols rather than one plus a tint, because a tint is the only
    /// thing distinguishing them otherwise — and over a video frame, tint is
    /// the least reliable signal the bar has.
    func symbol(active: Bool) -> String {
        switch self {
        case .feed: return active ? "house.fill" : "house"
        case .search: return "magnifyingglass"
        case .sell: return "plus"
        case .saved: return active ? "bookmark.fill" : "bookmark"
        case .me: return active ? "person.fill" : "person"
        }
    }
}

/// Height of the bar's content, excluding the home-indicator inset that SwiftUI
/// adds via the safe area. The reel overlay adds this to clear the bar — without
/// it the price and CTA sit underneath "Sell".
let navBarHeight: CGFloat = 52

/// The bottom bar.
///
/// Drawn over the reel rather than beside it. Letterboxing the video above a
/// solid bar would give back the full-bleed frame the whole product is built on,
/// so the bar floats and earns its legibility from a gradient scrim.
///
/// Icons rather than words, and Sell as a bordered chip in the middle. The
/// labels were five words competing with the reel behind them; a glyph reads at
/// a glance and leaves the frame to the video, which is the whole product.
///
/// Active is the filled symbol in full white, inactive the outlined one at 65%.
/// No accent underline: the bar sits over arbitrary footage and a 2pt rule is
/// the first thing to disappear against a bright frame.
struct BottomNav: View {
    let selected: Destination
    let onSelect: (Destination) -> Void

    var body: some View {
        HStack {
            ForEach(Destination.allCases, id: \.self) { destination in
                let active = destination == selected
                Group {
                    if destination == .sell {
                        // The create affordance, drawn as a chip. It is the one
                        // tab that starts something rather than going
                        // somewhere, and a bare "+" among four nouns does not
                        // say that.
                        Image(systemName: destination.symbol(active: active))
                            .font(.system(size: 15, weight: .bold))
                            .foregroundStyle(active ? .black : .white)
                            .frame(width: 34, height: 26)
                            .background(
                                RoundedRectangle(cornerRadius: 8)
                                    .fill(active ? Color.white : .clear)
                            )
                            .overlay(
                                RoundedRectangle(cornerRadius: 8)
                                    .stroke(.white, lineWidth: 1)
                            )
                    } else {
                        Image(systemName: destination.symbol(active: active))
                            .font(.system(size: 21, weight: active ? .semibold : .regular))
                            .foregroundStyle(active ? .white : .white.opacity(0.65))
                    }
                }
                .frame(maxWidth: .infinity)
                // The whole slot is the target, not the glyph. A 21pt symbol is
                // a 25-point target and misses are indistinguishable from an
                // unresponsive app.
                .contentShape(Rectangle())
                .onTapGesture { onSelect(destination) }
                .accessibilityLabel(NSLocalizedString(destination.labelKey, comment: ""))
                .accessibilityAddTraits(active ? [.isButton, .isSelected] : .isButton)
            }
        }
        .frame(height: navBarHeight)
        .background(
            LinearGradient(
                colors: [.clear, .black.opacity(0.75)],
                startPoint: .top,
                endPoint: .bottom
            )
        )
    }
}

/// What an unbuilt destination shows.
///
/// It names the destination and says plainly that it is not built. An empty
/// screen, or a spinner that never resolves, is indistinguishable from a bug —
/// and the person most likely to hit it is someone demoing the app.
struct NotBuiltYet: View {
    let destination: Destination

    var body: some View {
        VStack(spacing: 8) {
            Text(NSLocalizedString(destination.labelKey, comment: ""))
                .font(.system(size: 20, weight: .semibold))
                .foregroundStyle(.white)
            Text(NSLocalizedString("nav_not_built", comment: ""))
                .font(.system(size: 13))
                .foregroundStyle(.white.opacity(0.55))
                .multilineTextAlignment(.center)
                .padding(.horizontal, 32)
        }
    }
}
