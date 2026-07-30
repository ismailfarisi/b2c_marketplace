import SwiftUI
import AVKit
import Shared

/// The iOS feed.
///
/// Deliberately as thin as its Compose counterpart. Every decision that costs
/// money or corrupts data — which rendition, how far ahead to prefetch, what
/// counts as a view, when to page, and how a countdown resolves against a device
/// clock that is wrong — is in `:shared` and tested once. What is here is a
/// paging scroll view, an `AVPlayerLayer` and some text.
struct FeedScreen: View {
    let viewModel: FeedViewModel

    @State private var items: [FeedItem] = []
    @State private var surface: FeedSurface = FeedSurface.forYou
    @State private var emptyReason: FeedEmptyReason?
    @State private var error: String?
    @State private var clockSkewMillis: Int64 = 0

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .top) {
                Color.black.ignoresSafeArea()

                if items.isEmpty {
                    // The empty state sits under the tab row rather than
                    // replacing it. That is the point of naming *why* a surface
                    // is empty: the message exists to send the viewer to another
                    // tab, and a screen with no tabs gives them nowhere to go.
                    FeedEmptyStateView(reason: emptyReason, error: error)
                        .frame(width: geo.size.width, height: geo.size.height)
                } else {
                    ScrollViewReader { proxy in
                        ScrollView(.vertical, showsIndicators: false) {
                            LazyVStack(spacing: 0) {
                                ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
                                    ReelPage(
                                        item: item,
                                        skewMillis: clockSkewMillis,
                                        viewModel: viewModel
                                    )
                                    .frame(width: geo.size.width, height: geo.size.height)
                                    .id(index)
                                    .onAppear {
                                        // `onAppear` on a full-page cell in a
                                        // paging scroll view fires once the page
                                        // has settled, which is the iOS
                                        // equivalent of Compose's `settledPage`.
                                        // Reporting on scroll offset instead
                                        // would count a view for every reel a
                                        // viewer half-swipes and pulls back from.
                                        viewModel.onVisible(index: Int32(index))
                                    }
                                }
                            }
                        }
                        .scrollTargetBehavior(.paging)
                        .onChange(of: surface) { _, _ in
                            // Back to the top on a tab change. Without this the
                            // scroll offset survives, so switching to a two-item
                            // Live now feed leaves the viewer past the end of it
                            // and the screen looks blank.
                            //
                            // Unanimated: the list underneath has been replaced,
                            // and animating would flick through reels from a feed
                            // the viewer has already left — each reporting an
                            // impression on the way past.
                            proxy.scrollTo(0, anchor: .top)
                        }
                    }
                }

                FeedTabsView(selected: surface) { viewModel.onSurfaceSelected(surface: $0) }
            }
            .ignoresSafeArea(edges: .bottom)
        }
        .task {
            // Dubai Marina, hard-coded until the location permission flow lands.
            // The API requires lat/lng — "a feed without a location is a
            // different product" — so a fixed point is more honest than a silent
            // nationwide feed.
            viewModel.start(lat: 25.0805, lng: 55.1403, categoryId: nil)

            // The same StateFlow Compose collects. Nothing about paging, error
            // handling, the exhausted state or clock skew is reimplemented here.
            for await state in viewModel.state {
                items = state.items
                surface = state.surface
                emptyReason = state.emptyReason
                error = state.error
                clockSkewMillis = state.clockSkewMillis
            }
        }
    }
}

/// The four tabs.
///
/// Horizontally scrollable rather than four equal widths. Four short English
/// words fit; "Ending soon" in Arabic does not always, and a tab row that
/// truncates its own labels is worse than one that scrolls — the alternative is
/// choosing words for their length rather than their meaning.
struct FeedTabsView: View {
    let selected: FeedSurface
    let onSelect: (FeedSurface) -> Void

    private static let accent = Color(red: 0.706, green: 0.396, blue: 0.165)

    /// Listed explicitly rather than through `allCases`.
    ///
    /// A Kotlin enum crosses the Objective-C boundary as a class with static
    /// members, not as a Swift enum: there is no `CaseIterable` conformance and
    /// no pattern matching. Ordering matches `FeedSurface` in `:shared`, which is
    /// where the reasoning for it lives.
    private static let surfaces: [FeedSurface] = [
        .forYou, .liveNow, .endingSoon, .nearby,
    ]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 4) {
                ForEach(Self.surfaces, id: \.self) { surface in
                    let active = surface == selected
                    VStack(spacing: 4) {
                        Text(NSLocalizedString(Self.key(surface), comment: ""))
                            .font(.system(size: 14, weight: active ? .semibold : .regular))
                            .foregroundStyle(active ? .white : .white.opacity(0.65))

                        // A 2pt rule under the active tab rather than a filled
                        // chip: the row floats over video, and a filled
                        // background would need a scrim heavy enough to compete
                        // with the reel underneath it.
                        Rectangle()
                            .fill(active ? Self.accent : .clear)
                            .frame(width: active ? 20 : 0, height: 2)
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 10)
                    .contentShape(Rectangle())
                    .onTapGesture { onSelect(surface) }
                }
            }
            .padding(.horizontal, 12)
        }
    }

    private static func key(_ surface: FeedSurface) -> String {
        if surface == FeedSurface.liveNow { return "surface_live_now" }
        if surface == FeedSurface.endingSoon { return "surface_ending_soon" }
        if surface == FeedSurface.nearby { return "surface_nearby" }
        return "surface_for_you"
    }
}

/// The empty state, per surface.
///
/// Deliberately not the same sentence four times. The reason comes from the
/// server because the client cannot derive it: "no reels" and "no reels that are
/// live" are the same empty array.
struct FeedEmptyStateView: View {
    let reason: FeedEmptyReason?
    let error: String?

    var body: some View {
        Text(NSLocalizedString(key, comment: ""))
            .foregroundStyle(.white.opacity(0.7))
            .font(.system(size: 14))
            .multilineTextAlignment(.center)
            .padding(32)
    }

    private var key: String {
        if error != nil { return "feed_failed" }
        guard let reason else { return "empty_eligible" }
        if reason == FeedEmptyReason.nothingLive { return "empty_live" }
        if reason == FeedEmptyReason.nothingClosing { return "empty_closing" }
        if reason == FeedEmptyReason.nothingNear { return "empty_near" }
        return "empty_eligible"
    }
}

private struct ReelPage: View {
    let item: FeedItem
    let skewMillis: Int64
    let viewModel: FeedViewModel

    var body: some View {
        ZStack {
            ReelSurface(reelId: item.id, viewModel: viewModel)

            // Time bar, badge, time pill, product line, CTAs. One skeleton for
            // every vertical; only the time signal differs between them.
            ReelOverlay(item: item, skewMillis: skewMillis) { action in
                viewModel.onActionClick(actionId: action.id, reelId: item.id)
            }
        }
    }
}
