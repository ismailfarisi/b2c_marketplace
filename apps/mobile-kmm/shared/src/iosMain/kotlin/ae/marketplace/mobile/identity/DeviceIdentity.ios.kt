package ae.marketplace.mobile.identity

import ae.marketplace.mobile.PlatformContext
import platform.Foundation.NSUUID
import platform.Foundation.NSUserDefaults

/**
 * A random UUID in NSUserDefaults.
 *
 * Deliberately *not* `identifierForVendor`, which is what most apps reach for.
 * That value is stable per vendor across apps and resets when the last of their
 * apps is deleted — which makes it both more identifying than this needs and
 * less stable than it looks. A random UUID we generate ourselves is exactly as
 * useful for keying an interest profile and carries no cross-app correlation.
 *
 * NSUserDefaults rather than the Keychain, and the difference from Android is
 * real: a Keychain entry survives app deletion, so a reinstall would silently
 * resume someone's previous interest profile. NSUserDefaults is cleared with the
 * app, which is the behaviour a user deleting an app expects.
 */
internal class IosDeviceIdProvider : DeviceIdProvider {

    override suspend fun deviceId(): String {
        val defaults = NSUserDefaults.standardUserDefaults
        defaults.stringForKey(KEY)?.let { return it }

        val fresh = NSUUID().UUIDString()
        defaults.setObject(fresh, KEY)
        return fresh
    }

    private companion object {
        const val KEY = "ae.marketplace.device_id"
    }
}

actual fun deviceIdProvider(context: PlatformContext): DeviceIdProvider = IosDeviceIdProvider()
