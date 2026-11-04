import SwiftUI
import AVKit
import Shared
import CoreLocation

/// The iOS feed.
///
/// Deliberately as thin as its Compose counterpart. Every decision that costs
/// money or corrupts data — which rendition, how far ahead to prefetch, what
/// counts as a view, when to page, and how a countdown resolves against a device
/// clock that is wrong — is in `:shared` and tested once. What is here is a
/// paging scroll view, an `AVPlayerLayer` and some text.
struct FeedScreen: View {
    let viewModel: FeedViewModel
    /// Search, Saved and Sell, shown after the feed surfaces.
    var extras: [FeedExtra] = []
    /// Runs the closure once there is an account; like and save need one.
    var requireAccount: (@escaping () -> Void) -> Void = { $0() }

    @StateObject private var locator = FeedLocator()
    @State private var usingFallback = false
    @State private var items: [FeedItem] = []
    @State private var surface: FeedSurface = FeedSurface.forYou
    @State private var emptyReason: FeedEmptyReason?
    @State private var error: Failure?
    @State private var loading = false
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
                    FeedEmptyStateView(
                        reason: emptyReason, error: error, loading: loading,
                        onRetry: { viewModel.refresh() }
                    )
                    .frame(width: geo.size.width, height: geo.size.height)
                } else {
                    ScrollViewReader { proxy in
                        ScrollView(.vertical, showsIndicators: false) {
                            LazyVStack(spacing: 0) {
                                ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
                                    ReelPage(
                                        item: item,
                                        skewMillis: clockSkewMillis,
                                        viewModel: viewModel,
                                        requireAccount: requireAccount
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

                VStack(spacing: 0) {
                    FeedTabsView(selected: surface, onSelect: { viewModel.onSurfaceSelected(surface: $0) }, extras: extras)
                    if usingFallback {
                        // Said out loud, as web's banner and Android do: a
                        // viewer elsewhere is looking at Dubai Marina's supply.
                        Text("filter_location_denied")
                            .font(.system(size: 12))
                            .foregroundStyle(.white)
                            .padding(.horizontal, 12).padding(.vertical, 6)
                            .background(Capsule().fill(Color.black.opacity(0.55)))
                            .padding(.top, 6)
                    }
                }
            }
            .ignoresSafeArea(edges: .bottom)
        }
        .task {
            // The API requires lat/lng — "a feed without a location is a
            // different product". When-in-use location first; Dubai Marina, with
            // a notice, only when it is declined or unavailable.
            if let here = await locator.locate() {
                viewModel.start(lat: here.latitude, lng: here.longitude, categoryId: nil, locationLabel: "")
            } else {
                usingFallback = true
                viewModel.start(lat: 25.0805, lng: 55.1403, categoryId: nil,
                                locationLabel: String(localized: "place_dubai_marina"))
            }

            // The same StateFlow Compose collects. Nothing about paging, error
            // handling, the exhausted state or clock skew is reimplemented here.
            for await state in viewModel.state {
                items = state.items
                surface = state.surface
                emptyReason = state.emptyReason
                error = state.error
                loading = state.loading
                clockSkewMillis = state.clockSkewMillis
            }
        }
    }
}

/// A marketplace destination opened from the feed's top row.
struct FeedExtra {
    let label: String
    let symbol: String
    let open: () -> Void
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
    /// Marketplace destinations reached from the feed rather than the bottom bar.
    var extras: [FeedExtra] = []

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
        HStack(spacing: 0) {
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
                .padding(.leading, 12)
            }
            // Destinations, not filters: icon buttons pinned to the end so they
            // never scroll off-screen behind the filter row.
            if !extras.isEmpty {
                Rectangle().fill(.white.opacity(0.25)).frame(width: 1, height: 20).padding(.horizontal, 4)
                ForEach(Array(extras.enumerated()), id: \.offset) { _, extra in
                    Button(action: extra.open) {
                        Image(systemName: extra.symbol)
                            .font(.system(size: 19))
                            .foregroundStyle(.white)
                            .frame(width: Space.touch, height: Space.touch)
                    }
                    .accessibilityLabel(Text(extra.label))
                }
                Spacer().frame(width: 6)
            }
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
    let error: Failure?
    let loading: Bool
    let onRetry: () -> Void

    var body: some View {
        Group {
            if loading {
                // Ordered before the failure on purpose: a retry sets both,
                // and leaving the previous error under a spinner tells the
                // viewer the retry has already failed.
                ProgressView().tint(.white.opacity(0.7))
            } else if let error {
                VStack(spacing: Space.small) {
                    Text(error.words)
                        .foregroundStyle(.white.opacity(0.7))
                        .font(.system(size: 14))
                        .multilineTextAlignment(.center)
                    // Only where retrying could work. A retry button under
                    // "You are signed out" cannot succeed, and the viewer
                    // presses it three times before believing it.
                    if error.canRetry {
                        Button(action: onRetry) {
                            Text("action_retry")
                                .font(.system(size: 14))
                                .foregroundStyle(Palette.accent)
                                .frame(minHeight: Space.touch)
                        }
                    }
                }
            } else {
                Text(NSLocalizedString(emptyKey, comment: ""))
                    .foregroundStyle(.white.opacity(0.7))
                    .font(.system(size: 14))
                    .multilineTextAlignment(.center)
            }
        }
        .padding(32)
    }

    private var emptyKey: String {
        guard let reason else { return "empty_eligible" }
        if reason == FeedEmptyReason.nothingLive { return "empty_live" }
        if reason == FeedEmptyReason.nothingClosing { return "empty_closing" }
        if reason == FeedEmptyReason.nothingNear { return "empty_near" }
        return "empty_eligible"
    }
}

struct ReelPage: View {
    let item: FeedItem
    let skewMillis: Int64
    let viewModel: FeedViewModel
    var requireAccount: (@escaping () -> Void) -> Void = { $0() }

    var body: some View {
        ZStack {
            ReelSurface(reelId: item.id, viewModel: viewModel)

            // Time bar, badge, time pill, product line, CTAs. One skeleton for
            // every vertical; only the time signal differs between them.
            // navBarHeight is added by the overlay itself; the bar is drawn by
            // RootView over this view and neither knows about the other.
            ReelOverlay(item: item, skewMillis: skewMillis, chromeBottom: navBarHeight) { action in
                viewModel.onActionClick(actionId: action.id, reelId: item.id)
            }

            HStack {
                Spacer()
                VStack(spacing: 8) {
                    Button {
                        let id = item.id
                        requireAccount { viewModel.toggleLike(reelId: id) }
                    } label: {
                        VStack(spacing: 2) {
                            Image(systemName: item.viewer.liked ? "heart.fill" : "heart")
                                .font(.system(size: 22))
                            Text(verbatim: "\(item.counters.likes)")
                                .font(.system(size: 12))
                        }
                        .foregroundStyle(.white)
                        .frame(minWidth: 44, minHeight: 44)
                    }
                    .accessibilityLabel(Text(item.viewer.liked ? "feed_unlike" : "feed_like"))

                    Button {
                        let id = item.id
                        requireAccount { viewModel.toggleSave(reelId: id) }
                    } label: {
                        Image(systemName: item.viewer.saved ? "bookmark.fill" : "bookmark")
                            .font(.system(size: 22))
                            .foregroundStyle(.white)
                            .frame(width: 44, height: 44)
                    }
                    .accessibilityLabel(Text(item.viewer.saved ? "feed_unsave" : "feed_save"))
                }
            }
            .padding(.trailing, 12)
        }
    }
}

/// One when-in-use fix for the feed, or nil when declined, restricted, failed
/// or not answered within a few seconds (the caller then falls back).
@MainActor
final class FeedLocator: NSObject, ObservableObject, CLLocationManagerDelegate {
    private let manager = CLLocationManager()
    private var continuation: CheckedContinuation<CLLocationCoordinate2D?, Never>?

    func locate() async -> CLLocationCoordinate2D? {
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyHundredMeters
        let status = manager.authorizationStatus
        if status == .denied || status == .restricted { return nil }
        return await withCheckedContinuation { c in
            continuation = c
            if status == .notDetermined {
                manager.requestWhenInUseAuthorization()
            } else {
                manager.requestLocation()
            }
            Task { @MainActor [weak self] in
                try? await Task.sleep(nanoseconds: 6_000_000_000)
                self?.finish(nil)
            }
        }
    }

    private func finish(_ coordinate: CLLocationCoordinate2D?) {
        continuation?.resume(returning: coordinate)
        continuation = nil
    }

    nonisolated func locationManagerDidChangeAuthorization(_ m: CLLocationManager) {
        let status = m.authorizationStatus
        Task { @MainActor in
            guard continuation != nil else { return }
            switch status {
            case .authorizedWhenInUse, .authorizedAlways: manager.requestLocation()
            case .denied, .restricted: finish(nil)
            default: break
            }
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        guard let c = locations.last?.coordinate else { return }
        let lat = c.latitude, lng = c.longitude
        Task { @MainActor in finish(CLLocationCoordinate2D(latitude: lat, longitude: lng)) }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didFailWithError error: Error) {
        Task { @MainActor in finish(nil) }
    }
}
