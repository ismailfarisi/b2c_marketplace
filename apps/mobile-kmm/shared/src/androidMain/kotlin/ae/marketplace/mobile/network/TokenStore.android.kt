package ae.marketplace.mobile.network

import ae.marketplace.mobile.PlatformContext
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * Tokens in EncryptedSharedPreferences, behind a hardware-backed key.
 *
 * Plain SharedPreferences is a world-readable-if-rooted XML file and, more
 * practically, is included in an Android auto-backup by default — so a
 * long-lived refresh token sitting there is account takeover waiting for one
 * restored device backup.
 *
 * The reads are moved off the main thread deliberately. The first
 * `EncryptedSharedPreferences.create` on a cold start does keystore work that
 * takes tens of milliseconds, and doing it on the main thread during app launch
 * is a measurable stutter on exactly the frame budget a video feed cannot spare.
 */
internal class AndroidTokenStore(platform: PlatformContext) : TokenStore {

    private val context = platform.androidContext

    private val prefs: SharedPreferences by lazy {
        val key = MasterKey.Builder(context)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        EncryptedSharedPreferences.create(
            context,
            FILE,
            key,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }

    override suspend fun load(): Tokens? = withContext(Dispatchers.IO) {
        val access = prefs.getString(ACCESS, null) ?: return@withContext null
        val refresh = prefs.getString(REFRESH, null) ?: return@withContext null
        Tokens(access, refresh)
    }

    override suspend fun save(tokens: Tokens) = withContext(Dispatchers.IO) {
        prefs.edit().putString(ACCESS, tokens.access).putString(REFRESH, tokens.refresh).apply()
    }

    override suspend fun clear() = withContext(Dispatchers.IO) {
        prefs.edit().remove(ACCESS).remove(REFRESH).apply()
    }

    private companion object {
        const val FILE = "ae.marketplace.auth"
        const val ACCESS = "access_token"
        const val REFRESH = "refresh_token"
    }
}

actual fun tokenStore(context: PlatformContext): TokenStore = AndroidTokenStore(context)
