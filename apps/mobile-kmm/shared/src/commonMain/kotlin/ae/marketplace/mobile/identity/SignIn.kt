package ae.marketplace.mobile.identity

import ae.marketplace.mobile.network.ApiException
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.TokenStore
import ae.marketplace.mobile.network.Tokens
import ae.marketplace.mobile.network.forgetCachedBearer
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
class SignInApi(
    private val client: HttpClient,
    private val tokens: TokenStore,
    /** Sent with verify so "Signed-in devices" can name this session. */
    private val device: SignInDevice = signInDevice(),
) {

    /**
     * Asks for a code. [country] is how the server reads a national number; a
     * number typed with + carries its own and the country is ignored. A new
     * code retires every earlier one for this number on the server.
     */
    suspend fun requestCode(phone: String, country: PhoneCountry = PhoneCountry.Default): CodeSent {
        val res: OtpChallenge = client.post("auth/otp/request") {
            contentType(ContentType.Application.Json)
            setBody(OtpRequest(phone = phone, country = country.iso, purpose = "login"))
        }.body()
        return CodeSent(res.challengeId, res.resendAfterSeconds)
    }

    suspend fun verify(challengeId: String, code: String) {
        val res: TokenPair = client.post("auth/otp/verify") {
            contentType(ContentType.Application.Json)
            setBody(OtpVerify(challengeId = challengeId, code = code, device = device))
        }.body()
        tokens.save(Tokens(res.accessToken, res.refreshToken))
        // Ktor keeps the last bearer it loaded until told otherwise, so without
        // this the next request still carries the previous account's token.
        client.forgetCachedBearer()
    }

    suspend fun signedIn(): Boolean = tokens.load() != null

    suspend fun signOut() {
        tokens.clear()
        client.forgetCachedBearer()
    }
}

/** A code is on its way; [resendAfterSeconds] is the server's cooldown. */
data class CodeSent(val challengeId: String, val resendAfterSeconds: Int)

@Serializable
internal data class OtpRequest(val phone: String, val country: String, val purpose: String)

@Serializable
private data class OtpChallenge(
    @SerialName("challenge_id") val challengeId: String,
    @SerialName("resend_after_seconds") val resendAfterSeconds: Int = DEFAULT_RESEND_SECONDS,
)

private const val DEFAULT_RESEND_SECONDS = 60

@Serializable
private data class OtpVerify(
    @SerialName("challenge_id") val challengeId: String,
    val code: String,
    val device: SignInDevice,
)

/** The `device` block of `POST /auth/otp/verify`: platform is `android` or `ios`, name the model. */
@Serializable
data class SignInDevice(val platform: String, val name: String)

/** This phone, as the session row should describe it. */
expect fun signInDevice(): SignInDevice

@Serializable
private data class TokenPair(
    @SerialName("access_token") val accessToken: String,
    @SerialName("refresh_token") val refreshToken: String,
)

data class SignInUiState(
    val phone: String = "",
    /** The picker's country, which is how a national number is read. */
    val country: PhoneCountry = PhoneCountry.Default,
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
    /**
     * The number (country plus what was typed, normalised) [resendAtMillis]
     * applies to. The cooldown protects one number's codes, so after "Change
     * number" a different number can be sent at once.
     */
    val resendFor: String = "",
    /**
     * The server's `Retry-After` for a [SignInProblem.TooMany] or
     * [SignInProblem.ResendTooMany], so the copy can say how long rather than
     * "a minute" for a limit that lasts an hour. Null when it did not say.
     */
    val retryAfterSeconds: Int? = null,
) {
    /** The number as it will be texted, with its country code. */
    val sentTo: String get() = country.display(phone)

    /** The key [resendFor] is compared with. */
    internal val sendKey: String get() = country.normalize(phone) ?: "${country.iso}:${phone.trim()}"

    /** Whether Send should be enabled. */
    val canSend: Boolean get() = !busy && country.plausible(phone)

    /** [retryAfterSeconds] rounded up to whole minutes, for waits of a minute or more. */
    val retryAfterMinutes: Int? get() = retryAfterSeconds?.let { (it + 59) / 60 }
}

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

    /**
     * The digits are the wrong length for the picked country (server code
     * `wrong_length`) — usually the country is wrong, so the copy names it.
     */
    WrongLength,

    /**
     * A newer code was sent to this number, which retired this one (server
     * `otp_superseded`). Like [NeedsNewCode] the challenge is dead, but the
     * fix is to read the latest message rather than to ask again.
     */
    Superseded,

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

    /**
     * The account is suspended or blocked (verify answered 403). Retyping
     * cannot help, so the copy points to support instead.
     */
    AccountBlocked,

    /**
     * A resend failed while a code is already out. A failed request retires
     * nothing on the server — only a successful one does — so the earlier code
     * still works until it expires, and the screen says so rather than implying
     * the typed code is at fault.
     */
    ResendFailed,

    /** A resend was refused for asking too often; the earlier code still works. */
    ResendTooMany,

    /** No usable connection. */
    Offline,

    /** Anything else, including a 500. */
    Unknown;

    /** The entry's Kotlin name, for Swift, which spells multi-word case names unpredictably. */
    val code: String get() = name
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

    fun onCountry(country: PhoneCountry) {
        _state.value = _state.value.copy(country = country, error = null)
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
        if (nowMillis < s.resendAtMillis && s.sendKey == s.resendFor) return

        val resending = s.challengeId.isNotEmpty()
        _state.value = s.copy(busy = true, error = null, retryAfterSeconds = null)
        viewModelScope.launch {
            runCatching { api.requestCode(phone, s.country) }
                .onSuccess {
                    _state.value = _state.value.copy(
                        busy = false,
                        challengeId = it.challengeId,
                        code = "",
                        resendAtMillis = nowMillis + it.resendAfterSeconds * 1000L,
                        resendFor = s.sendKey,
                    )
                }
                .onFailure { e ->
                    val problem = classify(e)
                    // A failed resend keeps the challenge that is already out:
                    // the server only retires earlier codes when it sends a new
                    // one, so that code still works, and the copy says so.
                    _state.value = _state.value.copy(
                        busy = false,
                        retryAfterSeconds = e.retryAfter(),
                        error = if (!resending) problem
                        else if (problem == SignInProblem.TooMany) SignInProblem.ResendTooMany
                        else SignInProblem.ResendFailed,
                    )
                }
        }
    }

    fun verify() {
        val s = _state.value
        if (s.challengeId.isEmpty() || s.code.length < CODE_LENGTH || s.busy) return
        _state.value = s.copy(busy = true, error = null, retryAfterSeconds = null)
        viewModelScope.launch {
            runCatching { api.verify(s.challengeId, s.code.trim()) }
                .onSuccess {
                    // Cleared rather than kept, so a gate opened after a later
                    // sign-out starts on an empty phone step, not a spent code.
                    _state.value = SignInUiState()
                    session.granted()
                }
                .onFailure { e ->
                    val problem = classify(e)
                    val dead = problem == SignInProblem.NeedsNewCode || problem == SignInProblem.Superseded
                    _state.value = _state.value.copy(
                        busy = false,
                        error = problem,
                        retryAfterSeconds = e.retryAfter(),
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

    /** Clears the form when the gate is dismissed, so the next gate starts empty. */
    fun reset() {
        if (!_state.value.busy) _state.value = SignInUiState()
    }

    /** Back to the phone step, for a number typed wrong. */
    fun changeNumber() {
        _state.value = _state.value.copy(challengeId = "", code = "", error = null, retryAfterSeconds = null)
    }

    private fun Throwable.retryAfter(): Int? = (toFailure() as? Failure.RateLimited)?.retryAfterSeconds

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
        Failure.Unauthorized -> when ((e as? ApiException)?.code) {
            OTP_EXPIRED, OTP_ALREADY_USED, OTP_TOO_MANY, OTP_UNKNOWN -> SignInProblem.NeedsNewCode
            OTP_SUPERSEDED -> SignInProblem.Superseded
            else -> SignInProblem.WrongCode
        }
        Failure.Forbidden -> SignInProblem.AccountBlocked
        // Older servers answered the OTP rate limit with a bare 409; only a 422
        // validation error means the number itself is wrong.
        is Failure.Rejected -> when {
            f.code == OTP_RATE_LIMITED -> SignInProblem.TooMany
            f.fieldCodes["phone"] == PHONE_WRONG_LENGTH -> SignInProblem.WrongLength
            (e as? ApiException)?.status == 422 -> SignInProblem.BadPhone
            (e as? ApiException)?.status == 409 -> SignInProblem.TooMany
            else -> SignInProblem.Unknown
        }
        is Failure.Server, Failure.NotFound, Failure.Unknown -> SignInProblem.Unknown
    }

    private companion object {
        const val CODE_LENGTH = 6

        // Mirrors `identity.Code*` in services/api. Wire contract: these strings
        // are matched, not parsed, and the server's comment says they may be
        // added to but never reworded.
        const val OTP_EXPIRED = "otp_expired"
        const val OTP_ALREADY_USED = "otp_already_used"
        const val OTP_TOO_MANY = "otp_too_many_attempts"
        const val OTP_UNKNOWN = "otp_unknown_challenge"
        const val OTP_RATE_LIMITED = "otp_rate_limited"
        const val OTP_SUPERSEDED = "otp_superseded"

        // `identity.Phone*` field codes.
        const val PHONE_WRONG_LENGTH = "wrong_length"
    }
}
