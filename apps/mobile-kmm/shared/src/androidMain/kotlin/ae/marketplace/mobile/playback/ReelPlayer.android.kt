package ae.marketplace.mobile.playback

import ae.marketplace.mobile.PlatformContext
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.datasource.DefaultHttpDataSource

/**
 * ExoPlayer, wrapped down to the seven methods common code is allowed to know
 * about.
 *
 * The tuning below is the part that does not belong in shared code and is the
 * reason the app is KMM rather than something cross-platform all the way up.
 *
 * **Buffer targets are small.** ExoPlayer's defaults are built for a
 * feature-length stream: buffer 50 seconds, keep going. In a feed the viewer
 * swipes away after four, so every second buffered past that is egress paid for
 * content nobody watched — which is PLAN-VIDEO-FIRST §9's second-ranked risk,
 * measured in dollars per GB. Small buffers also mean a swipe starts playing
 * sooner, because the player is not still filling a queue for the reel before.
 *
 * **Playback starts after a very short buffer.** 500 ms of buffered video is
 * enough to start a 15-second vertical clip and is what makes the first frame
 * feel instant; the trade is an occasional rebuffer on a bad connection, which
 * is the better failure for this content.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
internal class ExoReelPlayer(platform: PlatformContext) : ReelPlayer {

    private var preparedUrl: String? = null

    private val player: ExoPlayer = ExoPlayer.Builder(platform.androidContext)
        .setLoadControl(
            DefaultLoadControl.Builder()
                .setBufferDurationsMs(
                    /* minBufferMs = */ 2_000,
                    /* maxBufferMs = */ 8_000,
                    /* bufferForPlaybackMs = */ 500,
                    /* bufferForPlaybackAfterRebufferMs = */ 1_000,
                )
                .setPrioritizeTimeOverSizeThresholds(true)
                .build()
        )
        .build()
        .apply {
            // Reels loop. A feed that stops on the last frame and shows a black
            // rectangle reads as a broken video, and the viewer swipes away from
            // content they were about to act on.
            repeatMode = Player.REPEAT_MODE_ONE
            playWhenReady = false
        }

    override fun prepare(url: String) {
        if (preparedUrl == url) return
        preparedUrl = url

        // HLS explicitly rather than by extension sniffing: the playlist urls
        // are presigned and carry a query string, and `.m3u8?X-Amz-...` defeats
        // the inference.
        val source = HlsMediaSource.Factory(DefaultHttpDataSource.Factory())
            .createMediaSource(MediaItem.fromUri(url))

        player.setMediaSource(source)
        player.prepare()
    }

    override fun play() {
        player.playWhenReady = true
    }

    override fun pause() {
        player.playWhenReady = false
    }

    override fun positionMs(): Long = player.currentPosition.coerceAtLeast(0)

    override fun isReady(): Boolean = player.playbackState == Player.STATE_READY

    override fun release() {
        preparedUrl = null
        player.release()
    }

    override fun currentUrl(): String? = preparedUrl

    /**
     * The one leak, and it is intentional.
     *
     * Compose needs the real `Player` to attach a `PlayerView`/`SurfaceView`.
     * Exposed from the Android source set only, so common code cannot reach it —
     * `ReelPlayer` in `commonMain` has no such member and never will.
     */
    val exoPlayer: ExoPlayer get() = player
}

actual fun createReelPlayer(context: PlatformContext): ReelPlayer = ExoReelPlayer(context)

/**
 * The bridge Compose needs, and the only public Android-specific API this module
 * exposes.
 *
 * Returns null rather than throwing for a player that is not an [ExoReelPlayer],
 * which in practice means a fake in a test. A surface that cannot attach should
 * draw the poster, not crash.
 */
fun ReelPlayer.asExoPlayer(): ExoPlayer? = (this as? ExoReelPlayer)?.exoPlayer
