package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.KitchenDetail
import ae.marketplace.mobile.model.MenuChoice
import ae.marketplace.mobile.model.MenuSlot
import ae.marketplace.mobile.model.WeeklyMenu
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue
import kotlin.time.ExperimentalTime
import kotlin.time.Instant

@OptIn(ExperimentalTime::class)
class KitchenMenuTest {
    private fun dish(id: String, allergens: List<String> = emptyList()) = MenuChoice(dishId = id, name = id, allergens = allergens)

    private val kitchen = KitchenDetail(
        branchId = "b1",
        name = "Teta",
        menu = WeeklyMenu(
            listOf(
                MenuSlot(0, "lunch", dish("a")),
                MenuSlot(2, "lunch", dish("b"), dish("c")),
                MenuSlot(4, "lunch", dish("d")),
                MenuSlot(5, "lunch", default = null, alternative = dish("e")),
            ),
        ),
    )

    @Test fun tomorrowIsInDubai() {
        // 22:00 UTC Saturday is 02:00 Sunday in Dubai, so tomorrow is Monday (1).
        assertEquals(1, KitchenMenu.dubaiTomorrowWeekday(Instant.parse("2026-11-07T22:00:00Z")))
        // 10:00 UTC Saturday is still Saturday in Dubai: tomorrow is Sunday (0).
        assertEquals(0, KitchenMenu.dubaiTomorrowWeekday(Instant.parse("2026-11-07T10:00:00Z")))
    }

    @Test fun servedDaysNeedADefaultDish() = assertEquals(listOf(0, 2, 4), KitchenMenu.servedDays(kitchen))

    @Test fun defaultDayIsTomorrowWhenServed() = assertEquals(2, KitchenMenu.defaultDay(listOf(0, 2, 4), 2))

    @Test fun defaultDayIsNextServedDay() {
        assertEquals(4, KitchenMenu.defaultDay(listOf(0, 2, 4), 3))
        // Friday: wraps round to Sunday rather than going back to Thursday.
        assertEquals(0, KitchenMenu.defaultDay(listOf(0, 2, 4), 5))
    }

    @Test fun noDaysNoDefault() = assertNull(KitchenMenu.defaultDay(emptyList(), 3))

    @Test fun choiceDays() {
        assertTrue(KitchenMenu.hasChoice(kitchen, 2))
        assertFalse(KitchenMenu.hasChoice(kitchen, 0))
        assertFalse(KitchenMenu.hasChoice(kitchen, 5))
    }

    @Test fun percent() {
        assertEquals("20%", KitchenMenu.percent(2000))
        assertEquals("12.5%", KitchenMenu.percent(1250))
        assertEquals("7.25%", KitchenMenu.percent(725))
        assertEquals("0.05%", KitchenMenu.percent(5))
    }

    @Test fun allergensAreLocalised() {
        val d = dish("m", listOf("gluten", "sesame", "tree_nuts"))
        assertEquals("Gluten, Sesame, Tree nuts", KitchenMenu.allergenLine(d, arabic = false))
        assertEquals("غلوتين، سمسم، مكسرات", KitchenMenu.allergenLine(d, arabic = true))
        assertEquals("new thing", KitchenMenu.allergen("new_thing", arabic = false))
    }

    @Test
    fun multiWeekMenuTabsUseTheCalendar() {
        val k = kitchen.copy(
            menuWeeks = 2,
            menuCalendar = listOf(
                ae.marketplace.mobile.model.MenuWeek("2026-11-08", listOf(MenuSlot(1, "lunch", dish("kabsa"), week = 2))),
                ae.marketplace.mobile.model.MenuWeek("2026-11-15", listOf(MenuSlot(3, "lunch", dish("biryani"), dish("dal")))),
            ),
        )
        assertEquals(2, KitchenMenu.menuWeeks(k).size)
        assertEquals("kabsa", KitchenMenu.weekSlots(k, 0).single().default?.name)
        assertEquals(listOf(3), KitchenMenu.servedDaysIn(KitchenMenu.weekSlots(k, 1)))
        assertEquals(3, KitchenMenu.openingDay(KitchenMenu.weekSlots(k, 1), 1))
        assertTrue(KitchenMenu.hasChoiceIn(KitchenMenu.weekSlots(k, 1), 3))
        assertEquals("8 Nov – 14 Nov", KitchenMenu.weekRange("2026-11-08", arabic = false))
        assertEquals("29 نوفمبر – 5 ديسمبر", KitchenMenu.weekRange("2026-11-29", arabic = true))
        // A one-week kitchen keeps its single menu.
        assertTrue(KitchenMenu.menuWeeks(kitchen).isEmpty())
        assertEquals(kitchen.menu.slots, KitchenMenu.weekSlots(kitchen, 0))
    }
}
