package ae.marketplace.mobile.android.meals

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.FailureStrip
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.SecondaryButton
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.meals.MealsViewModel
import ae.marketplace.mobile.meals.SubscriptionUiState
import ae.marketplace.mobile.model.CancelReasons
import ae.marketplace.mobile.model.CheaperPlans
import ae.marketplace.mobile.model.GuestWeekOption
import ae.marketplace.mobile.model.MySubscription
import ae.marketplace.mobile.network.Failure
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

// Boredom-aware cancellation and the guest week. One question (why?), one
// honest save option that fits the answer, and the cancel button right there
// the whole time. No nested "are you sure" step.

@Composable
internal fun reasonLabel(code: String): String = stringResource(
    when (code) {
        CancelReasons.BORED -> R.string.meals_reason_bored
        CancelReasons.EXPENSIVE -> R.string.meals_reason_expensive
        CancelReasons.QUALITY -> R.string.meals_reason_quality
        CancelReasons.DELIVERY -> R.string.meals_reason_delivery
        CancelReasons.MOVING -> R.string.meals_reason_moving
        else -> R.string.meals_reason_other
    },
)

@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun CancelReasonsDialog(
    rotation: Boolean,
    busy: Boolean,
    arabic: Boolean,
    canPause: Boolean,
    canGuestWeek: Boolean,
    cheaper: CheaperPlans?,
    cheaperLoading: Boolean,
    cheaperError: Failure?,
    onLoadCheaper: () -> Unit,
    onPause: () -> Unit,
    onGuestWeek: () -> Unit,
    onRotationBuilder: () -> Unit,
    onChangeWeek: () -> Unit,
    onCancel: (code: String?, note: String) -> Unit,
    onDismiss: () -> Unit,
) {
    var code by remember { mutableStateOf<String?>(null) }
    var note by remember { mutableStateOf("") }
    LaunchedEffect(code) { if (code == CancelReasons.EXPENSIVE && cheaper == null) onLoadCheaper() }
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = M.Surface,
        title = {
            Text(
                stringResource(if (rotation) R.string.meals_cancel_rotation else R.string.meals_cancel),
                color = M.Ink, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold,
            )
        },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(Space.Small)) {
                Text(stringResource(R.string.meals_cancel_why), color = M.Muted, fontSize = Type.Secondary)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    CancelReasons.all.forEach { c ->
                        val on = code == c
                        Text(
                            reasonLabel(c),
                            color = if (on) M.Paper else M.Ink,
                            fontSize = Type.Secondary,
                            modifier = Modifier
                                .background(if (on) M.Ink else M.Surface, RoundedCornerShape(50))
                                .border(1.dp, if (on) M.Ink else M.Line, RoundedCornerShape(50))
                                .clickable(enabled = !busy) { code = c }
                                .semantics { role = Role.RadioButton; selected = on }
                                .padding(horizontal = 12.dp, vertical = 7.dp),
                        )
                    }
                }
                code?.let { c ->
                    Text(stringResource(R.string.meals_cancel_note), color = M.Muted, fontSize = Type.Secondary)
                    BasicTextField(
                        value = note,
                        onValueChange = { if (it.length <= 500) note = it },
                        textStyle = TextStyle(color = M.Ink, fontSize = Type.Body),
                        cursorBrush = SolidColor(M.Ink),
                        modifier = Modifier
                            .fillMaxWidth()
                            .defaultMinSize(minHeight = 44.dp)
                            .background(M.Surface, RoundedCornerShape(10.dp))
                            .border(1.dp, M.Line, RoundedCornerShape(10.dp))
                            .padding(horizontal = Space.Medium, vertical = 10.dp),
                    )
                    SaveOption(
                        code = c, rotation = rotation, arabic = arabic, canPause = canPause, canGuestWeek = canGuestWeek,
                        cheaper = cheaper, cheaperLoading = cheaperLoading, cheaperError = cheaperError,
                        onLoadCheaper = onLoadCheaper, onPause = onPause, onGuestWeek = onGuestWeek,
                        onRotationBuilder = onRotationBuilder, onChangeWeek = onChangeWeek,
                    )
                }
            }
        },
        confirmButton = {
            // The reason is optional: nobody is kept subscribed for not saying why.
            TextButton(onClick = { onCancel(code, note) }, enabled = !busy) {
                Text(
                    stringResource(if (rotation) R.string.meals_cancel_rotation else R.string.meals_cancel),
                    color = if (!busy) Palette.Bad else M.Faint,
                )
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.meals_keep), color = M.Ink) }
        },
    )
}

@Composable
private fun SaveOption(
    code: String,
    rotation: Boolean,
    arabic: Boolean,
    canPause: Boolean,
    canGuestWeek: Boolean,
    cheaper: CheaperPlans?,
    cheaperLoading: Boolean,
    cheaperError: Failure?,
    onLoadCheaper: () -> Unit,
    onPause: () -> Unit,
    onGuestWeek: () -> Unit,
    onRotationBuilder: () -> Unit,
    onChangeWeek: () -> Unit,
) {
    val pause: @Composable () -> Unit = {
        if (canPause) {
            Text(stringResource(R.string.meals_save_pause), color = M.Ink, fontSize = Type.Secondary)
            SecondaryButton(stringResource(R.string.meals_save_pause_btn), onPause)
        }
    }
    val hasBody = code == CancelReasons.BORED || code == CancelReasons.EXPENSIVE || canPause
    if (!hasBody) return
    Column(
        Modifier
            .fillMaxWidth()
            .border(1.dp, Palette.Accent, RoundedCornerShape(12.dp))
            .padding(Space.Medium),
        verticalArrangement = Arrangement.spacedBy(Space.Small),
    ) {
        Text(stringResource(R.string.meals_before_you_go), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
        when (code) {
            CancelReasons.BORED -> if (rotation) {
                Text(stringResource(R.string.meals_save_bored_rotation), color = M.Ink, fontSize = Type.Secondary)
                SecondaryButton(stringResource(R.string.meals_save_change_week), onChangeWeek)
            } else {
                Text(stringResource(R.string.meals_save_bored_single), color = M.Ink, fontSize = Type.Secondary)
                if (canGuestWeek) SecondaryButton(stringResource(R.string.meals_save_guest_week), onGuestWeek)
                SecondaryButton(stringResource(R.string.meals_save_rotation), onRotationBuilder)
            }
            CancelReasons.EXPENSIVE -> {
                when {
                    cheaperLoading -> LoadingBlock(inline = true)
                    cheaperError != null -> FailureStrip(cheaperError, onRetry = onLoadCheaper)
                    cheaper == null || cheaper.items.isEmpty() ->
                        Text(stringResource(R.string.meals_save_expensive_none), color = M.Ink, fontSize = Type.Secondary)
                    else -> {
                        Text(stringResource(R.string.meals_save_expensive), color = M.Ink, fontSize = Type.Secondary)
                        cheaper.items.forEach { p ->
                            Text(
                                pick(p.kitchenName, p.kitchenNameAr, arabic) + " · " +
                                    stringResource(R.string.meals_per_meal, money(p.perMealMinor, p.currency)),
                                color = M.Ink, fontSize = Type.Secondary,
                            )
                        }
                    }
                }
                pause()
            }
            else -> pause()
        }
    }
}

/** The guest week in My meals: offer, the week itself with both kitchens named, then "make it a rotation". */
@Composable
internal fun GuestWeekCard(sub: MySubscription, s: SubscriptionUiState, arabic: Boolean, vm: MealsViewModel, onOpen: (String) -> Unit, onRotation: () -> Unit) {
    sub.guestOf?.let { g ->
        Card {
            Text(stringResource(R.string.meals_guest_week), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
            Text(stringResource(R.string.meals_guest_of, pick(g.kitchenName, g.kitchenNameAr, arabic)), color = M.Ink, fontSize = Type.Secondary)
            SecondaryButton(stringResource(R.string.meals_guest_back), { onOpen(g.subscriptionId) })
        }
        return
    }
    sub.guestWeek?.let { g ->
        val range = shortDate(g.weekStart, arabic) + " – " + shortDate(g.weekEnd, arabic)
        Card {
            Text(stringResource(R.string.meals_guest_week), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
            Text(stringResource(R.string.meals_guest_lede, range, pick(g.kitchenName, g.kitchenNameAr, arabic)), color = M.Ink, fontSize = Type.Secondary)
            if (g.status == "pending_payment") Text(stringResource(R.string.meals_guest_pending), color = M.Muted, fontSize = Type.Secondary)
            SecondaryButton(
                stringResource(if (g.status == "pending_payment") R.string.meals_pay_due else R.string.meals_guest_see),
                { onOpen(g.guestSubscriptionId) },
            )
            if (g.completed) {
                Text(stringResource(R.string.meals_make_rotation), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(top = Space.Small))
                Text(stringResource(R.string.meals_make_rotation_lede), color = M.Muted, fontSize = Type.Secondary)
                PrimaryButton(stringResource(R.string.meals_make_rotation_btn), onRotation)
            }
        }
        return
    }
    val offer = sub.guestWeekOffer ?: return
    val from = offer.weekStart ?: return
    val to = offer.weekEnd ?: return
    s.guestStarted?.let { started ->
        val k = offer.options.firstOrNull { it.weekMinor == started.walletMinor + started.dueMinor }
        Card {
            Text(
                stringResource(R.string.meals_guest_done, k?.let { pick(it.kitchenName, it.kitchenNameAr, arabic) } ?: ""),
                color = Palette.Good, fontSize = Type.Body,
            )
        }
        return
    }
    if (!offer.eligible) return
    var planId by remember(sub.id) { mutableStateOf<String?>(null) }
    Card {
        Text(stringResource(R.string.meals_guest_offer), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
        Text(
            stringResource(R.string.meals_guest_offer_lede, shortDate(from, arabic) + " – " + shortDate(to, arabic), money(offer.creditMinor, offer.currency)),
            color = M.Ink, fontSize = Type.Secondary,
        )
        Text(stringResource(R.string.meals_guest_same_days), color = M.Muted, fontSize = Type.Secondary)
        offer.options.forEach { o ->
            GuestOptionRow(o, arabic, selected = planId == o.planId, enabled = !s.busy) { planId = o.planId }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
            PrimaryButton(
                stringResource(R.string.meals_guest_start),
                { planId?.let(vm::startGuestWeek) },
                enabled = !s.busy && planId != null,
                modifier = Modifier.weight(1f),
            )
            SecondaryButton(stringResource(R.string.meals_guest_not_now), { if (!s.busy) vm.dismissGuestWeek() })
        }
    }
}

@Composable
private fun GuestOptionRow(o: GuestWeekOption, arabic: Boolean, selected: Boolean, enabled: Boolean, onSelect: () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .background(M.Surface, RoundedCornerShape(10.dp))
            .border(1.dp, if (selected) Palette.Accent else M.Line, RoundedCornerShape(10.dp))
            .clickable(enabled = enabled, onClick = onSelect)
            .semantics { role = Role.RadioButton; this.selected = selected }
            .padding(horizontal = Space.Medium, vertical = 10.dp),
    ) {
        Text(pick(o.kitchenName, o.kitchenNameAr, arabic), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
        if (o.cuisines.isNotEmpty()) Text(o.cuisines.joinToString(" · "), color = M.Muted, fontSize = Type.Secondary)
        Text(
            stringResource(R.string.meals_per_meal, money(o.perMealMinor, o.currency)) + " · " +
                stringResource(R.string.meals_guest_week_price, money(o.weekMinor, o.currency)),
            color = M.Muted, fontSize = Type.Secondary,
        )
        Text(
            when {
                o.dueMinor > 0 -> stringResource(R.string.meals_guest_due, money(o.dueMinor, o.currency))
                o.leftoverCreditMinor > 0 -> stringResource(R.string.meals_guest_leftover, money(o.leftoverCreditMinor, o.currency))
                else -> stringResource(R.string.meals_guest_covered)
            },
            color = Palette.Good, fontSize = Type.Secondary,
        )
    }
}
