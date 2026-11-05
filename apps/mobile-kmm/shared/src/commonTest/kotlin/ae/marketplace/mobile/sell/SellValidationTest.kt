package ae.marketplace.mobile.sell

import ae.marketplace.mobile.model.SellField
import ae.marketplace.mobile.model.SellForm
import ae.marketplace.mobile.model.SellOption
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * These tests never mention a car.
 *
 * That is the point of them. Every rule below comes from the form the server
 * sent, so the fixtures here are deliberately made-up fields — if a test needed
 * to know what `mileage_km` means in order to pass, the validation would have
 * leaked a vertical into shared code.
 */
class SellValidationTest {

    private fun form(vararg fields: SellField) = SellForm(fields = fields.toList())

    @Test
    fun a_missing_required_answer_is_reported_against_its_own_field() {
        val f = form(
            SellField(key = "a", label = "A", kind = "text", required = true),
            SellField(key = "b", label = "B", kind = "text", required = true),
        )
        val issues = SellValidation.check(f, mapOf("a" to "answered"))

        assertEquals(1, issues.size)
        assertEquals("b", issues[0].key)
        assertEquals(FieldProblem.Required, issues[0].problem)
    }

    @Test
    fun an_optional_field_left_blank_is_not_an_issue() {
        val f = form(SellField(key = "a", label = "A", kind = "text"))
        assertTrue(SellValidation.check(f, emptyMap()).isEmpty())
    }

    @Test
    fun a_blank_optional_number_is_absent_rather_than_zero() {
        // The bug this catches: range-checking an empty box treats it as 0,
        // which fails a `min` and reports "too small" for a field the seller was
        // entitled to skip.
        val f = form(SellField(key = "n", label = "N", kind = "integer", min = 100))
        assertTrue(SellValidation.check(f, mapOf("n" to "  ")).isEmpty())
    }

    @Test
    fun whitespace_alone_does_not_answer_a_required_field() {
        val f = form(SellField(key = "a", label = "A", kind = "text", required = true))
        val issues = SellValidation.check(f, mapOf("a" to "   "))
        assertEquals(FieldProblem.Required, issues.single().problem)
    }

    @Test
    fun the_bound_travels_with_the_issue() {
        // The message is written by the platform, so shared code has to hand it
        // the number. Without this the string table would need its own copy of
        // every vertical's limits, which is the leak in a different file.
        val f = form(SellField(key = "n", label = "N", kind = "money", min = 100_000, max = 900_000))

        // Typed in major units: 500 AED is 50,000 fils, under the 1,000 AED floor.
        val low = SellValidation.check(f, mapOf("n" to "500")).single()
        assertEquals(FieldProblem.TooSmall, low.problem)
        assertEquals(100_000L, low.bound)

        val high = SellValidation.check(f, mapOf("n" to "10000")).single()
        assertEquals(FieldProblem.TooLarge, high.problem)
        assertEquals(900_000L, high.bound)
    }

    @Test
    fun a_value_on_the_boundary_is_accepted() {
        val f = form(SellField(key = "n", label = "N", kind = "integer", min = 0, max = 2_000_000))
        assertTrue(SellValidation.check(f, mapOf("n" to "0")).isEmpty())
        assertTrue(SellValidation.check(f, mapOf("n" to "2000000")).isEmpty())
    }

    @Test
    fun the_grouping_the_field_renders_is_accepted_back() {
        // The box shows "64,000" as the seller types and some clients post back
        // what they render. Refusing that is a validation error the seller
        // cannot act on, because the string they are being shown looks correct.
        val f = form(SellField(key = "n", label = "N", kind = "integer", max = 100_000))
        for (typed in listOf("64000", "64,000", "64 000", " 64000 ")) {
            assertTrue(
                SellValidation.check(f, mapOf("n" to typed)).isEmpty(),
                "rejected $typed",
            )
        }
    }

    @Test
    fun text_that_is_not_a_number_is_caught_before_the_round_trip() {
        val f = form(SellField(key = "n", label = "N", kind = "integer"))
        val issue = SellValidation.check(f, mapOf("n" to "sixty four thousand")).single()
        assertEquals(FieldProblem.NotANumber, issue.problem)
    }

    @Test
    fun a_select_answer_outside_the_offered_options_is_refused() {
        val f = form(
            SellField(
                key = "s", label = "S", kind = "select", required = true,
                options = listOf(SellOption("gcc", "GCC"), SellOption("us", "American")),
            )
        )
        assertEquals(
            FieldProblem.NotAnOption,
            SellValidation.check(f, mapOf("s" to "martian")).single().problem,
        )
        assertTrue(SellValidation.check(f, mapOf("s" to "gcc")).isEmpty())
    }

    @Test
    fun a_select_that_arrived_with_no_options_does_not_blame_the_seller() {
        // A malformed form is the server's fault. Rejecting whatever the seller
        // managed to enter would make it look like theirs.
        val f = form(SellField(key = "s", label = "S", kind = "select", required = true))
        assertTrue(SellValidation.check(f, mapOf("s" to "anything")).isEmpty())
    }

    @Test
    fun a_lookup_answer_is_never_second_guessed() {
        // The value is an opaque token the vertical minted. The client has no
        // way to check it and must not invent one — the moment it parses the
        // format, the format cannot change without an app release.
        val f = form(SellField(key = "car", label = "Car", kind = "lookup", required = true, source = "/v1/x"))
        assertTrue(SellValidation.check(f, mapOf("car" to "nissan/patrol/2021")).isEmpty())
        assertTrue(SellValidation.check(f, mapOf("car" to "anything at all")).isEmpty())
    }

    @Test
    fun a_field_kind_this_build_does_not_know_is_treated_as_text() {
        // A newer server offering a kind this app predates must still produce a
        // usable form. Failing closed here would make one added field break the
        // whole flow for every installed copy.
        val f = form(SellField(key = "x", label = "X", kind = "colour_wheel", required = true))
        assertTrue(SellValidation.check(f, mapOf("x" to "teal")).isEmpty())
        assertEquals(
            FieldProblem.Required,
            SellValidation.check(f, emptyMap()).single().problem,
        )
    }

    @Test
    fun completeness_is_the_absence_of_issues() {
        val f = form(
            SellField(key = "car", label = "Car", kind = "lookup", required = true, source = "/v1/x"),
            SellField(key = "n", label = "N", kind = "integer", required = true, min = 0),
        )
        assertTrue(!SellValidation.isComplete(f, mapOf("car" to "a/b/c")))
        assertTrue(SellValidation.isComplete(f, mapOf("car" to "a/b/c", "n" to "12,000")))
    }

    @Test
    fun a_negative_number_parses_so_that_min_can_reject_it() {
        // Not stripped as a stray character. If the minus were dropped, "-500"
        // would pass a min of 0 as five hundred.
        assertEquals(-500L, SellValidation.parseNumber("-500"))

        val f = form(SellField(key = "n", label = "N", kind = "integer", min = 0))
        assertEquals(FieldProblem.TooSmall, SellValidation.check(f, mapOf("n" to "-500")).single().problem)
    }

    @Test
    fun a_decimal_integer_is_not_silently_truncated() {
        // parseNumber is for whole numbers; dropping the fraction of "1890.50"
        // would quietly change what the seller typed.
        assertNull(SellValidation.parseNumber("1890.50"))
    }

    @Test
    fun a_money_answer_is_typed_in_major_units_and_checked_in_minor() {
        // The live bug: a 1,000 AED floor (100,000 fils) rejected a 2,500 AED
        // car because the typed 2500 was compared to the bound as fils.
        val f = form(SellField(key = "price", label = "Price", kind = "money", required = true, min = 100_000))
        assertTrue(SellValidation.check(f, mapOf("price" to "2500")).isEmpty())
        assertTrue(SellValidation.check(f, mapOf("price" to "1,000")).isEmpty())
        val low = SellValidation.check(f, mapOf("price" to "999.99")).single()
        assertEquals(FieldProblem.TooSmall, low.problem)
        assertEquals("1,000", SellValidation.boundText(f.fields.single(), low))
    }

    @Test
    fun money_is_posted_in_minor_units() {
        // The live bug: 150,000 AED was posted verbatim and stored as 1,500 AED.
        val f = form(
            SellField(key = "price", label = "Price", kind = "money", required = true),
            SellField(key = "km", label = "KM", kind = "integer"),
            SellField(key = "spec", label = "Spec", kind = "text"),
        )
        val wire = SellValidation.toWire(
            f, mapOf("price" to "150,000", "km" to "64,000", "spec" to "gcc"),
        )
        assertEquals("15000000", wire["price"])
        assertEquals("64,000", wire["km"])
        assertEquals("gcc", wire["spec"])
    }

    @Test
    fun money_conversion_handles_fils_and_refuses_what_is_not_a_price() {
        assertEquals(189_050L, SellValidation.moneyToMinor("1890.5"))
        assertEquals(189_005L, SellValidation.moneyToMinor("1,890.05"))
        assertEquals(150_000_00L, SellValidation.moneyToMinor(" 150 000 "))
        assertEquals(-50_000L, SellValidation.moneyToMinor("-500"))
        assertNull(SellValidation.moneyToMinor("1890.505"))
        assertNull(SellValidation.moneyToMinor("1.2.3"))
        assertNull(SellValidation.moneyToMinor("abc"))
        assertNull(SellValidation.moneyToMinor("."))
        assertNull(SellValidation.moneyToMinor("99999999999999999999"))
        assertEquals("NotANumber", SellValidation.check(
            form(SellField(key = "p", label = "P", kind = "money")), mapOf("p" to "12.345"),
        ).single().problem.code)
    }

    @Test
    fun minor_units_display_as_major() {
        assertEquals("1,500", SellValidation.minorToMajor(150_000))
        assertEquals("150,000", SellValidation.minorToMajor(15_000_000))
        assertEquals("1,890.50", SellValidation.minorToMajor(189_050))
        assertEquals("0.05", SellValidation.minorToMajor(5))
        val km = SellField(key = "km", label = "KM", kind = "integer")
        assertEquals("2,000,000", SellValidation.boundText(km, FieldIssue("km", FieldProblem.TooLarge, 2_000_000)))
    }

    @Test
    fun server_field_codes_become_issues_without_the_server_sentence() {
        val f = form(
            SellField(key = "price", label = "Price", kind = "money", required = true),
            SellField(
                key = "spec", label = "Spec", kind = "select",
                options = listOf(SellOption(value = "gcc", label = "GCC")),
            ),
            SellField(key = "vin", label = "VIN", kind = "text"),
        )
        val issues = SellValidation.fromServer(
            f,
            mapOf("price" to "required", "spec" to "invalid", "vin" to "too_long", "region" to "required"),
        ).associateBy { it.key }

        assertEquals(FieldProblem.Required, issues["price"]?.problem)
        assertEquals(FieldProblem.NotAnOption, issues["spec"]?.problem)
        assertEquals(FieldProblem.Invalid, issues["vin"]?.problem)
        // Not on this form: left for the form-level failure, not dropped under a box.
        assertNull(issues["region"])
    }
}
