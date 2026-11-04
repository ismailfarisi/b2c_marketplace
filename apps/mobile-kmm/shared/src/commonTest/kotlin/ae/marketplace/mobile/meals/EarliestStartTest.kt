package ae.marketplace.mobile.meals

import kotlinx.datetime.LocalDate
import kotlinx.datetime.LocalDateTime
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

// Mirrors services/api/internal/modules/subscriptions/start_test.go so the
// app offers the same first day the API accepts.
class EarliestStartTest {
    private fun at(h: Int, m: Int) = LocalDateTime(2026, 11, 4, h, m)

    @Test fun cutoff20BeforeFour() =
        assertEquals(LocalDate(2026, 11, 5), MealsViewModel.earliestStartAt(20, at(3, 59)))

    @Test fun cutoff20AfterFour() =
        assertEquals(LocalDate(2026, 11, 6), MealsViewModel.earliestStartAt(20, at(4, 0)))

    @Test fun cutoff48() =
        assertEquals(LocalDate(2026, 11, 7), MealsViewModel.earliestStartAt(48, at(9, 0)))

    @Test fun cutoffZero() =
        assertEquals(LocalDate(2026, 11, 5), MealsViewModel.earliestStartAt(0, at(23, 0)))

    @Test fun startValidation() {
        assertTrue(validStart("2026-11-06", "2026-11-06"))
        assertFalse(validStart("2026-11-05", "2026-11-06"))
        assertFalse(validStart("2026-13-01", "2026-11-06"))
        assertFalse(validStart("", "2026-11-06"))
    }
}
