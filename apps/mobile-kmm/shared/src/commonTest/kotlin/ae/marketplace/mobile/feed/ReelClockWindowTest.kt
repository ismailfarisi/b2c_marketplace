package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.TimeSignal
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertTrue

/**
 * The window mode, which had no production user until flash offers arrived —
 * and broke on the first one.
 */
class ReelClockWindowTest {

    // 2026-07-31, a four-hour window from 22:00 to 02:00 Dubai time,
    // expressed in UTC instants the way the wire carries them.
    private val opens = "2026-07-31T18:00:00Z"
    private val closes = "2026-07-31T22:00:00Z"

    private fun signal() = TimeSignal(
        mode = "window",
        startsAt = opens,
        endsAt = closes,
        asOf = "2026-07-31T12:00:00Z",
    )

    /** 2026-07-31T12:53Z — about five hours before it opens. */
    private val beforeOpen = 1785502380000L

    @Test
    fun a_window_that_has_not_opened_does_not_claim_to_be_running() {
        // The bug, exactly as it reached a device: the bar was correctly empty
        // but the label read "9h 7m of 4h" — an elapsed larger than the total,
        // because it measured to the end of a window that had not begun.
        val face = ReelClock.face(signal(), deviceNowMillis = beforeOpen, skewMillis = 0)
            ?: error("no face")

        assertEquals(0f, face.fraction, "the bar should be empty before it opens")
        assertIs<ReelClock.Label.Opens>(
            face.label,
            "a window that has not opened must not render as elapsed-of-total",
        )
    }

    @Test
    fun before_it_opens_the_label_counts_down_to_the_opening() {
        val face = ReelClock.face(signal(), deviceNowMillis = beforeOpen, skewMillis = 0)!!
        val label = face.label as ReelClock.Label.Opens

        assertEquals(5, label.hours)
        assertEquals(7, label.minutes)
    }

    @Test
    fun once_it_opens_the_bar_fills_and_the_label_flips() {
        // One hour in: a quarter of a four-hour window.
        val oneHourIn = 1785520800000L + 3_600_000L // 18:00Z + 1h
        val face = ReelClock.face(signal(), deviceNowMillis = oneHourIn, skewMillis = 0)!!

        assertIs<ReelClock.Label.ElapsedOf>(face.label)
        assertTrue(face.fraction > 0.2f && face.fraction < 0.3f, "fraction was ${face.fraction}")
        val label = face.label as ReelClock.Label.ElapsedOf
        assertEquals(180, label.remainingMinutes)
        assertEquals(240, label.totalMinutes)
    }

    @Test
    fun a_window_fills_rather_than_drains() {
        // The distinction that makes window and countdown two modes rather than
        // one. Merging them would push this choice into the client as a
        // heuristic on how far away the end is.
        val early = ReelClock.face(signal(), 1785520800000L + 600_000L, 0)!!
        val late = ReelClock.face(signal(), 1785520800000L + 12_600_000L, 0)!!

        assertTrue(late.fraction > early.fraction, "a window should fill as it runs")
    }

    @Test
    fun a_closed_window_is_closed_not_negative() {
        val afterClose = 1785535200000L + 600_000L // 22:00Z + 10m
        val face = ReelClock.face(signal(), afterClose, 0)!!

        assertEquals(ReelClock.Label.Closed, face.label)
        assertTrue(face.expired)
    }
}
