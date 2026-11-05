package ae.marketplace.mobile.identity

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class PhoneCountryTest {
    @Test
    fun uaeIsTheDefaultAndAllSixGccCountriesAreOffered() {
        assertEquals(PhoneCountry.AE, PhoneCountry.Default)
        assertEquals(
            listOf("+971", "+966", "+974", "+965", "+973", "+968"),
            PhoneCountry.all.map { it.dialCode },
        )
    }

    @Test
    fun aNationalNumberIsShownInThePickedCountry() {
        assertEquals("+966 551230003", PhoneCountry.SA.display("0551230003"))
        assertEquals("+971 551230003", PhoneCountry.AE.display("055 123 0003"))
        assertEquals("+974 33123456", PhoneCountry.QA.display("3312 3456"))
        // Typed with its own code, the picker does not rewrite it.
        assertEquals("+966551230003", PhoneCountry.AE.display("+966551230003"))
        assertEquals("+966551230003", PhoneCountry.AE.display("00966551230003"))
    }

    @Test
    fun sendWaitsForEnoughDigitsForTheCountry() {
        assertFalse(PhoneCountry.AE.plausible("05012345"))
        assertTrue(PhoneCountry.AE.plausible("501234567"))
        assertTrue(PhoneCountry.QA.plausible("33123456"))
        assertFalse(PhoneCountry.QA.plausible("3312345"))
        assertTrue(PhoneCountry.QA.plausible("+971501234567"))
    }
}
