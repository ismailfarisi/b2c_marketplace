package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.KitchenDetail
import ae.marketplace.mobile.model.MenuChoice
import kotlinx.datetime.DatePeriod
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
