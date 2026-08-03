package ae.marketplace.mobile.media

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The hash used throughout is the one actually sitting in `reels.lqip` in the
 * dev database, so these tests fail if the decoder stops agreeing with the data
 * the API is already serving.
 */
private const val REAL = "LEHV6nWB2yk8pyo0adR*.7kCMdnj"

class BlurHashTest {

    @Test
    fun `decodes a real hash to the requested size`() {
        val pixels = assertNotNull(BlurHash.decode(REAL, 32, 32))
        assertEquals(32 * 32, pixels.size)
    }

    @Test
    fun `every pixel is opaque`() {
        // A placeholder that decodes with a zero alpha channel is invisible, and
        // the symptom is indistinguishable from the decoder not running at all —
        // which is a genuinely annoying afternoon.
        val pixels = assertNotNull(BlurHash.decode(REAL, 8, 8))
        assertTrue(pixels.all { (it ushr 24) and 0xFF == 0xFF })
    }

    @Test
    fun `the same hash renders at any size`() {
        // The property that makes this worth having at all: one 28-character
        // string fills a 54dp thumbnail and a full-screen backdrop, and they
        // look like the same image.
        //
        // Top-left is exact — every basis function is cos(0) there regardless of
        // size. The far corner is *not*, and the first version of this test
        // asserted it was: the basis samples at `x / width`, which reaches 3/4
        // at width 4 and 63/64 at width 64. Larger renders sample closer to the
        // true edge, so the corners converge rather than match. That is the
        // reference algorithm's behaviour, not a rounding artefact, and pinning
        // it to equality would have been pinning a bug into the suite.
        val small = assertNotNull(BlurHash.decode(REAL, 4, 4))
        val large = assertNotNull(BlurHash.decode(REAL, 64, 64))

        assertEquals(small[0], large[0], "top-left must be exact at any size")

        for (shift in listOf(16, 8, 0)) {
            val a = (small[15] shr shift) and 0xFF
            val b = (large[4095] shr shift) and 0xFF
            assertTrue(
                kotlin.math.abs(a - b) < 40,
                "bottom-right diverged at shift $shift: $a vs $b",
            )
        }
    }

    @Test
    fun `the average colour matches the decoded top-left corner`() {
        // At x=0,y=0 every basis function is cos(0) = 1, so the pixel is the sum
        // of all components rather than the DC term alone — but they must land
        // in the same part of the colour space. A mismatch here means the DC
        // decode and the pixel loop disagree about sRGB, which is the classic
        // way this algorithm goes subtly wrong.
        val avg = assertNotNull(BlurHash.averageColour(REAL))
        val corner = assertNotNull(BlurHash.decode(REAL, 1, 1))[0]

        for (shift in listOf(16, 8, 0)) {
            val a = (avg shr shift) and 0xFF
            val c = (corner shr shift) and 0xFF
            assertTrue(
                kotlin.math.abs(a - c) < 96,
                "channel at shift $shift: average $a vs corner $c",
            )
        }
    }

    // ── malformed input, which must never throw ──────────────────────────────
    //
    // A bad hash is cosmetic: the real poster is still on its way. Every one of
    // these returning null rather than an exception is what keeps a mangled
    // 30-character string from taking down the row it was decorating.

    @Test
    fun `an empty hash is refused`() {
        assertNull(BlurHash.decode("", 8, 8))
        assertNull(BlurHash.averageColour(""))
    }

    @Test
    fun `a truncated hash is refused rather than decoded to noise`() {
        // The length is derivable from the first character, so a hash cut short
        // is detectable exactly rather than guessed at.
        assertNull(BlurHash.decode(REAL.dropLast(4), 8, 8))
    }

    @Test
    fun `a hash with a character outside the alphabet is refused`() {
        // Base-83 excludes several ASCII characters on purpose. A backslash here
        // stands in for the realistic case: a value that went through an escape
        // it should not have.
        val corrupted = REAL.replaceRange(5, 6, "\\")
        assertNull(BlurHash.decode(corrupted, 8, 8))
    }

    @Test
    fun `a zero or negative size is refused`() {
        assertNull(BlurHash.decode(REAL, 0, 8))
        assertNull(BlurHash.decode(REAL, 8, 0))
        assertNull(BlurHash.decode(REAL, -1, 8))
    }

    @Test
    fun `decoding is deterministic`() {
        // Guards the one thing a caching placeholder must not do: change between
        // two compositions of the same row.
        val a = assertNotNull(BlurHash.decode(REAL, 16, 16))
        val b = assertNotNull(BlurHash.decode(REAL, 16, 16))
        assertTrue(a.contentEquals(b))
    }
}
