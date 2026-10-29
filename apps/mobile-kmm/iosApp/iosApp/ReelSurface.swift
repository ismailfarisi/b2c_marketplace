import SwiftUI
import AVFoundation
import Shared

/// Attaches the shared player to a layer.
///
/// The mirror of `ReelSurface.kt` on Android, and equally short for the same
/// reason: by the time this runs, which rendition to play and whether a player
/// exists at all have already been decided in `:shared`.
///
/// `AVPlayerLayer` directly rather than `VideoPlayer` from AVKit. `VideoPlayer`
/// brings a transport bar that intercepts touches and breaks the paging scroll,
/// and there is no supported way to remove it — the same problem
/// `useController = false` solves on Android, except AVKit has no equivalent
/// switch.
struct ReelSurface: UIViewRepresentable {
    let reelId: String
    let viewModel: FeedViewModel

    func makeUIView(context: Context) -> PlayerContainerView {
        PlayerContainerView()
    }

    func updateUIView(_ view: PlayerContainerView, context: Context) {
        // Nil when the reel is outside the prefetch window. Drawing black rather
        // than creating a player here is the point: a surface that eagerly made
        // its own would defeat the pool and the egress policy in one line.
        view.attach(player: viewModel.playerFor(reelId: reelId).flatMap { ReelPlayer_iosKt.asAvPlayer($0) })
    }
}

final class PlayerContainerView: UIView {
    override static var layerClass: AnyClass { AVPlayerLayer.self }

    private var playerLayer: AVPlayerLayer { layer as! AVPlayerLayer }

    override init(frame: CGRect) {
        super.init(frame: frame)
        backgroundColor = .black
        // Fill and crop. A vertical reel letterboxed into a 16:9 box is the
        // clearest sign of a feed built by someone who did not watch it.
        playerLayer.videoGravity = .resizeAspectFill
    }

    required init?(coder: NSCoder) { fatalError("not used") }

    func attach(player: AVPlayer?) {
        guard playerLayer.player !== player else { return }
        // Detach, never release. The player belongs to the shared pool and is
        // about to be reused for another reel.
        playerLayer.player = player
    }
}
