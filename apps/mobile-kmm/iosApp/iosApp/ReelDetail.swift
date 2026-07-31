import SwiftUI
import Shared

/// One reel, opened because someone asked for it.
///
/// Tapping a row in Saved used to navigate to the feed *in general* — wherever
/// it happened to be, showing whatever the ranker had chosen next. The viewer
/// had bookmarked one specific car and got a different one, which is the least
/// useful possible response to a bookmark.
///
/// Drawn over the feed rather than replacing it, for two reasons and the second
/// is load-bearing. The feed's scroll position and loaded pages survive, so
/// dismissing returns the viewer where they were. And there is only one
/// `PlaybackCoordinator` — a separate screen with its own would hold a second
/// pool of decoders behind the first, which on any device is how the next video
/// anywhere in the app fails to start with no error naming the cause. This
/// borrows the feed's, one item long.
struct ReelDetail: View {
    let viewModel: FeedViewModel
    let returnToIndex: Int32
    let onAction: (ResolvedAction, String) -> Void
    let onClose: () -> Void

    @State private var focused: FocusedReelState?
    @State private var skewMillis: Int64 = 0

    var body: some View {
        ZStack(alignment: .topTrailing) {
            Color.black.ignoresSafeArea()

            if let item = focused?.item {
                ReelPage(item: item, skewMillis: skewMillis, viewModel: viewModel)
                    .ignoresSafeArea()
            } else if let failure = focused?.error {
                // A saved reel that 404s is the common case here — taken down,
                // sold, expired — and NotFound is not retryable, so this renders
                // without a button that could not help.
                FailureBlock(failure: failure) { viewModel.retryReel() }
            } else {
                LoadingBlock()
            }

            // Top-*right*, not top-left. The overlay owns the left edge for the
            // subject's own badge on every vertical, and a close button there
            // sits directly on top of it.
            Button(action: dismiss) {
                Image(systemName: "xmark")
                    .font(.system(size: 18, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: Space.touch, height: Space.touch)
            }
            .accessibilityLabel(Text("action_close"))
            .padding(.trailing, Space.large)
        }
        .task {
            for await state in viewModel.focused { focused = state }
        }
        .task {
            for await state in viewModel.state { skewMillis = state.clockSkewMillis }
        }
        // The system back-swipe does the same thing. A modal that can only be
        // left by a gesture is a modal a fair number of people are stuck in,
        // which is why the button is there too.
        .gesture(DragGesture().onEnded { value in
            if value.translation.width > 100 { dismiss() }
        })
    }

    private func dismiss() {
        viewModel.closeReel(returnToIndex: returnToIndex)
        onClose()
    }
}
