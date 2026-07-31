package ae.marketplace.mobile.media

import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.pow
import kotlin.math.roundToInt

/**
 * Decodes the `lqip` string the API serves alongside every poster.
 *
 * # What it is
 *
 * A [BlurHash](https://blurha.sh) — around thirty characters that encode a
 * handful of DCT coefficients, enough to reconstruct a blurred approximation of
 * an image. `LEHV6nWB2yk8pyo0adR*.7kCMdnj` is a real one from this database.
 *
 * # Why bother, when the poster is one request away
 *
 * Because that request is the problem. The feed's whole premise is a vertical
 * pager that must feel instant, and a poster is tens of kilobytes over a cell
 * connection in a lift. The choice without this is a grey rectangle or a
 * layout that shifts when the image lands — and a list of grey rectangles is
 * exactly what the Saved screen was, with a comment explaining that drawing
 * nothing was the honest option because no loader existed.
 *
 * Thirty characters arrive with the JSON. They are already in memory when the
 * row is composed, they decode in well under a millisecond, and they are the
 * right colours — so a saved car looks like a car-shaped smudge rather than a
 * placeholder, and nothing moves when the real poster replaces it.
 *
 * # Why here rather than a library
 *
 * The reference decoder is a hundred lines of arithmetic with no dependencies
 * and no platform surface, so putting it in `commonMain` means iOS gets it for
 * free — where the alternative would have been a second implementation, in
 * Swift, with its own rounding. It is also the kind of code that is much easier
 * to trust with tests than to trust by reading, and tests need it to be common.
 *
 * Deliberately returns raw pixels rather than a platform bitmap: `commonMain`
 * has no image type, and the conversion is three lines on each platform.
 */
object BlurHash {

    /**
     * Decodes [hash] into [width] × [height] pixels, row-major, as packed ARGB.
     *
     * Returns null for anything malformed rather than throwing. A bad hash is a
     * cosmetic problem — the poster is still on its way — and letting it
     * propagate would take down a list row over a placeholder.
     *
     * [punch] scales the contrast of everything except the average colour. 1.0
     * is faithful to the encoder; the default is deliberately left there,
     * because the usual reason to reach for it is that the hash is being drawn
     * too large.
     */
    fun decode(hash: String, width: Int, height: Int, punch: Float = 1f): IntArray? {
        if (width <= 0 || height <= 0) return null
        if (hash.length < 6) return null

        val sizeFlag = decode83(hash, 0, 1) ?: return null
        val numY = (sizeFlag / 9) + 1
        val numX = (sizeFlag % 9) + 1
        if (hash.length != 4 + 2 * numX * numY) return null

        val quantisedMax = decode83(hash, 1, 2) ?: return null
        val maxValue = (quantisedMax + 1) / 166f

        // Each component is (r, g, b) in linear space.
        val components = Array(numX * numY) { FloatArray(3) }

        val dc = decode83(hash, 2, 6) ?: return null
        components[0] = decodeDC(dc)

        for (i in 1 until numX * numY) {
            val value = decode83(hash, 4 + i * 2, 6 + i * 2) ?: return null
            components[i] = decodeAC(value, maxValue * punch)
        }

        val pixels = IntArray(width * height)
        for (y in 0 until height) {
            for (x in 0 until width) {
                var r = 0f
                var g = 0f
                var b = 0f
                for (j in 0 until numY) {
                    for (i in 0 until numX) {
                        // The basis function. Note it samples at the pixel
                        // index over the output size, not over the source's —
                        // which is what lets the same hash render at any
                        // resolution, and is the whole reason a 28-character
                        // string can fill a 54×72 thumbnail and a full-screen
                        // reel backdrop equally well.
                        val basis = cos(PI * x * i / width).toFloat() *
                            cos(PI * y * j / height).toFloat()
                        val c = components[i + j * numX]
                        r += c[0] * basis
                        g += c[1] * basis
                        b += c[2] * basis
                    }
                }
                pixels[y * width + x] =
                    (0xFF shl 24) or
                        (linearToSrgb(r) shl 16) or
                        (linearToSrgb(g) shl 8) or
                        linearToSrgb(b)
            }
        }
        return pixels
    }

    /**
     * The average colour alone, as packed ARGB.
     *
     * Cheaper than [decode] by three orders of magnitude, and enough for the
     * cases where a blur would never be seen: the tint behind a 40dp avatar, or
     * the letterbox bars beside a portrait video on a wide screen.
     */
    fun averageColour(hash: String): Int? {
        if (hash.length < 6) return null
        val dc = decode83(hash, 2, 6) ?: return null
        val (r, g, b) = decodeDC(dc)
        return (0xFF shl 24) or (linearToSrgb(r) shl 16) or (linearToSrgb(g) shl 8) or linearToSrgb(b)
    }

    private fun decodeDC(value: Int): FloatArray = floatArrayOf(
        srgbToLinear((value shr 16) and 0xFF),
        srgbToLinear((value shr 8) and 0xFF),
        srgbToLinear(value and 0xFF),
    )

    private fun decodeAC(value: Int, maxValue: Float): FloatArray {
        val r = value / (19 * 19)
        val g = (value / 19) % 19
        val b = value % 19
        return floatArrayOf(
            signedPow((r - 9) / 9f) * maxValue,
            signedPow((g - 9) / 9f) * maxValue,
            signedPow((b - 9) / 9f) * maxValue,
        )
    }

    private fun signedPow(v: Float): Float =
        if (v < 0) -((-v).toDouble().pow(2.0)).toFloat() else (v.toDouble().pow(2.0)).toFloat()

    private fun srgbToLinear(value: Int): Float {
        val v = value / 255f
        return if (v <= 0.04045f) v / 12.92f
        else ((v + 0.055f) / 1.055f).toDouble().pow(2.4).toFloat()
    }

    private fun linearToSrgb(value: Float): Int {
        val v = value.coerceIn(0f, 1f)
        val s = if (v <= 0.0031308f) v * 12.92f
        else (1.055f * v.toDouble().pow(1 / 2.4).toFloat() - 0.055f)
        return (s * 255 + 0.5f).roundToInt().coerceIn(0, 255)
    }

    /**
     * Base-83, the alphabet BlurHash uses because it is URL- and JSON-safe
     * without escaping.
     *
     * Returns null on any character outside it, which is the check that stops a
     * truncated or mangled hash from silently decoding to noise.
     */
    private fun decode83(str: String, from: Int, to: Int): Int? {
        if (from < 0 || to > str.length || from >= to) return null
        var value = 0
        for (i in from until to) {
            val index = ALPHABET.indexOf(str[i])
            if (index == -1) return null
            value = value * 83 + index
        }
        return value
    }

    private const val ALPHABET =
        "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"
}
