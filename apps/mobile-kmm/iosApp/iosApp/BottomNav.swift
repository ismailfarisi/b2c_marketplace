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
    case meals
    case myMeals
    case feed
    case search
    case sell
    case saved
    case me

    var labelKey: String {
        switch self {
        case .meals: return "nav_meals"
        case .myMeals: return "nav_my_meals"
        case .feed: return "nav_feed"
        case .search: return "nav_search"
        case .sell: return "nav_sell"
        case .saved: return "nav_saved"
        case .me: return "nav_me"
        }
    }
}

extension Destination {
    /// What the bar shows. Meals lead; Search, Sell and Saved belong to the
    /// video marketplace and are reached from the feed's top row instead.
    static let bar: [Destination] = [.meals, .myMeals, .feed, .me]
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
/// Text only, no icons, matching the mock — and the honest choice while four of
/// five destinations are unbuilt, because an icon set implies a settled
/// information architecture and this one is not settled.
struct BottomNav: View {
    let selected: Destination
    let onSelect: (Destination) -> Void

    private static let accent = Color(red: 0.706, green: 0.396, blue: 0.165)
    private static let chilli = Color(red: 0.769, green: 0.231, blue: 0.086)
    private static let muted = Color(red: 0.463, green: 0.404, blue: 0.357)

    /// Meals is a light food-photo page; the scrim is for video only.
    private var light: Bool { selected == .meals }

    var body: some View {
        HStack {
            ForEach(Destination.bar, id: \.self) { destination in
                let active = destination == selected
                VStack(spacing: 3) {
                    Text(NSLocalizedString(destination.labelKey, comment: ""))
                        .font(.system(size: 12, weight: active ? .semibold : .regular))
                        .foregroundStyle(active ? (light ? Self.chilli : Self.accent) : (light ? Self.muted : .white.opacity(0.7)))

                    Rectangle()
                        .fill(active ? (light ? Self.chilli : Self.accent) : .clear)
                        .frame(width: active ? 14 : 0, height: 2)
                }
                .frame(maxWidth: .infinity)
                // The whole slot is the target, not the word. A 12pt label is a
                // 30-point target and misses are indistinguishable from an
                // unresponsive app.
                .contentShape(Rectangle())
                .onTapGesture { onSelect(destination) }
            }
        }
        .frame(height: navBarHeight)
        .background {
            if light {
                Color.white.ignoresSafeArea(edges: .bottom)
            } else {
                LinearGradient(
                    colors: [.clear, .black.opacity(0.75)],
                    startPoint: .top,
                    endPoint: .bottom
                )
            }
        }
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
