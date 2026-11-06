package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.KitchenDetail
import ae.marketplace.mobile.model.MenuChoice
import ae.marketplace.mobile.model.MenuSlot
import ae.marketplace.mobile.model.MenuWeek
import kotlinx.datetime.DatePeriod
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.isoDayNumber
import kotlinx.datetime.plus
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.time.Instant

/**
 * Kitchen-page rules shared by Android and iOS so both open the same day and
 * mark the same days as the web page (kitchens/[branchId]/page.tsx).
 */
@OptIn(ExperimentalTime::class)
object KitchenMenu {
    private val DUBAI = TimeZone.of("Asia/Dubai")

    /** Weekday (0 = Sunday) of tomorrow in Asia/Dubai. */
    fun dubaiTomorrowWeekday(now: Instant = Clock.System.now()): Int =
        now.toLocalDateTime(DUBAI).date.plus(DatePeriod(days = 1)).dayOfWeek.isoDayNumber % 7

    /** Weekdays that have a dish, sorted Sunday first. */
    fun servedDays(k: KitchenDetail): List<Int> =
        k.menu.slots.filter { it.default != null }.map { it.weekday }.distinct().sorted()

    /** The day the menu opens on: Dubai-tomorrow when it is served, else the next served day after it. */
    fun defaultDay(k: KitchenDetail): Int? = defaultDay(servedDays(k), dubaiTomorrowWeekday())

    fun defaultDay(days: List<Int>, from: Int): Int? = days.minByOrNull { ((it - from) % 7 + 7) % 7 }

    /** True when a slot on [day] offers a second main to choose from. */
    fun hasChoice(k: KitchenDetail, day: Int): Boolean =
        k.menu.slots.any { it.weekday == day && it.default != null && it.alternative != null }

    /**
     * The calendar weeks a multi-week kitchen's page tabs between ("This week",
     * "Next week", ...), this week first. Empty for a one-week menu, which
     * keeps the single weekly menu.
     */
    fun menuWeeks(k: KitchenDetail): List<MenuWeek> =
        if (k.menuWeeks > 1 && k.menuCalendar.size > 1) k.menuCalendar else emptyList()

    /** The cells shown under week tab [index]: that calendar week, or the live menu. */
    fun weekSlots(k: KitchenDetail, index: Int): List<MenuSlot> =
        menuWeeks(k).getOrNull(index)?.slots ?: k.menu.slots

    /** Weekdays of [slots] that have a dish, Sunday first. */
    fun servedDaysIn(slots: List<MenuSlot>): List<Int> =
        slots.filter { it.default != null }.map { it.weekday }.distinct().sorted()

    /** The day a week tab opens on: this week from Dubai-tomorrow, later weeks from their first day. */
    fun openingDay(slots: List<MenuSlot>, weekIndex: Int): Int? = openingDayAt(slots, weekIndex, Clock.System.now())

    fun openingDayAt(slots: List<MenuSlot>, weekIndex: Int, now: Instant): Int? =
        if (weekIndex == 0) defaultDay(servedDaysIn(slots), dubaiTomorrowWeekday(now)) else servedDaysIn(slots).firstOrNull()

    fun hasChoiceIn(slots: List<MenuSlot>, day: Int): Boolean =
        slots.any { it.weekday == day && it.default != null && it.alternative != null }

    private val monthsEn = listOf("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec")
    private val monthsAr = listOf("يناير", "فبراير", "مارس", "أبريل", "مايو", "يونيو", "يوليو", "أغسطس", "سبتمبر", "أكتوبر", "نوفمبر", "ديسمبر")

    /** "8 Nov – 14 Nov" for the week starting [startsOn]; the input unchanged when it is not a date. */
    fun weekRange(startsOn: String, arabic: Boolean): String {
        val start = runCatching { LocalDate.parse(startsOn) }.getOrNull() ?: return startsOn
        val end = start.plus(DatePeriod(days = 6))
        val months = if (arabic) monthsAr else monthsEn
        fun fmt(d: LocalDate) = "${d.day} ${months[d.month.ordinal]}"
        return "${fmt(start)} – ${fmt(end)}"
    }

    /** "20%" from 2000 basis points; up to two decimals ("12.5%"). */
    fun percent(bp: Int): String {
        val whole = bp / 100
        val frac = bp % 100
        return when {
            frac == 0 -> "$whole%"
            frac % 10 == 0 -> "$whole.${frac / 10}%"
            else -> "$whole.${frac.toString().padStart(2, '0')}%"
        }
    }

    /**
     * The API's closed allergen list (kitchen.Allergens: the fourteen EU/GSO
     * labelling names), in English and Arabic. Unknown codes fall back to the code.
     */
    private val allergens: Map<String, Pair<String, String>> = mapOf(
        "gluten" to ("Gluten" to "غلوتين"),
        "crustaceans" to ("Crustaceans" to "قشريات"),
        "eggs" to ("Eggs" to "بيض"),
        "fish" to ("Fish" to "سمك"),
        "peanuts" to ("Peanuts" to "فول سوداني"),
        "soy" to ("Soy" to "صويا"),
        "milk" to ("Milk" to "حليب"),
        "tree_nuts" to ("Tree nuts" to "مكسرات"),
        "celery" to ("Celery" to "كرفس"),
        "mustard" to ("Mustard" to "خردل"),
        "sesame" to ("Sesame" to "سمسم"),
        "sulphites" to ("Sulphites" to "كبريتيت"),
        "lupin" to ("Lupin" to "ترمس"),
        "molluscs" to ("Molluscs" to "رخويات"),
    )

    fun allergen(code: String, arabic: Boolean): String =
        allergens[code]?.let { if (arabic) it.second else it.first } ?: code.replace('_', ' ')

    /** The dish's allergens as one comma-separated line, empty when it declares none. */
    fun allergenLine(choice: MenuChoice, arabic: Boolean): String =
        choice.allergens.joinToString(if (arabic) "، " else ", ") { allergen(it, arabic) }
}
