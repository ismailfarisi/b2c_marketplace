package ae.marketplace.mobile.network

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class FailureTest {
    private fun rejected(status: Int, code: String) = ApiException(
        status = status,
        problem = Problem(title = "x", code = code),
        requestId = null,
    ).toFailure()

    @Test
    fun idempotency_codes_reach_the_screen() {
        val inFlight = rejected(409, IDEMPOTENCY_IN_FLIGHT)
        assertEquals(IDEMPOTENCY_IN_FLIGHT, (inFlight as Failure.Rejected).code)
        val reused = rejected(422, IDEMPOTENCY_KEY_REUSED)
        assertEquals(IDEMPOTENCY_KEY_REUSED, (reused as Failure.Rejected).code)
    }

    @Test
    fun only_an_in_flight_refusal_is_worth_retrying() {
        assertTrue(rejected(409, IDEMPOTENCY_IN_FLIGHT).isRetryable)
        assertFalse(rejected(422, IDEMPOTENCY_KEY_REUSED).isRetryable)
        assertFalse(rejected(422, "outside_delivery_area").isRetryable)
    }

    @Test
    fun or_null_swallows_failures() = runTest {
        assertNull(orNullUnlessCancelled<Int> { throw IllegalStateException("boom") })
        assertEquals(3, orNullUnlessCancelled { 3 })
    }

    @Test
    fun or_null_rethrows_cancellation() = runTest {
        assertFailsWith<CancellationException> {
            orNullUnlessCancelled<Int> { throw CancellationException("gone") }
        }
    }
}
