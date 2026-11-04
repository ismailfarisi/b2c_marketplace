package ae.marketplace.mobile.push

import kotlin.test.Test
import kotlin.test.assertEquals

class PushRetryTest {
    @Test
    fun backoffDoublesAndCaps() {
        assertEquals(2_000L, retryDelayMs(0))
        assertEquals(4_000L, retryDelayMs(1))
        assertEquals(16_000L, retryDelayMs(3))
        assertEquals(300_000L, retryDelayMs(8))
        assertEquals(300_000L, retryDelayMs(50))
    }
}
