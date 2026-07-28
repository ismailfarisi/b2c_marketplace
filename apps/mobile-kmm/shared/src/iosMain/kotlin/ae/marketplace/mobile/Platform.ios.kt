package ae.marketplace.mobile

import kotlinx.cinterop.ExperimentalForeignApi
import platform.Network.nw_path_get_status
import platform.Network.nw_path_is_expensive
import platform.Network.nw_path_monitor_create
import platform.Network.nw_path_monitor_set_queue
import platform.Network.nw_path_monitor_set_update_handler
import platform.Network.nw_path_monitor_start
import platform.Network.nw_path_status_satisfied
import platform.Network.nw_path_t
import platform.darwin.dispatch_get_main_queue
import kotlin.concurrent.AtomicReference

/**
 * iOS has no Context, and pretending otherwise would put an empty parameter
 * through every constructor for symmetry's sake.
 */
actual class PlatformContext

/**
 * Path monitoring, started once and read cheaply.
 *
 * `nw_path_is_expensive` is the iOS equivalent of Android's `NOT_METERED` and it
 * is what "expensive" means to the system: cellular, or a personal hotspot. The
 * monitor pushes updates rather than answering questions, so the last value is
 * cached and the callers — which are on the swipe path — read a field rather
 * than block.
 */
@OptIn(ExperimentalForeignApi::class)
actual class NetworkStatus {

    private val expensive = AtomicReference(true)
    private val satisfied = AtomicReference(false)

    init {
        val monitor = nw_path_monitor_create()
        nw_path_monitor_set_queue(monitor, dispatch_get_main_queue())
        nw_path_monitor_set_update_handler(monitor) { path: nw_path_t ->
            expensive.value = nw_path_is_expensive(path)
            satisfied.value = nw_path_get_status(path) == nw_path_status_satisfied
        }
        nw_path_monitor_start(monitor)
    }

    /**
     * Defaults to `true` until the first callback arrives.
     *
     * The safe direction for "we do not know yet" is the cheaper rendition. The
     * first reel of a cold start is the one most likely to be on cellular —
     * someone opening the app outside — and guessing unmetered there spends the
     * viewer's data to save a few hundred milliseconds of quality difference on
     * a phone screen.
     */
    actual fun isMetered(): Boolean = expensive.value

    actual fun isConnected(): Boolean = satisfied.value
}

actual fun networkStatus(context: PlatformContext): NetworkStatus = NetworkStatus()

actual val platformName: String = "ios"
