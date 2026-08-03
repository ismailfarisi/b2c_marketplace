package ae.marketplace.mobile.android.ui

import ae.marketplace.mobile.media.BlurHash
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import android.graphics.Bitmap
import androidx.compose.ui.graphics.FilterQuality
import coil3.compose.AsyncImagePainter
import coil3.compose.rememberAsyncImagePainter
import coil3.request.ImageRequest
import coil3.request.crossfade
import androidx.compose.ui.platform.LocalContext

/**
 * A poster, with the blurred approximation of itself underneath it.
 *
 * # The layering, and why it is in that order
 *
 * Three things can be on screen for one thumbnail, and they stack:
 *
 * 1. A flat [Palette.Surface] rectangle. What you see if there is no hash and
 *    no poster — a listing whose reel is still transcoding, for instance.
 * 2. The decoded [lqip], if there is one. Arrives with the JSON, costs no
 *    request, and is the right colours.
 * 3. The poster itself, crossfaded in over the top.
 *
 * The order matters for the case that actually bites: a slow connection. The
 * blur is drawn *behind* rather than swapped out for the image, so there is no
 * frame where the row is empty between the two, and nothing reflows — the box
 * is the same size throughout because its size never depended on what was
 * inside it.
 *
 * # Why not just a placeholder colour
 *
 * Because the grey rectangle was the old behaviour and the Saved screen carried
 * a comment defending it: with no loader in the app, a grey block was the
 * honest option. It was. It also meant a list of things the viewer had
 * deliberately kept was visually a list of identical grey blocks, which is the
 * one screen where telling them apart is the entire job.
 */
@Composable
fun Poster(
    url: String?,
    lqip: String?,
    modifier: Modifier = Modifier,
    contentScale: ContentScale = ContentScale.Crop,
    corner: androidx.compose.ui.unit.Dp = Shape.Thumb,
) {
    val shape = RoundedCornerShape(corner)

    Box(modifier.clip(shape).background(Palette.Surface)) {
        val blur = rememberBlur(lqip)
        if (blur != null) {
            Image(
                bitmap = blur,
                contentDescription = null,
                contentScale = ContentScale.Crop,
                // The decode is 32×32 and it is being stretched over a much
                // larger box, so bilinear filtering is doing the blurring for
                // free. Point sampling here would render visible squares.
                filterQuality = FilterQuality.Low,
                modifier = Modifier.fillMaxSize(),
            )
        }

        if (!url.isNullOrEmpty()) {
            val painter = rememberAsyncImagePainter(
                model = ImageRequest.Builder(LocalContext.current)
                    .data(url)
                    // Only when there is something to fade *from*. A crossfade
                    // over a flat grey box is a 200ms delay dressed up as
                    // polish; over a blur it is the thing that makes the
                    // transition invisible.
                    .crossfade(if (blur != null) CROSSFADE_MS else 0)
                    .build(),
            )
            val state by painter.state.collectAsState()

            // Errors are silent, deliberately. A poster that 404s leaves the
            // blur on screen, which is a slightly soft thumbnail rather than a
            // broken-image glyph — and the row's job is to be chosen from, not
            // to report on the object store's health.
            if (state !is AsyncImagePainter.State.Error) {
                Image(
                    painter = painter,
                    contentDescription = null,
                    contentScale = contentScale,
                    modifier = Modifier.fillMaxSize(),
                )
            }
        }
    }
}

private const val CROSSFADE_MS = 180

/**
 * Decodes a hash once per hash, not once per recomposition.
 *
 * A list row recomposes on scroll, on selection, and on any state its parent
 * touches. The decode is sub-millisecond but it allocates a 4KB int array, and
 * doing that on every frame of a fling is how a list that should be free starts
 * causing collections.
 */
@Composable
private fun rememberBlur(lqip: String?): ImageBitmap? = remember(lqip) {
    if (lqip.isNullOrEmpty()) return@remember null
    val pixels = BlurHash.decode(lqip, BLUR_W, BLUR_H) ?: return@remember null
    Bitmap.createBitmap(pixels, BLUR_W, BLUR_H, Bitmap.Config.ARGB_8888).asImageBitmap()
}

// 32×24 rather than something square: reels are portrait and posters are
// letterboxed from them, and a square decode of a 9:16 frame is stretched
// wrongly in whichever direction it is scaled.
private const val BLUR_W = 24
private const val BLUR_H = 32

