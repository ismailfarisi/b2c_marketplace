package ae.marketplace.mobile.identity

import ae.marketplace.mobile.network.ApiException
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.TokenStore
import ae.marketplace.mobile.network.Tokens
import ae.marketplace.mobile.network.toFailure
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Phone and OTP, which is the only sign-in this product has.
 *
 * It arrives with the sell flow rather than at launch, and that ordering is the
 * product's: the feed is browsable signed out — `GET /feed` has `security: []` —
 * and a login wall on the front door of a discovery app converts a curious
 * visitor into a bounce. Selling is the first thing that genuinely cannot be
 * anonymous, because a listing belongs to someone and a buyer has to be able to
 * message them.
 */
class SignInApi(private val client: HttpClient, private val tokens: TokenStore) {

    suspend fun requestCode(phone: String): String {
        val res: OtpChallenge = client.post("auth/otp/request") {
            contentType(ContentType.Application.Json)
            setBody(OtpRequest(phone = phone, purpose = "login"))
        }.body()
        return res.challengeId
    }

    suspend fun verify(challengeId: String, code: String) {
        val res: TokenPair = client.post("auth/otp/verify") {
            contentType(ContentType.Application.Json)
            setBody(OtpVerify(challengeId = challengeId, code = code))
        }.body()
        tokens.save(Tokens(res.accessToken, res.refreshToken))
    }

    suspend fun signedIn(): Boolean = tokens.load() != null

    suspend fun signOut() = tokens.clear()
}

@Serializable
private data class OtpRequest(val phone: String, val purpose: String)

@Serializable
private data class OtpChallenge(@SerialName("challenge_id") val challengeId: String)

@Serializable
private data class OtpVerify(
    @SerialName("challenge_id") val challengeId: String,
    val code: String,
)

@Serializable
private data class TokenPair(
    @SerialName("access_token") val accessToken: String,
    @SerialName("refresh_token") val refreshToken: String,
)

data class SignInUiState(
    val phone: String = "",
    val code: String = "",
    /** Non-empty once a code has been sent — which is what switches the screen. */
    val challengeId: String = "",
    val busy: Boolean = false,
    val error: SignInProblem? = null,
    /**
     * When the viewer may ask for another code. Epoch millis, zero when they may
     * ask now.
     *
     * A resend button with no cooldown is how someone taps it four times on a
     * slow connection, burns their SMS budget, and then reads the fourth code
     * while the server only remembers the fourth challenge — which invalidates
     * the three codes already sitting in their messages.
     */
    val resendAtMillis: Long = 0,
)

/**
 * What went wrong, as a code rather than a sentence.
 *
 * The same reasoning as the sell flow's [ae.marketplace.mobile.sell.FieldProblem]:
 * a view model that produces English cannot be rendered in Arabic, and this app
 * ships both. `e.message` was going straight to the screen before this, which
 * meant a viewer on a bad connection read a Ktor exception class name.
 */
enum class SignInProblem {
    /** The number is not a shape the server will accept. */
    BadPhone,

    /** Six digits that were not the right six. Retype them. */
    WrongCode,

    /**
     * The challenge is dead — expired, already spent, or out of attempts.
     *
     * One case rather than three, even though the server distinguishes them,
     * because the viewer's next move is identical for all three: ask for a new
     * code. The server's three exist so the *log* can tell them apart and so
     * the detail sentence can be accurate; the screen only has one door.
     */
    NeedsNewCode,

    /** Asking for codes too quickly. */
    TooMany,

    /** No usable connection. */
    Offline,

    /** Anything else, including a 500. */
    Unknown,
}

class SignInViewModel(
    private val api: SignInApi,
    private val session: Session,
) : ViewModel() {

    private val _state = MutableStateFlow(SignInUiState())
    val state: StateFlow<SignInUiState> = _state.asStateFlow()

    /**
     * Whether the viewer is signed in is [Session]'s answer, not this class's.
     *
     * It used to be a field here, set from a disk read in `init`. That made the
     * sign-in screen the authority on a fact the whole shell depends on, and it
     * was only correct while this view model happened to exist — which is not
     * at launch, because the gate is deliberately not constructed until someone
     * needs an account.
     */
    val sessionState: StateFlow<SessionState> = session.state

    fun onPhone(value: String) {
        _state.value = _state.value.copy(phone = value, error = null)
    }

    fun onCode(value: String) {
        // Digits only, capped at six. A pasted "Your code is 483920" should
        // become 483920 rather than being refused, because copying the whole SMS
        // line is what people actually do.
        val digits = value.filter { it.isDigit() }.take(CODE_LENGTH)
        _state.value = _state.value.copy(code = digits, error = null)
    }

    fun sendCode(nowMillis: Long) {
        val s = _state.value
        val phone = s.phone.trim()
        if (phone.isEmpty() || s.busy) return
        if (nowMillis < s.resendAtMillis) return

        _state.value = s.copy(busy = true, error = null)
        viewModelScope.launch {
            runCatching { api.requestCode(phone) }
                .onSuccess {
                    _state.value = _state.value.copy(
                        busy = false,
                        challengeId = it,
                        code = "",
                        resendAtMillis = nowMillis + RESEND_COOLDOWN_MS,
                    )
                }
                .onFailure { e ->
                    _state.value = _state.value.copy(busy = false, error = classify(e))
                }
        }
    }

    fun verify() {
        val s = _state.value
        if (s.challengeId.isEmpty() || s.code.length < CODE_LENGTH || s.busy) return
        _state.value = s.copy(busy = true, error = null)
        viewModelScope.launch {
            runCatching { api.verify(s.challengeId, s.code.trim()) }
                .onSuccess {
                    _state.value = _state.value.copy(busy = false)
                    session.granted()
                }
                .onFailure { e ->
                    val problem = classify(e)
                    val dead = problem == SignInProblem.NeedsNewCode
                    _state.value = _state.value.copy(
                        busy = false,
                        error = problem,
                        // A dead challenge cannot be retried with another guess,
                        // so the screen goes back to asking for a code rather
                        // than leaving six digits under a message telling the
                        // viewer they are too late. This also re-enables the
                        // resend button, which is the only way forward.
                        challengeId = if (dead) "" else s.challengeId,
                        // The code stays on screen for a wrong guess. Clearing it
                        // makes the viewer retype all six, and the usual cause of
                        // a failure here is a typo in one of them.
                        code = if (dead) "" else s.code,
                        resendAtMillis = if (dead) 0 else s.resendAtMillis,
                    )
                }
        }
    }

    /** Back to the phone step, for a number typed wrong. */
    fun changeNumber() {
        _state.value = _state.value.copy(challengeId = "", code = "", error = null)
    }

    /**
     * Branches on the server's reason code, falling back to the status.
     *
     * The fallback matters more than it looks: an older server, or a proxy that
     * rewrote the body, produces a 401 with no code, and reading that as
     * "wrong code" is the right guess — it is by far the most common reason and
     * it leaves the viewer able to retype. Guessing [NeedsNewCode] instead would
     * throw away a challenge that was probably fine.
     */
    private fun classify(e: Throwable): SignInProblem = when (val f = e.toFailure()) {
        Failure.Offline, Failure.Timeout -> SignInProblem.Offline
        is Failure.RateLimited -> SignInProblem.TooMany
        Failure.Unauthorized, Failure.Forbidden -> when ((e as? ApiException)?.code) {
            OTP_EXPIRED, OTP_ALREADY_USED, OTP_TOO_MANY, OTP_UNKNOWN -> SignInProblem.NeedsNewCode
            else -> SignInProblem.WrongCode
        }
        is Failure.Rejected -> SignInProblem.BadPhone
        is Failure.Server, Failure.NotFound, Failure.Unknown -> SignInProblem.Unknown
        else -> SignInProblem.Unknown
    }

    private companion object {
        const val CODE_LENGTH = 6
        const val RESEND_COOLDOWN_MS = 30_000L

        // Mirrors `identity.Code*` in services/api. Wire contract: these strings
        // are matched, not parsed, and the server's comment says they may be
        // added to but never reworded.
        const val OTP_EXPIRED = "otp_expired"
        const val OTP_ALREADY_USED = "otp_already_used"
        const val OTP_TOO_MANY = "otp_too_many_attempts"
        const val OTP_UNKNOWN = "otp_unknown_challenge"
    }
}
