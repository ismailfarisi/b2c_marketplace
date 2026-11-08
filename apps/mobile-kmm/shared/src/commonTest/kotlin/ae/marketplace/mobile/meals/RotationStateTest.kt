package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.Kitchen
import ae.marketplace.mobile.model.Rotation
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotEquals
import kotlin.test.assertTrue

class RotationStateTest {
    private fun rotation(status: String, renewedBy: String? = null, renewedByStatus: String? = null) =
        Rotation(
            status = status, startDate = "2026-11-09", endDate = "2026-12-06", weeks = emptyList(), amountMinor = 0,
            renewedById = renewedBy, renewedByStatus = renewedByStatus,
        )

    @Test
    fun unpaidRotationIsPayableAndLive() {
        val r = rotation("pending_payment")
        assertTrue(r.awaitingPayment)
        assertTrue(r.live)
        assertFalse(r.renewable)
    }

    @Test
    fun onlyActiveOrCompletedUnrenewedRotationsRenew() {
        assertTrue(rotation("active").renewable)
        assertTrue(rotation("completed").renewable)
        assertFalse(rotation("cancelled").renewable)
        assertFalse(rotation("active", renewedBy = "next").renewable)
        assertFalse(rotation("active", renewedBy = "next", renewedByStatus = "active").renewable)
        assertFalse(rotation("cancelled").live)
    }

    @Test
    fun unpaidAutomaticRenewalCanStillBeEdited() {
        val r = rotation("active", renewedBy = "next", renewedByStatus = "pending_payment")
        assertTrue(r.renewalPending)
        assertTrue(r.renewable)
        assertFalse(rotation("active", renewedBy = "next", renewedByStatus = "active").renewalPending)
    }

    @Test
    fun eachBuilderSessionHasItsOwnKey() {
        val a = RotationUiState()
        assertEquals(36, a.idempotencyKey.length)
        assertNotEquals(a.idempotencyKey, RotationUiState().idempotencyKey)
    }

    @Test
    fun fewerThanFourKitchensIsReportedOnlyOnceLoaded() {
        val three = List(3) { Kitchen(branchId = "k$it", name = "K") }
        assertFalse(RotationUiState(candidates = three).tooFewKitchens)
        assertTrue(RotationUiState(candidates = three, candidatesLoaded = true).tooFewKitchens)
        assertFalse(RotationUiState(candidates = three + Kitchen(branchId = "k4", name = "K"), candidatesLoaded = true).tooFewKitchens)
    }
}
