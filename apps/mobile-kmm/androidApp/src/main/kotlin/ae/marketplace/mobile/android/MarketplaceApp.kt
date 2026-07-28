package ae.marketplace.mobile.android

import ae.marketplace.mobile.MarketplaceSdk
import ae.marketplace.mobile.PlatformContext
import android.app.Application
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import java.util.UUID

/**
 * Builds the SDK once, off the main thread, and hands it out as a Deferred.
 *
 * `MarketplaceSdk.create` is suspending because the token store and the device
 * id both touch disk, and doing that during `Application.onCreate` on the main
 * thread is the classic way to add 60 ms to a cold start — which for a video
 * feed is a visible delay before the first frame.
 *
 * A `CompletableDeferred` rather than a nullable field so callers await it
 * instead of null-checking it: an activity that starts before initialisation
 * finishes is a normal race, not an error state.
 */
class MarketplaceApp : Application() {

    val sdk = CompletableDeferred<MarketplaceSdk>()

    /**
     * One session id per process launch.
     *
     * The server dedupes views per (user, reel, session), so a session that
     * changed on every screen would let one viewer count as several — and one
     * that persisted across launches would stop a genuine return visit counting
     * at all.
     */
    private val sessionId = UUID.randomUUID().toString()

    override fun onCreate() {
        super.onCreate()
        CoroutineScope(SupervisorJob() + Dispatchers.IO).launch {
            sdk.complete(
                MarketplaceSdk.create(
                    context = PlatformContext(applicationContext),
                    baseUrl = BuildConfig.API_BASE_URL,
                    locale = resources.configuration.locales[0].language,
                    sessionId = sessionId,
                )
            )
        }
    }
}
