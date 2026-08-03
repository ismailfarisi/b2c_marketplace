package ae.marketplace.mobile.identity

import ae.marketplace.mobile.network.TokenStore
import ae.marketplace.mobile.network.Tokens
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * Whether there is an account behind this session, as one observable fact.
 *
 * # Why this exists
 *
 * The shell used to keep a `rememberSaveable` boolean. That is wrong in three
 * ways at once and every one of them shipped:
 *
 * - It starts `false`, so a viewer with a perfectly good refresh token on disk
 *   was shown the sign-in gate again on every cold start. The token was there;
 *   nothing ever asked.
 * - Signing out flipped the boolean and never touched [TokenStore], so the
 *   credential stayed on the device. A shared phone would have handed the next
 *   person a live session on relaunch.
 * - Nothing could report a session ending from *underneath* the UI. When a
 *   refresh token is revoked the HTTP client clears the store and every
 *   subsequent request 401s, but the screen kept claiming to be signed in and
 *   kept showing the failures as network errors.
 *
 * # Why three states rather than a boolean
 *
 * [Unknown] is not pedantry. Reading the Keychain or EncryptedSharedPreferences
 * is asynchronous, and a boolean has to pick a side while that read is in
 * flight. Picking `false` flashes the sign-in gate at a signed-in returning
 * viewer for one frame; picking `true` flashes their empty Saved list. Neither
 * is recoverable by adding a spinner *inside* the gate, because the gate itself
 * is the wrong screen. So the shell waits, and this type makes waiting the
 * default rather than something each caller remembers to do.
 */
sealed interface SessionState {
    /** The token store has not been read yet. Draw neither the gate nor the account screens. */
    data object Unknown : SessionState

    /** No credential. A supported, working state — the feed and search are public. */
    data object Anonymous : SessionState

    data class Authenticated(val reason: Origin) : SessionState

    enum class Origin { Restored, SignedIn }
}

/**
 * Why a session ended, when the viewer did not end it.
 *
 * Kept because the two cases deserve different words on screen. Someone who
 * tapped Sign out knows why they are looking at a sign-in box; someone whose
 * token was revoked mid-scroll does not, and "Signed out" with no explanation
 * reads as the app losing their account.
 */
enum class SessionEnded { ByViewer, Expired }

class Session(private val tokens: TokenStore) {

    private val _state = MutableStateFlow<SessionState>(SessionState.Unknown)
    val state: StateFlow<SessionState> = _state.asStateFlow()

    private val _ended = MutableStateFlow<SessionEnded?>(null)

    /**
     * The last involuntary ending, for the shell to show once and clear.
     *
     * A flow rather than a callback because the shell may not be composed at
     * the moment a background refresh fails — a token can be rejected while the
     * app is in the recents list. A callback fired then goes nowhere; a value
     * sitting here is still true when the viewer comes back.
     */
    val ended: StateFlow<SessionEnded?> = _ended.asStateFlow()

    /**
     * Reads the store. Safe to call more than once; the first answer wins unless
     * the store has genuinely changed underneath.
     */
    suspend fun restore() {
        val existing = runCatching { tokens.load() }.getOrNull()
        _state.value =
            if (existing != null) SessionState.Authenticated(SessionState.Origin.Restored)
            else SessionState.Anonymous
    }

    /** After a verified OTP. The tokens are already saved by [SignInApi]. */
    fun granted() {
        _ended.value = null
        _state.value = SessionState.Authenticated(SessionState.Origin.SignedIn)
    }

    /**
     * The viewer asked to leave.
     *
     * Clears the store *before* announcing the state change, so nothing
     * observing [state] can fire a request that still finds a credential. The
     * other order produces a sign-out that loads the previous account's inbox
     * one last time.
     */
    suspend fun signOut() {
        runCatching { tokens.clear() }
        _ended.value = SessionEnded.ByViewer
        _state.value = SessionState.Anonymous
    }

    /**
     * The credential stopped working. Called from the HTTP client's refresh
     * failure path, which has already cleared the store.
     *
     * Idempotent, because a burst of parallel requests all fail together and
     * every one of them reports it. Announcing once matters: the shell shows a
     * message when this transitions, and five identical toasts is how a
     * revoked token looks like a crash.
     */
    fun expired() {
        if (_state.value is SessionState.Anonymous) return
        _ended.value = SessionEnded.Expired
        _state.value = SessionState.Anonymous
    }

    /** Acknowledges [ended] so it is not shown twice. */
    fun clearEnded() {
        _ended.value = null
    }

    /** Test seam. */
    internal suspend fun saveForTest(t: Tokens) = tokens.save(t)
}

/** Convenience for the common branch, which does not care why. */
val SessionState.isAuthenticated: Boolean
    get() = this is SessionState.Authenticated

val SessionState.isSettled: Boolean
    get() = this !is SessionState.Unknown
