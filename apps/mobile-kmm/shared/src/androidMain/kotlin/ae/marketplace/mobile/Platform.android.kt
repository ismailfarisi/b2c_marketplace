package ae.marketplace.mobile

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities

actual class PlatformContext(val androidContext: Context)

actual class NetworkStatus(private val context: Context) {

    // getSystemService(Class) directly rather than ContextCompat: it is API 23+
    // and minSdk here is 26, so the compat shim would be a dependency bought for
    // nothing.
    private val connectivity: ConnectivityManager?
        get() = context.getSystemService(ConnectivityManager::class.java)

    /**
     * True on cellular, on a hotspot, or when the user has asked for data saving.
     *
     * `NOT_METERED` is checked rather than "is it cellular", because the two are
     * not the same: a tethered laptop connection and a capped home router are
     * both metered over Wi-Fi, and unlimited 5G is not metered over cellular.
     * Guessing from the transport gets both cases wrong.
     */
    actual fun isMetered(): Boolean {
        val cm = connectivity ?: return true
        val caps = cm.getNetworkCapabilities(cm.activeNetwork) ?: return true
        return !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)
    }

    actual fun isConnected(): Boolean {
        val cm = connectivity ?: return false
        val caps = cm.getNetworkCapabilities(cm.activeNetwork) ?: return false
        return caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
    }
}

actual fun networkStatus(context: PlatformContext): NetworkStatus =
    NetworkStatus(context.androidContext)

actual val platformName: String = "android"
