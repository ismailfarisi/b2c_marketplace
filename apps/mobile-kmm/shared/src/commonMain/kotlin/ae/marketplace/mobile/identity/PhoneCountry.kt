package ae.marketplace.mobile.identity

/**
 * The sign-in country picker: the six GCC states, UAE first as the default.
 *
 * A national number means nothing without its country — 0551230003 is a valid
 * Emirati and a valid Saudi number — so the picked country goes to the server
 * with the number and the server reads the national form in it. Kept in step
 * with `identity/phone.go` in services/api and `lib/phone.ts` on the web.
 */
enum class PhoneCountry(
    /** ISO 3166 code, the `country` the API takes. */
    val iso: String,
    /** Country calling code without the plus. */
    val dial: String,
    /** Digits after the calling code. */
    val nsn: Int,
    /** Whether numbers are written with a leading 0 at home (050…, 055…). */
    val trunk: Boolean,
    /** How a number is written locally, for the placeholder. Digits only, so one string serves both languages. */
    val example: String,
) {
    AE("AE", "971", 9, true, "050 123 4567"),
    SA("SA", "966", 9, true, "055 123 4567"),
    QA("QA", "974", 8, false, "3312 3456"),
    KW("KW", "965", 8, false, "5012 3456"),
    BH("BH", "973", 8, false, "3600 1234"),
    OM("OM", "968", 8, false, "9212 3456");

    val dialCode: String get() = "+$dial"

    /**
     * The number as it will be texted, for "We sent a code to …": the server's
     * E.164 with a space after a GCC dial code, so a number typed with its
     * country code but no plus does not show the code twice. Falls back to the
     * typed text when the server would refuse it anyway.
     */
    fun display(raw: String): String {
        val e164 = normalize(raw) ?: return raw.trim()
        val c = entries.firstOrNull { e164.startsWith("+" + it.dial) } ?: return e164
        return "${c.dialCode} ${e164.drop(1 + c.dial.length)}"
    }

    /**
     * What the server's `NormalizePhone` (services/api identity/phone.go) makes
     * of [raw] read in this country: E.164, or null where it would refuse it.
     */
    fun normalize(raw: String): String? {
        val b = StringBuilder()
        for (ch in raw.trim()) {
            if (ch == '+' && b.isEmpty()) b.append(ch) else if (ch in '0'..'9') b.append(ch)
        }
        var out = b.toString()
        when {
            out.startsWith("+") -> Unit
            out.startsWith("00") -> out = "+" + out.drop(2)
            out.startsWith(dial) && out.length == dial.length + nsn -> out = "+$out"
            else -> {
                val digits = if (trunk && out.startsWith("0")) out.drop(1) else out
                if (digits.length != nsn || digits.startsWith("0")) return null
                out = "+$dial$digits"
            }
        }
        val c = entries.firstOrNull { out.startsWith("+" + it.dial) }
        if (c != null) {
            var rest = out.drop(1 + c.dial.length)
            if (c.trunk && rest.length == c.nsn + 1 && rest.startsWith("0")) rest = rest.drop(1)
            if (rest.length != c.nsn || rest.startsWith("0")) return null
            out = "+${c.dial}$rest"
        }
        return if (E164.matches(out)) out else null
    }

    /**
     * Whether it is worth asking the server. Deliberately loose — the server
     * holds the length rule and answers 422 — but a national number shorter
     * than this country's numbers cannot be right, so the button waits.
     */
    fun plausible(raw: String): Boolean {
        val typed = raw.trim()
        val digits = typed.filter { it.isDigit() }
        if (typed.startsWith("+") || typed.startsWith("00")) return digits.length >= 8
        return digits.length >= nsn
    }

    companion object {
        private val E164 = Regex("""^\+[1-9]\d{7,14}$""")

        val Default: PhoneCountry = AE

        /** Picker order, for Swift, which cannot iterate a Kotlin enum directly. */
        val all: List<PhoneCountry> = entries.toList()
    }
}
