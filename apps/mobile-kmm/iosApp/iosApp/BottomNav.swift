import SwiftUI

/// Every top-level screen; the mirror of `Destination` in BottomNav.kt. Only
/// `bar` is on the bottom bar. Search, Sell and Saved belong to the video
/// marketplace and open from the feed.
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
    var symbol: String {
        switch self {
        case .meals: return "fork.knife"
        case .myMeals: return "calendar"
        case .feed: return "play.rectangle"
        default: return "person"
        }
    }

    /// What the bar shows. Meals lead; Search, Sell and Saved belong to the
    /// video marketplace and are reached from the feed's top row instead.
    static let bar: [Destination] = [.meals, .myMeals, .feed, .me]
}

/// Height of the bar's content, excluding the home-indicator inset. Screens
/// that draw under the bar add this to clear it.
let navBarHeight: CGFloat = 60

/// The bottom bar: an SF Symbol and a label per destination.
///
/// White on the meals tabs with a Chilli Red selection pill; over the video
/// feed it floats on a gradient scrim so the reel stays full-bleed.
struct BottomNav: View {
    let selected: Destination
    let onSelect: (Destination) -> Void

    private static let chilli = Color(red: 0.769, green: 0.231, blue: 0.086)
    private static let chilliSoft = Color(red: 0.984, green: 0.890, blue: 0.855)
    private static let muted = Color(red: 0.463, green: 0.404, blue: 0.357)
    private static let hairline = Color(red: 0.941, green: 0.886, blue: 0.824)

    private var light: Bool { selected == .meals || selected == .myMeals }

    var body: some View {
        HStack(spacing: 0) {
            ForEach(Destination.bar, id: \.self) { destination in
                let active = destination == selected
                let tint: Color = active ? (light ? Self.chilli : .white) : (light ? Self.muted : .white.opacity(0.7))
                Button { onSelect(destination) } label: {
                    VStack(spacing: 3) {
                        Image(systemName: destination.symbol)
                            .symbolVariant(active ? .fill : .none)
                            .font(.system(size: 19, weight: active ? .semibold : .regular))
                            .frame(width: 56, height: 30)
                            .background(
                                Capsule().fill(active ? (light ? Self.chilliSoft : .white.opacity(0.18)) : .clear)
                            )
                        Text(NSLocalizedString(destination.labelKey, comment: ""))
                            .font(.caption2.weight(active ? .semibold : .medium))
                            .lineLimit(1)
                            .minimumScaleFactor(0.8)
                    }
                    .foregroundStyle(tint)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(active ? [.isSelected] : [])
            }
        }
        .frame(height: navBarHeight)
        .background {
            if light {
                Color.white
                    .overlay(alignment: .top) { Self.hairline.frame(height: 1) }
                    .ignoresSafeArea(edges: .bottom)
            } else {
                LinearGradient(
                    colors: [.clear, .black.opacity(0.8)],
                    startPoint: .top,
                    endPoint: .bottom
                )
                .ignoresSafeArea(edges: .bottom)
            }
        }
    }
}
