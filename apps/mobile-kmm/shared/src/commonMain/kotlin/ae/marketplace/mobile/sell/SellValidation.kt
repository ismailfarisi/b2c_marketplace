package ae.marketplace.mobile.sell

import ae.marketplace.mobile.model.SellField
import ae.marketplace.mobile.model.SellForm

/**
 * What is wrong with one answer.
 *
 * A code, not a sentence. The server sends the field's label and its bounds; the
 * *wording* of "that is too low" is UI copy and belongs in each platform's
 * string table, where a translator can reach it. Returning "must be at least
 * 1000" from shared code would be an English sentence no Arabic build could
 * fix — the same argument TimeSignal makes about never sending a rendered
 * "02:04 left".
 */
enum class FieldProblem {
    Required,
    NotANumber,
    TooSmall,
    TooLarge,
    /** A select or lookup answer that is not one of the offered values. */
    NotAnOption,
    /** The server refused the answer for a reason the form's rules do not express. */
    Invalid;

    /** The entry's Kotlin name, for Swift, which spells multi-word case names unpredictably. */
    val code: String get() = name
}

/**
 * [bound] carries the number the message needs — the min for [FieldProblem.TooSmall],
 * the max for [FieldProblem.TooLarge], null otherwise. In *minor* units for a
 * money field, exactly as the field declared it; show it with
 * [SellValidation.boundText].
 */
data class FieldIssue(
    val key: String,
    val problem: FieldProblem,
    val bound: Long? = null,
)

/**
 * Local validation, run before the round trip.
 *
 * It exists for latency and nothing else. The server re-checks every one of
 * these — a client-side bound is a courtesy, never a control — and the reason to
 * duplicate it is that a seller who has just filmed a car should find out that
 * the price box is empty in the same frame they tapped Next, not after a
 * request.
 *
 * The rules come entirely from the [SellForm] the server sent. There is no
 * knowledge here of what any field means: `required`, `min`, `max` and the
 * option list are all data, and adding a vertical adds no case to this function.
 */
object SellValidation {

    /**
     * The server's field refusals, as issues under the boxes they name.
     *
     * Keyed off `errors[].code`, never `message`: the message is an English
     * sentence and the wording belongs in each platform's string table. Keys the
     * form does not have are left out — they are not something the seller can
     * fix on this step, and the caller shows them as a form-level failure.
     */
    fun fromServer(form: SellForm, fieldCodes: Map<String, String>): List<FieldIssue> {
        val known = form.fields.associateBy { it.key } +
            listOfNotNull(form.trust?.reference).associateBy { it.key }
        return fieldCodes.mapNotNull { (key, code) ->
            val field = known[key] ?: return@mapNotNull null
            val problem = when (code) {
                "required" -> FieldProblem.Required
                "not_a_number" -> FieldProblem.NotANumber
                "invalid_option", "not_an_option" -> FieldProblem.NotAnOption
                "invalid" -> if (field.options.isNotEmpty()) FieldProblem.NotAnOption else FieldProblem.Invalid
                else -> FieldProblem.Invalid
            }
            FieldIssue(key, problem)
        }
    }

    fun check(form: SellForm, values: Map<String, String>): List<FieldIssue> {
        val issues = mutableListOf<FieldIssue>()
        for (field in form.fields) {
            issueFor(field, values[field.key].orEmpty())?.let(issues::add)
        }
        // The trust slot's identifier is validated exactly like a field,
        // because it is one. Checking it here rather than at the publish call
        // means a property advertiser is told the permit number is needed while
        // the box is still on screen, instead of getting a 422 after tapping
        // Publish on a form that looked complete.
        form.trust?.reference?.let { ref ->
            issueFor(ref, values[ref.key].orEmpty())?.let(issues::add)
        }
        return issues
    }

    /** True when every required answer is present and in range. */
    fun isComplete(form: SellForm, values: Map<String, String>): Boolean =
        check(form, values).isEmpty()

    private fun issueFor(field: SellField, raw: String): FieldIssue? {
        val value = raw.trim()

        if (value.isEmpty()) {
            // An optional empty field is not an error, and it must not be range
            // checked either — a blank optional number is absent, not zero.
            return if (field.required) FieldIssue(field.key, FieldProblem.Required) else null
        }

        when (field.kind) {
            "select" -> {
                // Guarded on the list being non-empty. A select that arrived with
                // no options is a malformed form, and rejecting the seller's
                // answer would blame them for it.
                if (field.options.isNotEmpty() && field.options.none { it.value == value }) {
                    return FieldIssue(field.key, FieldProblem.NotAnOption)
                }
            }

            "integer", "money" -> {
                // A money box holds what the seller typed — dirhams — while the
                // bounds are minor units, exactly as the wire carries them.
                // Comparing the two directly rejected a 2,500 AED car against a
                // 1,000 AED floor, so the answer is converted first.
                val number = (if (field.kind == "money") moneyToMinor(value) else parseNumber(value))
                    ?: return FieldIssue(field.key, FieldProblem.NotANumber)
                field.min?.let { if (number < it) return FieldIssue(field.key, FieldProblem.TooSmall, it) }
                field.max?.let { if (number > it) return FieldIssue(field.key, FieldProblem.TooLarge, it) }
            }

            // text, lookup, and anything this build does not recognise. A lookup
            // answer is an opaque token the server minted; the client has no way
            // to check it and must not invent one. An unknown kind is rendered as
            // text and validated as text, which is the degradation that keeps a
            // newer server working against an older app.
            else -> Unit
        }
        return null
    }

    /**
     * Parses what a keypad and a grouped display produce.
     *
     * Separators are stripped rather than rejected, matching the server: the box
     * renders "64,000" as the seller types, and telling them their own display
     * is invalid is a dead end they cannot get out of.
     */
    fun parseNumber(raw: String): Long? {
        val cleaned = buildString {
            for (c in raw.trim()) {
                when (c) {
                    ',', ' ', ' ' -> Unit
                    else -> append(c)
                }
            }
        }
        return cleaned.toLongOrNull()
    }

    /**
     * A typed money answer — major units, as on screen — in minor units.
     *
     * Up to two decimals are accepted ("1890.5" is 189,050 fils); more than
     * that is not a price and is refused rather than rounded. Grouping is
     * stripped like [parseNumber]. Null when it is not a number or overflows.
     */
    fun moneyToMinor(raw: String): Long? {
        val cleaned = buildString {
            for (c in raw.trim()) {
                when (c) {
                    ',', ' ', ' ' -> Unit
                    else -> append(c)
                }
            }
        }
        val parts = cleaned.split('.')
        if (parts.size > 2) return null
        val whole = parts[0]
        val fraction = parts.getOrNull(1) ?: ""
        if (fraction.length > 2 || fraction.any { !it.isDigit() }) return null
        if (whole.isEmpty() && fraction.isEmpty()) return null
        val major = if (whole.isEmpty() || whole == "-") 0L else whole.toLongOrNull() ?: return null
        if (major > Long.MAX_VALUE / 100 || major < Long.MIN_VALUE / 100) return null
        val fils = fraction.padEnd(2, '0').toLong()
        val negative = whole.startsWith("-")
        return major * 100 + if (negative) -fils else fils
    }

    /** Minor units as the seller reads them: "1,500", or "1,500.50" when there are fils. */
    fun minorToMajor(minor: Long): String {
        val negative = minor < 0
        val abs = if (negative) -minor else minor
        val whole = group(abs / 100)
        val fils = abs % 100
        val text = if (fils == 0L) whole else "$whole.${fils.toString().padStart(2, '0')}"
        return if (negative) "-$text" else text
    }

    /**
     * The number an issue's sentence needs, ready to show: a money bound in
     * major units, any other bound grouped. Empty when the issue has none.
     */
    fun boundText(field: SellField, issue: FieldIssue): String {
        val bound = issue.bound ?: return ""
        return if (field.kind == "money") minorToMajor(bound) else group(bound)
    }

    /**
     * The answers as the server expects them: every money field converted from
     * the major units the seller typed to minor units. Posting the typed value
     * verbatim stored 150,000 AED as 1,500 AED. Answers that do not parse are
     * passed through untouched so the server names the field.
     */
    fun toWire(form: SellForm, values: Map<String, String>): Map<String, String> {
        val money = form.fields.filter { it.kind == "money" }.map { it.key }.toSet()
        return values.mapValues { (key, value) ->
            if (key in money && value.isNotBlank()) moneyToMinor(value)?.toString() ?: value else value
        }
    }

    private fun group(n: Long): String {
        val s = if (n < 0) (-n).toString() else n.toString()
        val grouped = if (s.length <= 3) s else s.reversed().chunked(3).joinToString(",").reversed()
        return if (n < 0) "-$grouped" else grouped
    }
}
