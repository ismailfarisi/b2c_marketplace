package ae.marketplace.mobile.identity

import ae.marketplace.mobile.PlatformContext

/**
 * Who the API thinks is asking.
 *
 * The important field is [deviceId], and the reason is Phase 4: interest
 * profiles are keyed on `viewer_key`, which is `d:<device>` for an anonymous
 * viewer and `u:<uuid>` once they sign in. A device id that changes on reinstall
 * is fine; one that changes on every launch means personalisation never
 * accumulates and the feed stays cold forever for people who never sign up —
 * which, in a product whose front door is a feed, is most people.
 */
data class ViewerIdentity(
    val deviceId: String,
    val userId: String? = null,
    val locale: String = "en",
) {
    /**
     * The same string the server derives.
     *
     * Not sent — the server computes it from the token or the header — but
     * computed here so client-side caches key on exactly what the server
     * personalises on. A cache keyed on `deviceId` alone would serve a signed-in
     * viewer their anonymous feed.
     */
    val viewerKey: String
        get() = userId?.let { "u:$it" } ?: "d:$deviceId"
}

/**
 * A stable, app-scoped random id.
 *
 * Deliberately *not* an advertising id, a hardware serial or Android's
 * `ANDROID_ID`: this identifies a feed session, not a person, and the moment it
 * is something that survives an uninstall or correlates across apps it becomes a
 * tracking identifier with a consent story attached. A random UUID in the app's
 * own secure storage is enough for the job and nothing more.
 */
expect fun deviceIdProvider(context: PlatformContext): DeviceIdProvider

interface DeviceIdProvider {
    /** Reads the stored id, generating and persisting one on first call. */
    suspend fun deviceId(): String
}
