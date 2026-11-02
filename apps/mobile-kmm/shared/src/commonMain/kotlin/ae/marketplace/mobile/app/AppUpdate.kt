package ae.marketplace.mobile.app

import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.get
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

enum class UpdateNeed { UP_TO_DATE, RECOMMENDED, REQUIRED }

/** What the launch check found, and where to send the user. */
data class AppUpdate(val need: UpdateNeed, val storeUrl: String)

@Serializable
internal data class AppRelease(
    @SerialName("min_version") val minVersion: String = "",
    @SerialName("latest_version") val latestVersion: String = "",
    @SerialName("store_url") val storeUrl: String = "",
)

@Serializable
internal data class AppReleases(
    val android: AppRelease = AppRelease(),
    val ios: AppRelease = AppRelease(),
)

object AppVersions {
    /** Compares dotted versions numerically ("1.10" > "1.9"); missing parts count as 0. */
    fun compare(a: String, b: String): Int {
        val x = a.parts()
        val y = b.parts()
        for (i in 0 until maxOf(x.size, y.size)) {
            val c = (x.getOrNull(i) ?: 0).compareTo(y.getOrNull(i) ?: 0)
            if (c != 0) return c
        }
        return 0
    }

    fun need(current: String, min: String, latest: String): UpdateNeed = when {
        min.isNotBlank() && compare(current, min) < 0 -> UpdateNeed.REQUIRED
        latest.isNotBlank() && compare(current, latest) < 0 -> UpdateNeed.RECOMMENDED
        else -> UpdateNeed.UP_TO_DATE
    }

    private fun String.parts() =
        trim().substringBefore('-').split('.').map { p -> p.takeWhile { it.isDigit() }.toIntOrNull() ?: 0 }
}

internal class AppConfigApi(private val client: HttpClient) {
    /** Never throws: an unreachable or unconfigured server means up to date. */
    suspend fun check(platform: String, current: String): AppUpdate {
        val all = runCatching { client.get("app/config").body<AppReleases>() }.getOrNull()
            ?: return AppUpdate(UpdateNeed.UP_TO_DATE, "")
        val r = if (platform == "ios") all.ios else all.android
        return AppUpdate(AppVersions.need(current, r.minVersion, r.latestVersion), r.storeUrl)
    }
}
