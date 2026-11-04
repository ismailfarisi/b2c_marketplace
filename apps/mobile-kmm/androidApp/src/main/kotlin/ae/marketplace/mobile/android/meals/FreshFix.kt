package ae.marketplace.mobile.android.meals

import ae.marketplace.mobile.model.LatLng
import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Build
import android.os.CancellationSignal
import android.os.Handler
import android.os.Looper
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat

/**
 * A fresh location fix for pinning a delivery address, never a cached one or
 * the feed's area: the point is where the driver goes. Returns a launcher;
 * [onFix] gets null when permission is refused, location is off, or no fix
 * arrives within 30 seconds.
 */
@Composable
internal fun rememberFreshFix(onFix: (LatLng?) -> Unit): () -> Unit {
    val context = LocalContext.current
    val latest by rememberUpdatedState(onFix)
    val permission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) { grants ->
        if (grants.values.any { it }) freshFix(context) { latest(it) } else latest(null)
    }
    return {
        if (hasLocation(context)) freshFix(context) { latest(it) }
        else permission.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION))
    }
}

private fun hasLocation(context: Context) = listOf(
    Manifest.permission.ACCESS_FINE_LOCATION,
    Manifest.permission.ACCESS_COARSE_LOCATION,
).any { ContextCompat.checkSelfPermission(context, it) == PackageManager.PERMISSION_GRANTED }

@SuppressLint("MissingPermission")
private fun freshFix(context: Context, done: (LatLng?) -> Unit) {
    val manager = context.getSystemService(LocationManager::class.java)
    val provider = listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER)
        .firstOrNull { runCatching { manager?.isProviderEnabled(it) == true }.getOrDefault(false) }
    if (manager == null || provider == null) return done(null)

    val main = Handler(Looper.getMainLooper())
    var finished = false
    fun finish(loc: Location?) {
        if (finished) return
        finished = true
        done(loc?.let { LatLng(it.latitude, it.longitude) })
    }
    if (Build.VERSION.SDK_INT >= 30) {
        val cancel = CancellationSignal()
        main.postDelayed({ cancel.cancel(); finish(null) }, TIMEOUT_MS)
        runCatching { manager.getCurrentLocation(provider, cancel, context.mainExecutor) { finish(it) } }
            .onFailure { finish(null) }
    } else {
        val listener = object : LocationListener {
            override fun onLocationChanged(location: Location) = finish(location)
        }
        main.postDelayed({ manager.removeUpdates(listener); finish(null) }, TIMEOUT_MS)
        @Suppress("DEPRECATION")
        runCatching { manager.requestSingleUpdate(provider, listener, Looper.getMainLooper()) }
            .onFailure { finish(null) }
    }
}

private const val TIMEOUT_MS = 30_000L
