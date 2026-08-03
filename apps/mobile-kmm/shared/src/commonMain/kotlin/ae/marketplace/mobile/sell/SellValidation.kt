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
}

/**
 * [bound] carries the number the message needs — the min for [FieldProblem.TooSmall],
 * the max for [FieldProblem.TooLarge], null otherwise. In *minor* units for a
 * money field, exactly as the field declared it.
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
                val number = parseNumber(value)
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
}
