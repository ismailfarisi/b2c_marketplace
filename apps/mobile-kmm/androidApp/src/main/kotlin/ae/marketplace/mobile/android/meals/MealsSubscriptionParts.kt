package ae.marketplace.mobile.android.meals

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.FailureStrip
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.meals.MealsViewModel
import ae.marketplace.mobile.meals.ReviewUiState
import ae.marketplace.mobile.model.MyDelivery
import ae.marketplace.mobile.model.MySubscription
import ae.marketplace.mobile.model.UpcomingMeal
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset

private fun isoToMillis(iso: String): Long? =
    runCatching { LocalDate.parse(iso).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli() }.getOrNull()

private fun millisToIso(ms: Long): String = Instant.ofEpochMilli(ms).atZone(ZoneOffset.UTC).toLocalDate().toString()

/**
 * Pause dates from a date picker that only offers days still before their
 * cutoff, so a typo or a locked day never reaches the API.
 */
@Composable
internal fun PauseCard(sub: MySubscription, busy: Boolean, vm: MealsViewModel) {
    val left = (sub.pauseAllowanceDays - sub.pauseDaysUsed).coerceAtLeast(0)
    val first = sub.upcoming.firstOrNull { !it.locked }?.date ?: return
    var from by remember(first) { mutableStateOf(first) }
    var to by remember(first) { mutableStateOf(first) }
    var picking by remember { mutableStateOf<String?>(null) }
    val valid = to >= from
    Card {
        Text(stringResource(R.string.meals_pause_title), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
        Text(stringResource(R.string.meals_pause_left, left, sub.pauseAllowanceDays), color = M.Muted, fontSize = Type.Secondary)
        Row(horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
            DateField(stringResource(R.string.meals_pause_from), from, Modifier.weight(1f)) { picking = "from" }
            DateField(stringResource(R.string.meals_pause_to), to, Modifier.weight(1f)) { picking = "to" }
        }
        if (!valid) Text(stringResource(R.string.meals_pause_bad_range), color = Palette.Bad, fontSize = Type.Secondary)
        PrimaryButton(stringResource(R.string.meals_pause), { vm.pause(from, to) }, enabled = !busy && left > 0 && valid)
    }
    picking?.let { which ->
        PauseDatePicker(
            initial = if (which == "from") from else to,
            earliest = if (which == "to") from else first,
            onPick = { day ->
                if (which == "from") {
                    from = day
                    if (to < day) to = day
                } else {
                    to = day
                }
                picking = null
            },
            onDismiss = { picking = null },
        )
    }
}

@Composable
private fun DateField(label: String, value: String, modifier: Modifier, onClick: () -> Unit) {
    val arabic = ae.marketplace.mobile.android.ui.rememberArabic()
    Column(
        modifier
            .background(M.Surface, RoundedCornerShape(10.dp))
            .border(1.dp, M.Line, RoundedCornerShape(10.dp))
            .clickable(onClick = onClick)
            .semantics { role = Role.Button }
            .padding(horizontal = Space.Medium, vertical = 10.dp),
    ) {
        Text(label, color = M.Faint, fontSize = Type.Caption)
        Text(shortDate(value, arabic), color = M.Ink, fontSize = Type.Body)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PauseDatePicker(initial: String, earliest: String, onPick: (String) -> Unit, onDismiss: () -> Unit) {
    val min = isoToMillis(earliest) ?: 0L
    val state = rememberDatePickerState(
        initialSelectedDateMillis = isoToMillis(initial),
        selectableDates = object : SelectableDates {
            override fun isSelectableDate(utcTimeMillis: Long) = utcTimeMillis >= min
            override fun isSelectableYear(year: Int) = year >= LocalDate.parse(earliest).year
        },
    )
    DatePickerDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(onClick = { state.selectedDateMillis?.let { onPick(millisToIso(it)) } ?: onDismiss() }) {
                Text(stringResource(R.string.meals_date_ok), color = M.Chilli)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.meals_date_cancel), color = M.Ink) }
        },
    ) { DatePicker(state = state, showModeToggle = false) }
}

/** The current and next pauses, and a way to end them early. */
@Composable
internal fun ResumeCard(sub: MySubscription, busy: Boolean, resumesOn: String?, arabic: Boolean, vm: MealsViewModel) {
    Card {
        Text(stringResource(R.string.meals_paused_dates), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
        sub.pauses.forEach { p ->
            Text(shortDate(p.fromDate, arabic) + " – " + shortDate(p.toDate, arabic), color = M.Muted, fontSize = Type.Secondary)
        }
        resumesOn?.let { Text(stringResource(R.string.meals_resumes_on, shortDate(it, arabic)), color = M.Leaf, fontSize = Type.Secondary) }
        PrimaryButton(stringResource(R.string.meals_resume), vm::resume, enabled = !busy)
        Text(stringResource(R.string.meals_resume_note), color = M.Muted, fontSize = Type.Caption)
    }
}

@Composable
internal fun deliveryLabel(status: String): String = when (status) {
    "scheduled" -> stringResource(R.string.meals_delivery_scheduled)
    "dispatched" -> stringResource(R.string.meals_delivery_dispatched)
    "en_route" -> stringResource(R.string.meals_delivery_en_route)
    "arrived" -> stringResource(R.string.meals_delivery_arrived)
    "completed" -> stringResource(R.string.meals_delivery_completed)
    "failed" -> stringResource(R.string.meals_delivery_failed)
    else -> stringResource(R.string.meals_status_other)
}

@Composable
internal fun DeliveryPill(status: String) {
    val tint = when (status) {
        "completed" -> M.Leaf
        "failed" -> Palette.Bad
        "dispatched", "en_route", "arrived" -> M.Chilli
        else -> M.Muted
    }
    Text(
        deliveryLabel(status),
        color = tint,
        fontSize = Type.Caption,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.border(1.dp, tint.copy(alpha = 0.5f), RoundedCornerShape(50)).padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

@Composable
internal fun RecentDeliveriesCard(items: List<MyDelivery>, arabic: Boolean) {
    if (items.isEmpty()) return
    SectionTitle(stringResource(R.string.meals_recent))
    Card {
        items.forEach { d ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(shortDate(d.date, arabic) + " · " + slotLabel(d.mealSlot), color = M.Ink, fontSize = Type.Secondary, modifier = Modifier.weight(1f))
                DeliveryPill(d.status)
            }
        }
    }
}

/** A skipped meal stays listed, with a way to put it back before the cutoff. */
@Composable
internal fun SkippedMealCard(m: UpcomingMeal, arabic: Boolean, busy: Boolean, vm: MealsViewModel) {
    Card {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(shortDate(m.date, arabic) + " · " + slotLabel(m.mealSlot), color = M.Faint, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
            Text(stringResource(R.string.meals_skipped), color = M.Muted, fontSize = Type.Caption, fontWeight = FontWeight.SemiBold)
        }
        if (!m.locked) {
            Text(
                stringResource(R.string.meals_unskip),
                color = M.Chilli,
                fontSize = Type.Secondary,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.clickable(enabled = !busy) { vm.unskip(m.date, m.mealSlot) }.padding(vertical = Space.Small),
            )
        }
    }
}

@Composable
internal fun ConfirmSkip(onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = M.Surface,
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Space.Small)) {
                Text(stringResource(R.string.meals_skip_confirm), color = M.Ink, fontSize = Type.Body)
                Text(stringResource(R.string.meals_skip_note), color = M.Muted, fontSize = Type.Secondary)
            }
        },
        confirmButton = { TextButton(onClick = onConfirm) { Text(stringResource(R.string.meals_skip), color = M.Chilli) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.meals_keep), color = M.Ink) } },
    )
}

/** One review per subscription, once the first delivery has arrived. */
@Composable
internal fun RateKitchenCard(state: ReviewUiState, vm: MealsViewModel) {
    val data = state.data
    SectionTitle(stringResource(R.string.meals_rate_title))
    when {
        data == null && state.error != null -> FailureStrip(state.error!!, onRetry = { vm.loadReview() })
        data == null -> LoadingBlock()
        !data.canReview -> Card { Text(stringResource(R.string.meals_rate_after), color = M.Muted, fontSize = Type.Secondary) }
        else -> Card {
            var rating by remember(data.review?.id) { mutableIntStateOf(data.review?.rating ?: 0) }
            var comment by remember(data.review?.id) { mutableStateOf(data.review?.comment ?: "") }
            if (data.review?.status == "hidden") Text(stringResource(R.string.meals_rate_hidden), color = Palette.Bad, fontSize = Type.Secondary)
            Row(horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
                (1..5).forEach { n ->
                    val label = stringResource(R.string.meals_rate_star, n)
                    Text(
                        "★",
                        color = if (n <= rating) M.Saffron else M.Ghost,
                        fontSize = Type.Title,
                        modifier = Modifier
                            .semantics { role = Role.RadioButton; selected = rating == n; contentDescription = label }
                            .clickable(enabled = !state.saving) { rating = n }
                            .defaultMinSize(minWidth = Space.Touch, minHeight = Space.Touch)
                            .padding(Space.Small),
                    )
                }
            }
            BasicTextField(
                value = comment,
                onValueChange = { if (it.length <= 1000) comment = it },
                textStyle = TextStyle(color = M.Ink, fontSize = Type.Body),
                cursorBrush = SolidColor(M.Chilli),
                minLines = 3,
                modifier = Modifier
                    .fillMaxWidth()
                    .background(M.Surface, RoundedCornerShape(10.dp))
                    .border(1.dp, M.Line, RoundedCornerShape(10.dp))
                    .padding(horizontal = Space.Medium, vertical = 14.dp),
                decorationBox = { inner ->
                    if (comment.isEmpty()) Text(stringResource(R.string.meals_rate_comment), color = M.Faint, fontSize = Type.Body)
                    inner()
                },
            )
            state.error?.let { FailureStrip(it, onRetry = { vm.saveReview(rating, comment) }) }
            if (state.saved) Text(stringResource(R.string.meals_rate_saved), color = M.Leaf, fontSize = Type.Secondary)
            PrimaryButton(
                stringResource(if (data.review != null) R.string.meals_rate_update else R.string.meals_rate_save),
                { vm.saveReview(rating, comment) },
                enabled = rating > 0 && !state.saving,
            )
        }
    }
}

/** "Mon 14:00" in the reader's language and time zone, or null when unparseable. */
internal fun cutoffLabel(iso: String, arabic: Boolean): String? = runCatching {
    java.time.OffsetDateTime.parse(iso)
        .atZoneSameInstant(java.time.ZoneId.systemDefault())
        .format(java.time.format.DateTimeFormatter.ofPattern("EEE HH:mm", if (arabic) java.util.Locale("ar") else java.util.Locale.ENGLISH))
}.getOrNull()
