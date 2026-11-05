package ae.marketplace.mobile.identity

import android.os.Build

actual fun signInDevice(): SignInDevice {
    // Null in host unit tests, where android.jar is stubs.
    val maker = Build.MANUFACTURER.orEmpty().trim()
    val model = Build.MODEL.orEmpty().trim()
    val name = when {
        model.isEmpty() -> "Android"
        maker.isEmpty() || model.startsWith(maker, ignoreCase = true) -> model
        else -> "${maker.replaceFirstChar { it.uppercase() }} $model"
    }
    return SignInDevice(platform = "android", name = name)
}
