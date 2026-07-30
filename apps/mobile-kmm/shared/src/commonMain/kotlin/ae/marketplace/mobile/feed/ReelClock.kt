package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.TimeSignal
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlin.time.ExperimentalTime
import kotlin.time.Instant
import kotlinx.datetime.TimeZone
import kotlinx.datetime.toLocalDateTime

/**
 * Turns a [TimeSignal] into the two things a reel overlay draws: a bar and a
 * pill.
 *
 * In `:shared` for the reason ReelFeed.kt states about everything else here —
 * *every decision that costs money or corrupts data lives in :shared and is
 * tested there* — and this one qualifies on both counts. Skew correction decides
 * whether a viewer can bid; the progress fraction decides whether a bar drains
 * or fills. Written twice, the two platforms disagree about whether an auction
 * is open, and the way that surfaces is an iOS user tapping into a 409 three
 * months later.
 *
 * What is left to Compose and SwiftUI is a rectangle and a string, which is the
 * part that genuinely has to be written twice.
 */
@OptIn(ExperimentalTime::class)
object ReelClock {

    /**
     * What the overlay needs, resolved. Nothing here needs interpreting.
     */
    data class Face(
        /**
         * How full the bar is, 0f..1f.
         *
         * Already oriented: a countdown returns a value that *falls* toward zero
         * and a window one that *rises* toward one, so the platform draws
         * `fraction` and never asks which way round this mode goes. Putting that
         * question in the UI layer is how the two platforms end up drawing
         * opposite bars for the same auction.
         */
        val fraction: Float,
        /** The pill's shape. The platform picks words; this picks which words. */
        val label: Label,
        /**
         * The clock has run out. For a recurring signal this is momentary — the
         * next reconcile rolls it forward — and for everything else it means the
         * reel should leave the pager.
         */
        val expired: Boolean,
    )

    /**
     * A rendered pill, as data rather than as a string.
     *
     * The formatting is deliberately not done here even though the arithmetic
     * is. Digits are locale-dependent (Arabic-Indic vs Western), plurals are
     * language-dependent, and both platforms have a formatter that already knows
     * the device's rules. What is *not* platform-dependent is which of these five
     * shapes to use and what numbers go in it, and that is what this returns.
     */
    sealed interface Label {
        /** "02:04 left" — a countdown under an hour, at second resolution. */
        data class Remaining(val hours: Int, val minutes: Int, val seconds: Int) : Label

        /**
         * "1h 48m of 3h" — inside a window.
         *
         * Minutes rather than [Duration]. Duration is an inline value class and
         * inline value classes are not exported to Objective-C, so a Duration
         * here would be silently absent from the generated framework and the
         * iOS overlay could not read it. Every exported type in this file is
         * chosen for that constraint.
         */
        data class ElapsedOf(val remainingMinutes: Int, val totalMinutes: Int) : Label

        /** "listed 3d ago" — freshness, at the coarsest unit that is still true. */
        data class Ago(val amount: Int, val unit: Unit) : Label

        /** "order by 16:00" — a wall-clock time in the vendor's zone, not the device's. */
        data class By(val hour: Int, val minute: Int) : Label

        /** The window shut. Recurring signals never reach this. */
        data object Closed : Label

        enum class Unit { MINUTE, HOUR, DAY, WEEK }
    }

    /**
     * The correction between the server's clock and this device's.
     *
     * **Sampled once, when the page lands, and then held.** [deviceNowMillis]
     * must be the device clock at the moment the response arrived — not at the
     * moment a countdown ticks.
     *
     * The distinction is not pedantic; getting it wrong is silent and total.
     * Recomputing this on every tick makes the corrected now equal `as_of`
     * forever, because the growing device clock and the growing correction
     * cancel exactly — and the visible symptom is a countdown frozen at the
     * value it had when the page loaded, which looks like a paused animation
     * rather than a clock bug.
     *
     * The correction itself is why `as_of` exists on the wire at all. A device
     * four minutes fast renders a live auction as already closed and the viewer
     * concludes the app is broken; four minutes slow lets them tap into a closed
     * lot and eat a 409. Neither is exotic — a manually set clock and a phone
     * that has not reached an NTP server since a flight are both ordinary.
     */
    fun skew(signal: TimeSignal, deviceNowMillis: Long): Long {
        val asOf = parse(signal.asOf) ?: return 0L
        return asOf.toEpochMilliseconds() - deviceNowMillis
    }

    /**
     * Resolves a signal against the device clock, corrected by [skew].
     *
     * Both clock inputs are parameters rather than read here, which makes this a
     * pure function: the platform ticks a timer and calls it, and a test runs the
     * whole of an auction's last two minutes in microseconds. That is the only
     * reason the frozen-countdown bug above was found before either platform
     * drew a single bar.
     */
    fun face(signal: TimeSignal, deviceNowMillis: Long, skewMillis: Long = 0L): Face? {
        val now = deviceNowMillis + skewMillis
        val starts = signal.startsAt?.let { parse(it) }?.toEpochMilliseconds()
        val ends = signal.endsAt?.let { parse(it) }?.toEpochMilliseconds()
        val horizon = signal.horizonSeconds?.seconds

        return when (signal.mode) {
            MODE_COUNTDOWN -> countdown(now, starts, ends ?: return null, horizon, signal.recurring)
            MODE_WINDOW -> window(now, starts ?: return null, ends ?: return null)
            MODE_FRESHNESS -> freshness(now, starts ?: return null, horizon ?: return null)
            MODE_CUTOFF -> cutoff(now, ends ?: return null, signal.timezone, horizon, signal.recurring)
            // An unknown mode draws nothing rather than guessing. A client older
            // than the server will meet one, and a wrong bar is worse than no bar
            // — it is a claim about time that happens to be false.
            else -> null
        }
    }

    private fun countdown(
        now: Long, starts: Long?, ends: Long, horizon: Duration?, recurring: Boolean,
    ): Face {
        val remainingMs = ends - now
        if (remainingMs <= 0) {
            return Face(0f, if (recurring) Label.Remaining(0, 0, 0) else Label.Closed, expired = true)
        }

        // The bar's denominator: the explicit horizon when the vertical set one,
        // otherwise the interval itself. A nine-day auction sets a horizon
        // precisely so the bar moves during the two minutes anyone is watching.
        val span = horizon?.inWholeMilliseconds
            ?: starts?.let { ends - it }
            ?: remainingMs
        val fraction = if (span <= 0) 0f else (remainingMs.toFloat() / span).coerceIn(0f, 1f)

        val total = remainingMs / 1000
        return Face(
            fraction = fraction,
            label = Label.Remaining(
                hours = (total / 3600).toInt(),
                minutes = ((total % 3600) / 60).toInt(),
                seconds = (total % 60).toInt(),
            ),
            expired = false,
        )
    }

    private fun window(now: Long, starts: Long, ends: Long): Face {
        val total = ends - starts
        if (now >= ends) return Face(1f, Label.Closed, expired = true)

        // Before it opens the bar sits empty rather than negative. A window
        // announced early is a normal state — "midnight offer" is posted in the
        // afternoon — and clamping is what makes the reel show the offer rather
        // than a broken bar.
        val elapsed = (now - starts).coerceAtLeast(0L)
        val fraction = if (total <= 0) 1f else (elapsed.toFloat() / total).coerceIn(0f, 1f)

        return Face(
            fraction = fraction,
            label = Label.ElapsedOf(
                remainingMinutes = ((ends - now) / 60_000).toInt(),
                totalMinutes = (total / 60_000).toInt(),
            ),
            expired = false,
        )
    }

    private fun freshness(now: Long, starts: Long, horizon: Duration): Face {
        val age = (now - starts).coerceAtLeast(0L)
        val span = horizon.inWholeMilliseconds

        // Drains as it ages, so the bar means the same thing it does on a
        // countdown: how much is left. A freshness bar that filled up would read
        // as progress toward something.
        val fraction = if (span <= 0) 0f else (1f - age.toFloat() / span).coerceIn(0f, 1f)

        val minutes = age / 60_000
        val label = when {
            minutes < 60 -> Label.Ago(minutes.toInt().coerceAtLeast(1), Label.Unit.MINUTE)
            minutes < 60 * 24 -> Label.Ago((minutes / 60).toInt(), Label.Unit.HOUR)
            minutes < 60 * 24 * 14 -> Label.Ago((minutes / (60 * 24)).toInt(), Label.Unit.DAY)
            else -> Label.Ago((minutes / (60 * 24 * 7)).toInt(), Label.Unit.WEEK)
        }

        // Never expired. A freshness signal has no end — a listing does not stop
        // existing because it got old, it just stops being interesting, and that
        // is ranking's problem rather than the overlay's.
        return Face(fraction, label, expired = false)
    }

    private fun cutoff(
        now: Long, ends: Long, timezone: String?, horizon: Duration?, recurring: Boolean,
    ): Face {
        val remainingMs = ends - now
        if (remainingMs <= 0 && !recurring) return Face(0f, Label.Closed, expired = true)

        // The wall time in the *vendor's* zone. This is the one place a device's
        // own timezone is the wrong answer: "order by 16:00" is a rule in the
        // kitchen's clock, and a viewer in another emirate reading their own
        // 16:00 would miss the cutoff by exactly the offset between them.
        val zone = timezone?.let { runCatching { TimeZone.of(it) }.getOrNull() } ?: TimeZone.UTC
        val local = Instant.fromEpochMilliseconds(ends).toLocalDateTime(zone)

        // Cycle length as the denominator — a day unless the vertical said
        // otherwise. Without one the bar would have nothing to be a fraction of,
        // because a recurring boundary has no start.
        val span = horizon?.inWholeMilliseconds ?: (24L * 60 * 60 * 1000)
        val fraction = if (span <= 0) 0f
        else (remainingMs.toFloat() / span).coerceIn(0f, 1f)

        return Face(
            fraction = fraction,
            label = Label.By(local.hour, local.minute),
            // Recurring cutoffs are never expired to the client. The instant has
            // passed, the projection will roll it, and dropping the reel in the
            // meantime would make a meal plan vanish from the feed every evening
            // at 16:00 and reappear a minute later.
            expired = remainingMs <= 0 && !recurring,
        )
    }

    private fun parse(iso: String): Instant? = runCatching { Instant.parse(iso) }.getOrNull()

    const val MODE_COUNTDOWN = "countdown"
    const val MODE_WINDOW = "window"
    const val MODE_FRESHNESS = "freshness"
    const val MODE_CUTOFF = "cutoff"
}
