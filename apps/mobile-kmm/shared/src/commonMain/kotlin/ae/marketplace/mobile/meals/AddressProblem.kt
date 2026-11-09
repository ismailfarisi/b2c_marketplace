package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.ScheduleRule
import ae.marketplace.mobile.network.ApiException
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.toFailure

/**
 * Why an address was refused, in a shape the screens can word in either
 * language. [kitchen], [slot] and [week] come from the server's message
 * ("Teta does not deliver lunch to that address (week 2)") when it named them.
 */
data class AddressProblem(
    /** outside, gone, company, closed, rotation_week or unknown_slot. */
    val kind: String,
    val kitchen: String? = null,
    val slot: String? = null,
    val week: Int? = null,
) {
    /** Swift has no Int? that reads cleanly: 0 when no week was named. */
    val weekNo: Int get() = week ?: 0

    companion object {
        const val OUTSIDE = "outside"
        const val GONE = "gone"
        const val COMPANY = "company"
        const val CLOSED = "closed"
        const val ROTATION_WEEK = "rotation_week"
        const val UNKNOWN_SLOT = "unknown_slot"

        private val outsideMessage =
            Regex("""^(.+?) does not deliver (?:(\w+) )?to that address(?: \(week (\d+)\))?\s*$""")

        /** Reads "<kitchen> does not deliver <slot> to that address (week N)"; "this kitchen" names none. */
        fun parseOutside(message: String?): AddressProblem {
            val m = message?.let { outsideMessage.find(it.trim()) } ?: return AddressProblem(OUTSIDE)
            val kitchen = m.groupValues[1].takeUnless { it.equals("this kitchen", ignoreCase = true) || it.isBlank() }
            return AddressProblem(
                OUTSIDE,
                kitchen = kitchen,
                slot = m.groupValues[2].ifBlank { null },
                week = m.groupValues[3].toIntOrNull(),
            )
        }

        private fun ofCode(code: String?, message: String?): AddressProblem? = when (code) {
            "outside_delivery_area" -> parseOutside(message)
            "not_found", "address_not_found" -> AddressProblem(GONE)
            "unknown_slot" -> AddressProblem(UNKNOWN_SLOT)
            else -> null
        }

        /** The address refusal in [f] under [field] (or the top-level code), or null for any other failure. */
        fun of(f: Failure?, field: String): AddressProblem? {
            val r = f as? Failure.Rejected ?: return null
            r.fieldCodes[field]?.let { return ofCode(it, r.fieldMessages[field]) }
            return when (r.code) {
                "company_lunch" -> AddressProblem(COMPANY)
                "not_changeable" -> AddressProblem(CLOSED)
                "rotation_week" -> AddressProblem(ROTATION_WEEK)
                "address_not_found" -> AddressProblem(GONE)
                else -> null
            }
        }

        /** A rotation create/preview/suggestion refusal: per-meal addresses first, then the default. */
        fun ofRotation(f: Failure?): AddressProblem? = of(f, "slot_addresses") ?: of(f, "address_id")
            ?.takeIf { it.kind != COMPANY && it.kind != CLOSED && it.kind != ROTATION_WEEK }

        /** A change-address refusal: the `addresses` field or the 409/404 codes. */
        fun ofChange(f: Failure?): AddressProblem? = of(f, "addresses")

        /**
         * Checkout refusals by meal slot: `schedule[i].address_id` is rule i of
         * [schedule]; the default `address_id` belongs to every meal sent to
         * [defaultAddress] when [perMeal], else to none (shown under the one picker).
         */
        internal fun bySlot(
            f: Failure?,
            schedule: List<ScheduleRule>,
            defaultAddress: String,
            perMeal: Boolean,
        ): Map<String, AddressProblem> {
            val r = f as? Failure.Rejected ?: return emptyMap()
            val out = linkedMapOf<String, AddressProblem>()
            val rule = Regex("""^schedule\[(\d+)]\.address_id$""")
            for ((field, code) in r.fieldCodes) {
                val i = rule.find(field)?.groupValues?.get(1)?.toIntOrNull() ?: continue
                val slot = schedule.getOrNull(i)?.mealSlot ?: continue
                ofCode(code, r.fieldMessages[field])?.let { out.getOrPut(slot) { it } }
            }
            if (perMeal) {
                r.fieldCodes["address_id"]?.let { code ->
                    ofCode(code, r.fieldMessages["address_id"])?.let { p ->
                        schedule.filter { (it.addressId ?: defaultAddress) == defaultAddress }
                            .forEach { out.getOrPut(it.mealSlot) { p } }
                    }
                }
            }
            return out
        }

        /**
         * Classifies a change-address failure: a 404 that names `address_not_found`
         * is the address being gone, not the plan, so it is kept as a refusal.
         */
        internal fun failureOf(e: Throwable): Failure =
            if (e is ApiException && e.status == 404 && e.code == "address_not_found") {
                Failure.Rejected(code = "address_not_found")
            } else {
                e.toFailure()
            }
    }
}
