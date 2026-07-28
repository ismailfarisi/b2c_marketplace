package ae.marketplace.mobile.identity

import ae.marketplace.mobile.PlatformContext
import android.content.Context
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.util.UUID

/**
 * A random UUID, generated once and kept in the app's own storage.
 *
 * Deliberately *not* `Settings.Secure.ANDROID_ID`, an advertising id, or
 * anything derived from hardware. This value keys an interest profile, which is
 * a personalisation concern, not an identity one — and the moment it survives an
 * uninstall or correlates across apps it becomes a tracking identifier with a
 * consent obligation attached.
 *
 * Plain SharedPreferences rather than the encrypted store: this is a random
 * number that identifies nobody, and paying keystore latency for it on every
 * cold start buys nothing.
 */
internal class AndroidDeviceIdProvider(private val context: Context) : DeviceIdProvider {

    override suspend fun deviceId(): String = withContext(Dispatchers.IO) {
        val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)
        prefs.getString(KEY, null) ?: UUID.randomUUID().toString().also {
            prefs.edit().putString(KEY, it).apply()
        }
    }

    private companion object {
        const val FILE = "ae.marketplace.device"
        const val KEY = "device_id"
    }
}

actual fun deviceIdProvider(context: PlatformContext): DeviceIdProvider =
    AndroidDeviceIdProvider(context.androidContext)
