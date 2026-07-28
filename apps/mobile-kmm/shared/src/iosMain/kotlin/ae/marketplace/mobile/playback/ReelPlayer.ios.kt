package ae.marketplace.mobile.playback

import ae.marketplace.mobile.PlatformContext
import kotlinx.cinterop.ExperimentalForeignApi
import platform.AVFoundation.AVPlayer
import platform.AVFoundation.AVPlayerItem
import platform.AVFoundation.AVPlayerItemStatusReadyToPlay
import platform.AVFoundation.AVPlayerActionAtItemEndNone
import platform.AVFoundation.actionAtItemEnd
import platform.AVFoundation.currentItem
import platform.AVFoundation.pause
import platform.AVFoundation.play
import platform.AVFoundation.replaceCurrentItemWithPlayerItem
import platform.AVFoundation.seekToTime
import platform.CoreMedia.CMTimeGetSeconds
import platform.CoreMedia.kCMTimeZero
import platform.Foundation.NSNotificationCenter
import platform.Foundation.NSOperationQueue
import platform.Foundation.NSURL
import platform.AVFoundation.AVPlayerItemDidPlayToEndTimeNotification

/**
 * AVPlayer, wrapped down to the same seven methods.
 *
 * Two differences from the Android implementation are worth naming, because they
 * are exactly the kind of thing a cross-platform UI framework would have hidden
 * and got wrong.
 *
 * **Looping is a notification, not a flag.** AVFoundation has no equivalent of
 * ExoPlayer's `REPEAT_MODE_ONE` for a single item, so the loop is an observer on
 * `AVPlayerItemDidPlayToEndTime` that seeks back to zero. Same behaviour, wholly
 * different mechanism.
 *
 * **Buffering is not tuned here.** AVPlayer's equivalent of ExoPlayer's load
 * control is `preferredForwardBufferDuration` on the *item*, and setting it too
 * low on iOS causes visible stalls rather than the faster start it gives on
 * Android. It is left at the system default deliberately: the shared
 * [PlaybackPolicy] already bounds cost by choosing the rendition and how many
 * reels are warmed, which is the lever that actually moves egress.
 */
@OptIn(ExperimentalForeignApi::class)
internal class AvReelPlayer : ReelPlayer {

    private val player = AVPlayer().apply {
        // Without this AVPlayer pauses on the last frame and the reel shows a
        // frozen image, which reads as a broken video.
        actionAtItemEnd = AVPlayerActionAtItemEndNone
    }

    private var preparedUrl: String? = null
    private var observer: Any? = null

    override fun prepare(url: String) {
        if (preparedUrl == url) return
        preparedUrl = url

        val nsUrl = NSURL.URLWithString(url) ?: return
        val item = AVPlayerItem(uRL = nsUrl)
        player.replaceCurrentItemWithPlayerItem(item)

        observer?.let { NSNotificationCenter.defaultCenter.removeObserver(it) }
        observer = NSNotificationCenter.defaultCenter.addObserverForName(
            name = AVPlayerItemDidPlayToEndTimeNotification,
            `object` = item,
            queue = NSOperationQueue.mainQueue,
        ) { _ ->
            player.seekToTime(kCMTimeZero)
            player.play()
        }
    }

    override fun play() {
        player.play()
    }

    override fun pause() {
        player.pause()
    }

    override fun positionMs(): Long {
        val item = player.currentItem ?: return 0
        val seconds = CMTimeGetSeconds(item.currentTime())
        return if (seconds.isNaN()) 0 else (seconds * 1000).toLong()
    }

    override fun isReady(): Boolean =
        player.currentItem?.status == AVPlayerItemStatusReadyToPlay

    override fun release() {
        observer?.let { NSNotificationCenter.defaultCenter.removeObserver(it) }
        observer = null
        preparedUrl = null
        player.pause()
        player.replaceCurrentItemWithPlayerItem(null)
    }

    override fun currentUrl(): String? = preparedUrl

    /**
     * SwiftUI needs the real `AVPlayer` to build an `AVPlayerLayer`.
     *
     * Exposed from the iOS source set only, mirroring `ExoReelPlayer.exoPlayer`.
     * Common code cannot reach either.
     */
    val avPlayer: AVPlayer get() = player
}

actual fun createReelPlayer(context: PlatformContext): ReelPlayer = AvReelPlayer()

/**
 * The bridge SwiftUI needs, mirroring `asExoPlayer()` on Android.
 *
 * Swift sees this as `ReelPlayerKt.asAvPlayer(player)` and wraps the result in
 * an `AVPlayerLayer`.
 */
@OptIn(ExperimentalForeignApi::class)
fun ReelPlayer.asAvPlayer(): AVPlayer? = (this as? AvReelPlayer)?.avPlayer
