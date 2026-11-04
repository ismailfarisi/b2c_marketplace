package ae.marketplace.mobile.network

import kotlin.test.Test
import kotlin.test.assertEquals

class ApiErrorTest {
    @Test
    fun field_errors_carry_codes_not_the_server_sentence() {
        val e = ApiException(
            status = 422,
            problem = Problem(
                title = "Validation failed",
                errors = listOf(
                    FieldError(field = "price", code = "required", message = "Price is required"),
                    FieldError(field = "vin", code = "", message = "VIN looks wrong"),
                ),
            ),
            requestId = null,
        )
        assertEquals("required", e.fieldErrors["price"])
        assertEquals("VIN looks wrong", e.fieldErrors["vin"])
    }
}
