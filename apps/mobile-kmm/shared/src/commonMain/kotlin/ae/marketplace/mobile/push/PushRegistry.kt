package ae.marketplace.mobile.push

import ae.marketplace.mobile.identity.Session
import ae.marketplace.mobile.identity.isAuthenticated
import io.ktor.client.HttpClient
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Hands the phone's FCM/APNs token to the server, against the signed-in
 * session. The OS hands out a token whether or not anyone is signed in, so it
 * is held here and sent once there is a session to attach it to.
 */
class PushRegistry(
    private val client: HttpClient,
    private val session: Session,
    private val scope: CoroutineScope,
) {
    private val lock = Mutex()
    private var token: String? = null
    private var platform: String = ""
    private var sent: String? = null

    init {
        scope.launch {
            session.state.collect { state ->
                if (state.isAuthenticated) flush() else lock.withLock { sent = null }
            }
        }
    }

    fun update(token: String, platform: String) {
        scope.launch {
            lock.withLock {
                this@PushRegistry.token = token
                this@PushRegistry.platform = platform
            }
            flush()
        }
    }

    /** Ends the server session, which also detaches this phone's token. */
    suspend fun logout() {
        runCatching { client.post("auth/logout") }
    }

    private suspend fun flush() = lock.withLock {
        val t = token ?: return@withLock
        if (!session.state.value.isAuthenticated || sent == t) return@withLock
        runCatching {
            client.put("me/device") {
                contentType(ContentType.Application.Json)
                setBody(DeviceBody(t, platform))
            }
        }.onSuccess { sent = t }
    }
}

@Serializable
private data class DeviceBody(
    @SerialName("push_token") val pushToken: String,
    val platform: String,
)
