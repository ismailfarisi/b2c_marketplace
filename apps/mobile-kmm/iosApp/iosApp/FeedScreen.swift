import SwiftUI
import AVKit
import Shared

/// The iOS feed.
///
/// Deliberately as thin as its Compose counterpart. Every decision that costs
/// money or corrupts data — which rendition, how far ahead to prefetch, what
/// counts as a view, when to page — is in `:shared` and tested once. What is
/// here is a paging scroll view, an `AVPlayerLayer` and some text.
struct FeedScreen: View {
    let viewModel: FeedViewModel

    @State private var items: [FeedItem] = []
    @State private var visible: Int = 0

    var body: some View {
        GeometryReader { geo in
            ScrollView(.vertical, showsIndicators: false) {
                LazyVStack(spacing: 0) {
                    ForEach(Array(items.enumerated()), id: \.element.id) { index, item in
                        ReelPage(item: item, viewModel: viewModel)
                            .frame(width: geo.size.width, height: geo.size.height)
                            .id(index)
                            .onAppear {
                                // `onAppear` on a full-page cell in a paging
                                // scroll view fires once the page has settled,
                                // which is the iOS equivalent of Compose's
                                // `settledPage`. Reporting on scroll offset
                                // instead would count a view for every reel a
                                // viewer half-swipes and pulls back from.
                                visible = index
                                viewModel.onVisible(index: Int32(index))
                            }
                    }
                }
            }
            .scrollTargetBehavior(.paging)
            .background(Color.black)
            .ignoresSafeArea()
        }
        .task {
            // Dubai Marina, hard-coded until the location permission flow lands.
            // The API requires lat/lng — "a feed without a location is a
            // different product" — so a fixed point is more honest than a
            // silent nationwide feed.
            viewModel.start(lat: 25.0805, lng: 55.1403, categoryId: nil)

            // The same StateFlow Compose collects. Nothing about paging, error
            // handling or the exhausted state is reimplemented here.
            for await state in viewModel.state {
                items = state.items
            }
        }
    }
}

private struct ReelPage: View {
    let item: FeedItem
    let viewModel: FeedViewModel

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            ReelSurface(reelId: item.id, viewModel: viewModel)

            VStack(alignment: .leading, spacing: 8) {
                Text(item.author.name.isEmpty ? "—" : item.author.name)
                    .foregroundStyle(.white)
                    .fontWeight(.semibold)

                if !item.caption.isEmpty {
                    Text(item.caption)
                        .foregroundStyle(.white)
                        .font(.footnote)
                        .lineLimit(3)
                }

                ForEach(item.actions.sorted { $0.position < $1.position }, id: \.id) { action in
                    Button {
                        viewModel.onActionClick(actionId: action.id, reelId: item.id)
                    } label: {
                        // Rendered from `label` and `available` alone. There is
                        // deliberately no switch on `targetType` anywhere in
                        // this app — the moment one exists, adding a vertical
                        // means shipping a mobile release.
                        Text(action.label)
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(!action.available)

                    if !action.available, let reason = action.reason {
                        // Always shown. A greyed-out button with no explanation
                        // reads as a broken app, and the vendor gets blamed for
                        // a platform decision.
                        Text(reason)
                            .foregroundStyle(.white.opacity(0.6))
                            .font(.caption2)
                    }
                }
            }
            .padding(16)
        }
    }
}
