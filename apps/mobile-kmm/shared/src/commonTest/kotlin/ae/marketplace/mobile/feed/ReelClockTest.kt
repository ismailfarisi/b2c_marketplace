package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.TimeSignal
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertIs
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * The clock, tested here rather than on either platform.
 *
 * This is the whole argument for putting it in `:shared`: an auction's last two
 * minutes run in a few microseconds against a supplied clock, and both platforms
 * inherit the result. The alternative is two hand-written implementations whose
 * disagreement first shows up as an iOS user tapping "bid" into a closed lot.
 */
@kotlin.OptIn(kotlin.time.ExperimentalTime::class)
class ReelClockTest {

    /** 2026-07-30T12:00:00Z, as epoch millis. The device's idea of now. */
    private val now = 1_785_412_800_000L

    private fun iso(offsetMs: Long): String {
        // Formatted from the same arithmetic the clock parses, so the test is
        // about the clock rather than about a date library.
        val instant = kotlin.time.Instant.fromEpochMilliseconds(now + offsetMs)
        return instant.toString()
    }

    private fun signal(
        mode: String,
        startsAt: String? = null,
        endsAt: String? = null,
        horizonSeconds: Int? = null,
        timezone: String? = null,
        recurring: Boolean = false,
        asOf: String = iso(0),
    ) = TimeSignal(
        mode = mode,
        startsAt = startsAt,
        endsAt = endsAt,
        horizonSeconds = horizonSeconds,
        timezone = timezone,
        recurring = recurring,
        asOf = asOf,
    )

    // ── skew ─────────────────────────────────────────────────────────────────

    @Test
    fun a_fast_device_does_not_close_an_auction_early() {
        // The device is four minutes ahead of the server. The auction has two
        // minutes left by the server's clock, so it is open — and a client that
        // trusted its own clock would render it closed and refuse the bid.
        val deviceNow = now + 4 * 60_000
        val sig = signal(
            ReelClock.MODE_COUNTDOWN,
            endsAt = iso(2 * 60_000),
            horizonSeconds = 3_600,
            asOf = iso(0),
        )

        val skew = ReelClock.skew(sig, deviceNow)
        val face = ReelClock.face(sig, deviceNow, skew)!!
        assertFalse(face.expired, "a device four minutes fast closed a live auction")

        val label = assertIs<ReelClock.Label.Remaining>(face.label)
        assertEquals(0, label.hours)
        assertEquals(2, label.minutes)
    }

    @Test
    fun a_slow_device_does_not_keep_a_closed_lot_open() {
        // The mirror case, and the one that costs the viewer a 409 rather than a
        // missed opportunity.
        val deviceNow = now - 4 * 60_000
        val sig = signal(ReelClock.MODE_COUNTDOWN, endsAt = iso(-60_000), horizonSeconds = 3_600)

        val face = ReelClock.face(sig, deviceNow, ReelClock.skew(sig, deviceNow))!!
        assertTrue(face.expired, "a device four minutes slow held a closed lot open")
    }

    @Test
    fun the_correction_is_sampled_once_and_does_not_freeze_the_clock() {
        // The bug this pins down is silent and total: recompute skew on every
        // tick and the corrected now equals as_of forever, because the growing
        // device clock and the growing correction cancel exactly. What the viewer
        // sees is a countdown stuck at the value it had when the page loaded,
        // which reads as a paused animation rather than a clock bug.
        val deviceNowAtFetch = now + 4 * 60_000
        val sig = signal(ReelClock.MODE_COUNTDOWN, endsAt = iso(10 * 60_000), horizonSeconds = 3_600)
        val skew = ReelClock.skew(sig, deviceNowAtFetch)

        val first = ReelClock.face(sig, deviceNowAtFetch, skew)!!
        val oneMinuteLater = ReelClock.face(sig, deviceNowAtFetch + 60_000, skew)!!

        assertTrue(
            oneMinuteLater.fraction < first.fraction,
            "the countdown did not advance — skew is being recomputed per tick",
        )
        val label = assertIs<ReelClock.Label.Remaining>(oneMinuteLater.label)
        assertEquals(9, label.minutes)
    }

    // ── countdown ────────────────────────────────────────────────────────────

    @Test
    fun a_countdown_drains() {
        val sig = signal(ReelClock.MODE_COUNTDOWN, endsAt = iso(30 * 60_000), horizonSeconds = 3_600)
        val half = ReelClock.face(sig, now)!!
        assertEquals(0.5f, half.fraction, 0.01f)

        // Ten minutes later the bar is emptier, not fuller. Getting this backwards
        // is the single most likely mistake in a hand-written platform version,
        // and it looks plausible until you watch one close.
        val later = ReelClock.face(sig, now + 10 * 60_000)!!
        assertTrue(later.fraction < half.fraction)
    }

    @Test
    fun a_long_auction_uses_its_horizon_so_the_bar_actually_moves() {
        // Nine days out, one hour of legible urgency. Without the horizon the bar
        // would be pinned at ~1.0 for the entire time anyone is watching.
        val nineDays = 9L * 24 * 60 * 60 * 1000
        val sig = signal(ReelClock.MODE_COUNTDOWN, endsAt = iso(nineDays), horizonSeconds = 3_600)

        assertEquals(1f, ReelClock.face(sig, now)!!.fraction, 0.001f)

        // And it still resolves at the end, where the horizon is the denominator.
        val nearEnd = ReelClock.face(sig, now + nineDays - 15 * 60_000)!!
        assertEquals(0.25f, nearEnd.fraction, 0.01f)
    }

    // ── window ───────────────────────────────────────────────────────────────

    @Test
    fun a_window_fills_where_a_countdown_drains() {
        // Three hours total, 1h12m elapsed — the mock's "1h 48m of 3h".
        val sig = signal(
            ReelClock.MODE_WINDOW,
            startsAt = iso(-72 * 60_000),
            endsAt = iso(108 * 60_000),
        )
        val face = ReelClock.face(sig, now)!!
        assertEquals(0.4f, face.fraction, 0.01f)

        val label = assertIs<ReelClock.Label.ElapsedOf>(face.label)
        assertEquals(108, label.remainingMinutes)
        assertEquals(180, label.totalMinutes)
    }

    @Test
    fun a_window_announced_early_shows_empty_rather_than_negative() {
        // A midnight offer is posted in the afternoon. This is a normal state and
        // the reel has to render it as an offer, not as a broken bar.
        val sig = signal(
            ReelClock.MODE_WINDOW,
            startsAt = iso(3 * 60 * 60_000),
            endsAt = iso(6 * 60 * 60_000),
        )
        val face = ReelClock.face(sig, now)!!
        assertEquals(0f, face.fraction)
        assertFalse(face.expired)
    }

    // ── freshness ────────────────────────────────────────────────────────────

    @Test
    fun freshness_reads_in_the_coarsest_unit_that_is_still_true() {
        val threeDays = 3L * 24 * 60 * 60 * 1000
        val sig = signal(
            ReelClock.MODE_FRESHNESS,
            startsAt = iso(-threeDays),
            horizonSeconds = 30 * 24 * 60 * 60,
        )
        val face = ReelClock.face(sig, now)!!

        val label = assertIs<ReelClock.Label.Ago>(face.label)
        assertEquals(3, label.amount)
        assertEquals(ReelClock.Label.Unit.DAY, label.unit)

        // 27 of 30 days left.
        assertEquals(0.9f, face.fraction, 0.01f)
    }

    @Test
    fun a_stale_listing_never_expires() {
        // A car does not stop existing because it got old. Dropping it from the
        // pager would be the overlay making a ranking decision.
        val sig = signal(
            ReelClock.MODE_FRESHNESS,
            startsAt = iso(-200L * 24 * 60 * 60 * 1000),
            horizonSeconds = 30 * 24 * 60 * 60,
        )
        val face = ReelClock.face(sig, now)!!
        assertFalse(face.expired)
        assertEquals(0f, face.fraction)
    }

    // ── cutoff ───────────────────────────────────────────────────────────────

    @Test
    fun a_cutoff_reads_in_the_vendors_zone_not_the_devices() {
        // 12:00Z is 16:00 in Dubai. A viewer whose device sits in UTC must still
        // be told 16:00, because that is the rule in the kitchen's clock — this
        // is the one place the device's own timezone is the wrong answer.
        val sig = signal(
            ReelClock.MODE_CUTOFF,
            endsAt = iso(4 * 60 * 60_000),
            timezone = "Asia/Dubai",
            recurring = true,
        )
        val face = ReelClock.face(sig, now)!!

        val label = assertIs<ReelClock.Label.By>(face.label)
        assertEquals(20, label.hour)
        assertEquals(0, label.minute)
    }

    @Test
    fun a_passed_recurring_cutoff_does_not_drop_the_reel() {
        // 16:00 comes and goes every day. If the client expired the reel, a meal
        // plan would vanish from the feed each evening and reappear a minute
        // later when the projection rolled.
        val sig = signal(
            ReelClock.MODE_CUTOFF,
            endsAt = iso(-60_000),
            timezone = "Asia/Dubai",
            recurring = true,
        )
        assertFalse(ReelClock.face(sig, now)!!.expired)
    }

    // ── forward compatibility ────────────────────────────────────────────────

    @Test
    fun an_unknown_mode_draws_nothing_rather_than_guessing() {
        // A client older than the server will meet one. A wrong bar is worse than
        // no bar: it is a claim about time that happens to be false.
        assertNull(ReelClock.face(signal("auction_extended", endsAt = iso(60_000)), now))
    }

    @Test
    fun a_mode_missing_its_required_field_draws_nothing() {
        // The server validates this on write, so reaching here means a projection
        // was stored incoherently. Rendering half a bar would hide it.
        assertNull(ReelClock.face(signal(ReelClock.MODE_WINDOW, endsAt = iso(60_000)), now))
        assertNull(ReelClock.face(signal(ReelClock.MODE_FRESHNESS), now))
    }
}
