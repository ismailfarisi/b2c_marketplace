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
        assertEquals("+966 551230003", PhoneCountry.AE.display("+966551230003"))
        assertEquals("+966 551230003", PhoneCountry.AE.display("00966551230003"))
    }

    @Test
    fun theDialCodeIsShownOnceWhenTypedWithoutAPlus() {
        assertEquals("+971 501112244", PhoneCountry.AE.display("971501112244"))
        assertEquals("+971 501112244", PhoneCountry.AE.display("+971 0501112244"))
        assertEquals("+447700900123", PhoneCountry.AE.display("+447700900123"))
    }

    @Test
    fun normalizeMatchesTheServer() {
        assertEquals("+971501112244", PhoneCountry.AE.normalize("971501112244"))
        assertEquals("+971501112244", PhoneCountry.AE.normalize("050 111 2244"))
        assertEquals("+966551230003", PhoneCountry.SA.normalize("0551230003"))
        assertEquals("+97433123456", PhoneCountry.QA.normalize("33123456"))
        assertEquals(null, PhoneCountry.QA.normalize("0551230003"))
        assertEquals(null, PhoneCountry.AE.normalize("05011"))
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
