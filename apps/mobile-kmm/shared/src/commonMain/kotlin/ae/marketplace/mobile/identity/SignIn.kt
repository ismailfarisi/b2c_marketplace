package ae.marketplace.mobile.identity

import ae.marketplace.mobile.network.TokenStore
import ae.marketplace.mobile.network.Tokens
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
    val checking: Boolean = true,
    val signedIn: Boolean = false,
    val phone: String = "",
    val code: String = "",
    /** Non-empty once a code has been sent — which is what switches the screen. */
    val challengeId: String = "",
    val busy: Boolean = false,
    val error: String? = null,
)

class SignInViewModel(private val api: SignInApi) : ViewModel() {

    private val _state = MutableStateFlow(SignInUiState())
    val state: StateFlow<SignInUiState> = _state.asStateFlow()

    init {
        viewModelScope.launch {
            val already = runCatching { api.signedIn() }.getOrDefault(false)
            _state.value = _state.value.copy(checking = false, signedIn = already)
        }
    }

    fun onPhone(value: String) {
        _state.value = _state.value.copy(phone = value, error = null)
    }

    fun onCode(value: String) {
        _state.value = _state.value.copy(code = value, error = null)
    }

    fun sendCode() {
        val phone = _state.value.phone.trim()
        if (phone.isEmpty() || _state.value.busy) return
        _state.value = _state.value.copy(busy = true, error = null)
        viewModelScope.launch {
            runCatching { api.requestCode(phone) }
                .onSuccess { _state.value = _state.value.copy(busy = false, challengeId = it) }
                .onFailure { e -> _state.value = _state.value.copy(busy = false, error = e.message) }
        }
    }

    fun verify() {
        val s = _state.value
        if (s.challengeId.isEmpty() || s.code.isBlank() || s.busy) return
        _state.value = s.copy(busy = true, error = null)
        viewModelScope.launch {
            runCatching { api.verify(s.challengeId, s.code.trim()) }
                .onSuccess { _state.value = _state.value.copy(busy = false, signedIn = true) }
                .onFailure { e ->
                    // The code stays on screen. Clearing it on a wrong digit
                    // makes the seller retype all six, and the most common cause
                    // of a failure here is a typo in one of them.
                    _state.value = _state.value.copy(busy = false, error = e.message)
                }
        }
    }

    /** Back to the phone step, for a number typed wrong. */
    fun changeNumber() {
        _state.value = _state.value.copy(challengeId = "", code = "", error = null)
    }
}
