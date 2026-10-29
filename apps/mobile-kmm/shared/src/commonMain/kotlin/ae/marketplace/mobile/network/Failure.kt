package ae.marketplace.mobile.network

import io.ktor.client.plugins.HttpRequestTimeoutException
import kotlinx.coroutines.CancellationException
import kotlinx.io.IOException

/**
 * What went wrong, in the small number of shapes a screen can actually respond
 * to.
 *
 * # Why this is not `String`
 *
 * Every view model in this module used to end its failure path with
 * `error = e.message`, and every screen drew that string. Three things are wrong
 * with it, and all three shipped:
 *
 * - **It is not translatable.** This app renders Arabic, and `e.message` is
 *   whatever Ktor, kotlinx.serialization or the platform socket layer produced,
 *   in English, if anything at all.
 * - **It is frequently not a sentence.** A dropped connection on Android
 *   surfaces as the class name of an SSL exception. A viewer in a lift reads
 *   `javax.net.ssl.SSLException` and concludes the app is broken rather than
 *   that they are in a lift.
 * - **It cannot be branched on.** "Retry" is the right button for a timeout and
 *   the wrong one for a 422; "Sign in" is right for a 401 and nonsense for a
 *   500. A string forces every screen to either offer the same affordance for
 *   all of them or re-parse the error itself.
 *
 * So the classification happens once, here, and screens map a [Failure] to
 * words and buttons in their own resource files.
 *
 * # Why the set is this small
 *
 * These are the distinctions that change what the viewer should *do*. Splitting
 * further — 502 apart from 503, DNS apart from TCP — produces cases no screen
 * treats differently, and a taxonomy nobody branches on is just a longer `when`
 * with the same two outcomes.
 */
sealed interface Failure {

    /**
     * Nothing reached the server: no route, DNS failure, connection refused, TLS
     * failure, or a socket that died mid-response.
     *
     * The one case where retrying immediately is usually pointless and the
     * viewer has something they can fix.
     */
    data object Offline : Failure

    /**
     * The request reached the server and the answer did not come back in time.
     *
     * Kept apart from [Offline] because the remedy differs: this one is worth
     * retrying on the spot, and it is also the shape a *successful but slow*
     * write takes — so a screen that retries a POST here must be idempotent or
     * carry an idempotency key. The feed's 10-second budget makes this common on
     * a congested cell rather than exotic.
     */
    data object Timeout : Failure

    /**
     * The server refused the credential, and refreshing it did not help.
     *
     * By the time a screen sees this, [Session] has already been told and the
     * store has already been cleared — so the correct response is to show the
     * signed-out state, never to retry.
     */
    data object Unauthorized : Failure

    /** Authenticated, but not allowed. Retrying will not change it. */
    data object Forbidden : Failure

    /** The thing being asked for is gone, or was never there. */
    data object NotFound : Failure

    /**
     * The request was well-formed and refused on its merits.
     *
     * [fields] carries per-field codes when the server gave them, so a form can
     * mark the offending box instead of showing a banner. [code] carries a
     * reason for the refusals that are not about a field at all.
     */
    data class Rejected(
        val code: String? = null,
        val fields: Map<String, String> = emptyMap(),
        val detail: String? = null,
        val fieldCodes: Map<String, String> = emptyMap(),
    ) : Failure

    /** Too fast, or too often. [retryAfterSeconds] when the server said. */
    data class RateLimited(val retryAfterSeconds: Int? = null) : Failure

    /**
     * The server broke.
     *
     * [requestId] is kept because it is the only thing that makes a support
     * conversation about a 500 tractable, and showing it in small grey text is
     * cheaper than asking someone to describe what they were doing.
     */
    data class Server(val status: Int, val requestId: String? = null) : Failure

    /** Anything else, including a response this build cannot parse. */
    data object Unknown : Failure
}

/**
 * Classifies a caught throwable.
 *
 * Rethrows [CancellationException] rather than classifying it, which is not a
 * detail. A cancelled coroutine is a *normal* event here — the search box
 * cancels the previous request on every keystroke, and the feed cancels a page
 * when the viewer swipes past it — and a `runCatching` that swallows it both
 * breaks structured concurrency and paints "Something went wrong" over a screen
 * that is working exactly as designed. This was the shape of a real bug in the
 * search box: typing quickly flashed an error under the field.
 */
fun Throwable.toFailure(): Failure {
    if (this is CancellationException) throw this

    return when (this) {
        is ApiException -> fromStatus()
        is HttpRequestTimeoutException -> Failure.Timeout
        is IOException -> Failure.Offline
        else -> {
            // Ktor wraps engine exceptions, and the platform socket types are
            // not common code, so the leaf is often an IOException two causes
            // down. Walking the chain is less brittle than naming the classes
            // each platform happens to throw.
            var cause = cause
            var depth = 0
            while (cause != null && depth < MAX_CAUSE_DEPTH) {
                when (cause) {
                    is HttpRequestTimeoutException -> return Failure.Timeout
                    is IOException -> return Failure.Offline
                    else -> Unit
                }
                cause = cause.cause
                depth++
            }
            Failure.Unknown
        }
    }
}

private const val MAX_CAUSE_DEPTH = 6

private fun ApiException.fromStatus(): Failure = when (status) {
    401 -> Failure.Unauthorized
    403 -> Failure.Forbidden
    404, 410 -> Failure.NotFound
    409, 422, 400 -> Failure.Rejected(
        code = code,
        fields = fieldErrors,
        detail = problem?.detail,
        fieldCodes = problem?.errors.orEmpty().associate { it.field to it.code },
    )
    429 -> Failure.RateLimited()
    in 500..599 -> Failure.Server(status, requestId)
    else -> Failure.Unknown
}

/**
 * Whether offering a Retry button makes sense.
 *
 * A button that cannot work is worse than no button: it teaches the viewer that
 * tapping things in this app does nothing.
 */
val Failure.isRetryable: Boolean
    get() = when (this) {
        Failure.Offline, Failure.Timeout, is Failure.Server, Failure.Unknown -> true
        is Failure.RateLimited -> true
        Failure.Unauthorized, Failure.Forbidden, Failure.NotFound, is Failure.Rejected -> false
    }
