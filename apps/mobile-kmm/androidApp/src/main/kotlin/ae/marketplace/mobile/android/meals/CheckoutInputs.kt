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
