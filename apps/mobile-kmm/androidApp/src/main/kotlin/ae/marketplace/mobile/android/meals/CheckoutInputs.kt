package ae.marketplace.mobile.android.meals

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.SecondaryButton
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.model.LatLng
import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationManager
import android.os.CancellationSignal
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.core.location.LocationManagerCompat
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Start date as a picker that cannot go before the first day still open. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun StartDateField(
    date: String,
    earliest: String,
    arabic: Boolean,
    onPick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    var open by remember { mutableStateOf(false) }
    val min = utcMillis(earliest) ?: 0L
    Text(
        longDate(date, arabic) ?: stringResource(R.string.meals_start_pick),
        color = M.Ink,
        fontSize = Type.Body,
        modifier = modifier
            .fillMaxWidth()
            .background(M.Surface, RoundedCornerShape(10.dp))
            .border(1.dp, M.Line, RoundedCornerShape(10.dp))
            .semantics { role = Role.Button }
            .clickable { open = true }
            .padding(horizontal = Space.Medium, vertical = 14.dp),
    )
    if (!open) return
    val state = rememberDatePickerState(
        initialSelectedDateMillis = utcMillis(date)?.takeIf { it >= min } ?: min,
        selectableDates = object : SelectableDates {
            override fun isSelectableDate(utcTimeMillis: Long) = utcTimeMillis >= min
        },
    )
    DatePickerDialog(
        onDismissRequest = { open = false },
        confirmButton = {
            TextButton({
                state.selectedDateMillis?.let {
                    onPick(Instant.ofEpochMilli(it).atZone(ZoneOffset.UTC).toLocalDate().toString())
                }
                open = false
            }) { Text(stringResource(R.string.meals_date_done)) }
        },
        dismissButton = { TextButton({ open = false }) { Text(stringResource(R.string.meals_date_dismiss)) } },
    ) { DatePicker(state) }
}

private fun utcMillis(iso: String): Long? = runCatching {
    LocalDate.parse(iso).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
}.getOrNull()

private fun longDate(iso: String, arabic: Boolean): String? = runCatching {
    LocalDate.parse(iso).format(DateTimeFormatter.ofPattern("EEEE d MMMM yyyy", if (arabic) Locale("ar") else Locale.ENGLISH))
}.getOrNull()

internal fun startReason(code: String?): Int? = when (code) {
    "too_soon" -> R.string.meals_start_too_soon
    "in_the_past" -> R.string.meals_start_past
    else -> null
}

/**
 * Pins a new address at the device's current fix. The browse location is an
 * area centre, and service-area eligibility and driver routing both read the
 * saved point, so an address is not saved without an exact one.
 */
@Composable
internal fun PinRow(pin: LatLng?, onPin: (LatLng) -> Unit) {
    val context = LocalContext.current
    var problem by remember { mutableStateOf<Int?>(null) }
    var locating by remember { mutableStateOf(false) }
    fun locate() {
        locating = true
        problem = null
        currentFix(context) { fix ->
            locating = false
            if (fix != null) onPin(LatLng(fix.latitude, fix.longitude)) else problem = R.string.meals_pin_failed
        }
    }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { grants ->
        if (grants.values.any { it }) locate() else problem = R.string.meals_pin_denied
    }
    if (pin != null) {
        Text("✓ " + stringResource(R.string.meals_pin_set), color = M.Leaf, fontSize = Type.Secondary)
    } else {
        Text(stringResource(R.string.meals_pin_needed), color = M.Muted, fontSize = Type.Secondary)
        SecondaryButton(stringResource(R.string.meals_use_location), {
            if (locating) return@SecondaryButton
            if (granted(context)) locate()
            else ask.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION))
        })
    }
    problem?.let { Text(stringResource(it), color = Palette.Bad, fontSize = Type.Secondary) }
}

private fun granted(context: Context) = listOf(
    Manifest.permission.ACCESS_FINE_LOCATION,
    Manifest.permission.ACCESS_COARSE_LOCATION,
).any { ContextCompat.checkSelfPermission(context, it) == PackageManager.PERMISSION_GRANTED }

/** A fix from the last two minutes, else a fresh one (null after the platform's timeout). */
@SuppressLint("MissingPermission")
private fun currentFix(context: Context, done: (Location?) -> Unit) {
    val m = context.getSystemService(Context.LOCATION_SERVICE) as? LocationManager ?: return done(null)
    val providers = m.getProviders(true)
    val recent = providers.asSequence()
        .mapNotNull { runCatching { m.getLastKnownLocation(it) }.getOrNull() }
        .filter { System.currentTimeMillis() - it.time < 2 * 60_000 }
        .maxByOrNull { it.time }
    if (recent != null) return done(recent)
    val provider = listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER).firstOrNull { it in providers }
        ?: providers.firstOrNull() ?: return done(null)
    runCatching {
        LocationManagerCompat.getCurrentLocation(m, provider, CancellationSignal(), ContextCompat.getMainExecutor(context)) { done(it) }
    }.onFailure { done(null) }
}
