package ae.marketplace.mobile.network

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * RFC 9457 problem details — one error shape for the whole API.
 *
 * Mirrors `#/components/schemas/Problem` in api/openapi.yaml. The Go side made
 * this the only error shape deliberately, and the client's job is to not undo
 * that by inventing a second one for transport failures: a 502 from a proxy with
 * no JSON body still becomes an [ApiException], it just has no [problem].
 */
@Serializable
data class Problem(
    val type: String = "about:blank",
    val title: String = "",
    val status: Int = 0,
    val detail: String? = null,
    val instance: String? = null,
    /**
     * A stable machine-readable reason, present only where the status alone is
     * not enough to act on. See `httpx.Problem.Code` for the rule about when the
     * server sets it.
     */
    val code: String? = null,
    @SerialName("request_id") val requestId: String? = null,
    val errors: List<FieldError> = emptyList(),
)

@Serializable
data class FieldError(
    val field: String,
    val code: String,
    val message: String? = null,
)

class ApiException(
    val status: Int,
    val problem: Problem?,
    val requestId: String?,
) : Exception(problem?.title ?: "Request failed with status $status") {

    /** The server's reason code, when it gave one. */
    val code: String? get() = problem?.code

    /** Field-level detail, keyed by field name, for form binding. */
    val fieldErrors: Map<String, String>
        get() = problem?.errors.orEmpty().associate { it.field to (it.message ?: it.code) }

    /**
     * A stale or unparseable feed cursor.
     *
     * Its own predicate because the client has to *do* something specific about
     * it: throw the cursor away and reload from the top. The API documents the
     * cursor format as having already changed once, between Phase 2 and Phase 4,
     * and a client that treats 422 as a generic error shows an error screen to a
     * user whose only problem is an old page token.
     */
    val isStaleCursor: Boolean
        get() = status == 422 && problem?.errors.orEmpty().any { it.field == "cursor" }
}
