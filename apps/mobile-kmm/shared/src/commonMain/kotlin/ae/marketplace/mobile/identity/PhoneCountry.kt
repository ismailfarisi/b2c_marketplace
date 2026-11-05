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
     * The number as it will be texted, for "We sent a code to …". Display only:
     * the server normalises for real and refuses what it cannot read.
     */
    fun display(raw: String): String {
        val typed = raw.trim()
        if (typed.startsWith("+")) return typed
        if (typed.startsWith("00")) return "+" + typed.drop(2)
        val digits = typed.filter { it.isDigit() }
        val national = if (trunk && digits.startsWith("0")) digits.drop(1) else digits
        return "$dialCode $national"
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
        val Default: PhoneCountry = AE

        /** Picker order, for Swift, which cannot iterate a Kotlin enum directly. */
        val all: List<PhoneCountry> = entries.toList()
    }
}
