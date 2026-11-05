package ae.marketplace.mobile.android.sell

import androidx.annotation.StringRes
import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.identity.PhoneCountry
import ae.marketplace.mobile.identity.SignInProblem
import ae.marketplace.mobile.identity.SignInUiState
import ae.marketplace.mobile.identity.SignInViewModel
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.ArrowDropDown
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay

/**
 * The one screen between an account-shaped action and the account.
 *
 * It is here rather than at launch because this is the first thing in the
 * product that genuinely cannot be anonymous: a listing belongs to someone and a
 * buyer has to be able to message them. Everything before it — the feed, the
 * filters, search — works signed out, and asking a visitor for a phone number
 * before they have seen a reel is how a discovery app loses the visit.
 *
 * It says why it is asking. A sign-in wall with no explanation reads as a data
 * grab; "buyers message you here" is the actual reason and it is also the
 * seller's benefit.
 *
 * # What this screen owes the viewer
 *
 * An SMS login has more dead ends than any other screen in the product, and
 * each one used to look identical: a red line reading `ClientRequestException`.
 * Four are handled explicitly now — a wrong code, a dead challenge, a rate
 * limit, and no connection at all — because the correct next action differs for
 * every one and getting it wrong strands someone at the door.
 */
@Composable
fun SignInGate(
    viewModel: SignInViewModel,
    onCancel: () -> Unit,
    modifier: Modifier = Modifier,
    @StringRes title: Int = R.string.signin_title,
    @StringRes why: Int = R.string.signin_why,
    /** Cream and Chilli Red, for gates over the light meals tabs. */
    light: Boolean = true,
) {
    val ink = if (light) MealsInk else Color.White
    val accent = if (light) MealsChilli else Accent
    val state by viewModel.state.collectAsState()
    val keyboard = LocalSoftwareKeyboardController.current
    val focus = remember { FocusRequester() }

    // A local clock, ticking only while a cooldown is actually running.
    //
    // The view model stores the deadline as an absolute instant rather than a
    // countdown, so it stays correct across a rotation or a trip to the
    // background — a counter decremented by the UI resets to thirty every time
    // the composition is recreated, which turns the cooldown into a suggestion.
    var nowMillis by remember { mutableLongStateOf(0L) }
    LaunchedEffect(state.resendAtMillis) {
        nowMillis = currentMillis()
        while (nowMillis < state.resendAtMillis) {
            delay(500)
            nowMillis = currentMillis()
        }
    }
    val cooldown = ((state.resendAtMillis - nowMillis) / 1000).toInt().coerceAtLeast(0)

    val onCode = state.challengeId.isNotEmpty()

    // The keyboard follows the step. Landing on a code field and having to tap
    // it is a small thing that happens to every single person who signs in.
    LaunchedEffect(onCode) {
        focus.requestFocus()
        keyboard?.show()
    }

    Column(
        modifier
            .fillMaxSize()
            .background(if (light) MealsPaper else Ink)
            .statusBarsPadding()
            .imePadding()
            .padding(horizontal = 22.dp, vertical = 18.dp),
    ) {
        val close = stringResource(R.string.action_close)
        Box(
            Modifier
                .padding(bottom = 12.dp)
                .size(48.dp)
                .clickable(onClick = onCancel)
                .semantics { contentDescription = close },
            contentAlignment = Alignment.CenterStart,
        ) {
            Icon(Icons.Rounded.Close, contentDescription = null, tint = ink, modifier = Modifier.size(24.dp))
        }
        Text(
            text = stringResource(title),
            color = ink,
            fontSize = 21.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            text = if (onCode) stringResource(R.string.signin_sent_to, state.sentTo)
            else stringResource(why),
            color = if (light) MealsMuted else Color.White.copy(alpha = 0.55f),
            fontSize = 13.sp,
            modifier = Modifier.padding(top = 8.dp, bottom = 26.dp),
        )

        if (!onCode) {
            // Country code then number, left to right in Arabic too: the pair
            // reads as one number, and "+971" reversed is not one.
            androidx.compose.runtime.CompositionLocalProvider(
                androidx.compose.ui.platform.LocalLayoutDirection provides androidx.compose.ui.unit.LayoutDirection.Ltr,
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    CountryPicker(
                        selected = state.country,
                        onSelect = viewModel::onCountry,
                        light = light,
                    )
                    Box(Modifier.width(8.dp))
                    Field(
                        value = state.phone,
                        onValue = viewModel::onPhone,
                        placeholder = state.country.example,
                        keyboard = KeyboardType.Phone,
                        imeAction = ImeAction.Send,
                        onImeAction = { viewModel.sendCode(currentMillis()) },
                        focusRequester = focus,
                        light = light,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
            Text(
                text = stringResource(R.string.signin_phone_hint),
                color = if (light) MealsMuted else Color.White.copy(alpha = 0.55f),
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 8.dp),
            )
            Box(Modifier.padding(top = 16.dp)) {
                BusyButton(
                    label = stringResource(R.string.signin_send_code),
                    busyLabel = stringResource(R.string.signin_sending),
                    busy = state.busy,
                    enabled = state.canSend,
                    onClick = { viewModel.sendCode(currentMillis()) },
                    fill = accent,
                )
            }
            androidx.compose.material3.Text(
                stringResource(R.string.legal_agree),
                color = androidx.compose.ui.graphics.Color.Gray,
                fontSize = ae.marketplace.mobile.android.ui.Type.Secondary,
                modifier = Modifier.padding(top = 12.dp),
            )
            ae.marketplace.mobile.android.ui.LegalLinks(color = accent)
        } else {
            Field(
                value = state.code,
                onValue = viewModel::onCode,
                placeholder = stringResource(R.string.signin_code_hint),
                keyboard = KeyboardType.NumberPassword,
                imeAction = ImeAction.Done,
                onImeAction = viewModel::verify,
                focusRequester = focus,
                light = light,
            )

            // Submitted on the sixth digit rather than on a button press.
            //
            // The code is a known, fixed length, so there is nothing to confirm
            // — asking for a tap after the last digit is a step that exists only
            // because the form was built before anyone thought about it. The
            // button stays for the retry case and for anyone whose autofill
            // pastes all six at once.
            LaunchedEffect(state.code) {
                if (state.code.length == CODE_LENGTH && !state.busy) {
                    keyboard?.hide()
                    viewModel.verify()
                }
            }

            Box(Modifier.padding(top = 16.dp)) {
                BusyButton(
                    label = stringResource(R.string.signin_verify),
                    busyLabel = stringResource(R.string.signin_checking),
                    busy = state.busy,
                    enabled = state.code.length == CODE_LENGTH && !state.busy,
                    onClick = viewModel::verify,
                    fill = accent,
                )
            }

            Row(Modifier.fillMaxWidth().padding(top = 16.dp)) {
                Text(
                    text = stringResource(R.string.signin_change_number),
                    color = if (light) MealsMuted else Color.White.copy(alpha = 0.5f),
                    fontSize = 13.sp,
                    modifier = Modifier.clickable(onClick = viewModel::changeNumber),
                )
                Box(Modifier.weight(1f))
                Text(
                    // The countdown is shown rather than the button being
                    // silently inert. A disabled control with no explanation is
                    // read as a bug, and this one is disabled for a reason the
                    // viewer would agree with if told.
                    text = if (cooldown > 0) stringResource(R.string.signin_resend_in, cooldown)
                    else stringResource(R.string.signin_resend),
                    color = if (cooldown > 0) ink.copy(alpha = 0.35f) else accent,
                    fontSize = 13.sp,
                    modifier = Modifier.clickable(enabled = cooldown == 0 && !state.busy) {
                        viewModel.sendCode(currentMillis())
                    },
                )
            }
        }

        state.error?.let { problem ->
            Text(
                text = problemText(problem, state),
                color = accent,
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 14.dp),
            )
        }
    }
}

private const val CODE_LENGTH = 6

/**
 * The primary button with a spinner and a "Sending…"/"Checking…" label while a
 * request is out. Dimming alone looked frozen on a slow connection, where the
 * request can take the full ten-second timeout.
 */
@Composable
private fun BusyButton(
    label: String,
    busyLabel: String,
    busy: Boolean,
    enabled: Boolean,
    onClick: () -> Unit,
    fill: Color,
) {
    Box(
        Modifier
            .fillMaxWidth()
            .background(
                if (enabled || busy) fill else fill.copy(alpha = 0.3f),
                androidx.compose.foundation.shape.RoundedCornerShape(10.dp),
            )
            .clickable(enabled = enabled, onClick = onClick)
            .padding(vertical = 15.dp),
        contentAlignment = Alignment.Center,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (busy) {
                androidx.compose.material3.CircularProgressIndicator(
                    color = Color.White,
                    strokeWidth = 2.dp,
                    modifier = Modifier.size(16.dp),
                )
                Box(Modifier.width(10.dp))
            }
            Text(
                if (busy) busyLabel else label,
                color = Color.White,
                fontSize = 15.sp,
                fontWeight = androidx.compose.ui.text.font.FontWeight.SemiBold,
            )
        }
    }
}

private val MealsPaper = Color(0xFFFFF8F0)
private val MealsInk = Color(0xFF1E1410)
private val MealsMuted = Color(0xFF76675B)
private val MealsLine = Color(0xFFE6D5C3)
private val MealsChilli = Color(0xFFC43B16)

/**
 * Words for a problem, chosen by what the viewer should do next.
 *
 * Every one of these ends in an instruction. "Something went wrong" is the
 * message this screen used to show for all of them, and it is the one message
 * that cannot be acted on.
 */
@Composable
private fun problemText(problem: SignInProblem, state: SignInUiState): String {
    val wait = state.retryAfterSeconds?.let { seconds ->
        if (seconds < 60) stringResource(R.string.signin_wait_seconds, seconds.coerceAtLeast(1))
        else stringResource(R.string.signin_wait_minutes, state.retryAfterMinutes ?: 1)
    }
    return when {
        problem == SignInProblem.WrongLength ->
            stringResource(R.string.signin_err_phone_length, stringResource(state.country.nameRes()))
        problem == SignInProblem.TooMany && wait != null ->
            stringResource(R.string.signin_err_too_many_wait, wait)
        problem == SignInProblem.ResendTooMany && wait != null ->
            stringResource(R.string.signin_err_resend_too_many_wait, wait)
        else -> stringResource(problem.stringRes())
    }
}

private fun PhoneCountry.nameRes(): Int = when (this) {
    PhoneCountry.AE -> R.string.signin_country_ae
    PhoneCountry.SA -> R.string.signin_country_sa
    PhoneCountry.QA -> R.string.signin_country_qa
    PhoneCountry.KW -> R.string.signin_country_kw
    PhoneCountry.BH -> R.string.signin_country_bh
    PhoneCountry.OM -> R.string.signin_country_om
}

/** The calling-code chip in front of the number, opening the six GCC countries. */
@Composable
private fun CountryPicker(
    selected: PhoneCountry,
    onSelect: (PhoneCountry) -> Unit,
    light: Boolean,
) {
    val ink = if (light) MealsInk else Color.White
    var open by remember { mutableStateOf(false) }
    val label = stringResource(R.string.signin_country)
    val name = stringResource(selected.nameRes())
    Box {
        Row(
            Modifier
                .then(if (light) Modifier.background(Color.White, RoundedCornerShape(8.dp)) else Modifier)
                .border(1.dp, if (light) MealsLine else Line, RoundedCornerShape(8.dp))
                .clickable { open = true }
                .semantics { contentDescription = "$label: $name ${selected.dialCode}" }
                .padding(start = 12.dp, end = 6.dp, top = 14.dp, bottom = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(selected.dialCode, color = ink, fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
            Icon(Icons.Rounded.ArrowDropDown, contentDescription = null, tint = ink, modifier = Modifier.size(20.dp))
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            PhoneCountry.entries.forEach { c ->
                DropdownMenuItem(
                    text = { Text("${c.dialCode}  ${stringResource(c.nameRes())}") },
                    onClick = {
                        onSelect(c)
                        open = false
                    },
                )
            }
        }
    }
}

private fun SignInProblem.stringRes(): Int = when (this) {
    SignInProblem.BadPhone -> R.string.signin_err_phone
    SignInProblem.WrongLength -> R.string.signin_err_phone
    SignInProblem.Superseded -> R.string.signin_err_superseded
    SignInProblem.WrongCode -> R.string.signin_err_code
    SignInProblem.NeedsNewCode -> R.string.signin_err_stale
    SignInProblem.TooMany -> R.string.signin_err_too_many
    SignInProblem.Offline -> R.string.signin_err_offline
    SignInProblem.AccountBlocked -> R.string.signin_err_blocked
    SignInProblem.ResendFailed -> R.string.signin_err_resend_failed
    SignInProblem.ResendTooMany -> R.string.signin_err_resend_too_many
    SignInProblem.Unknown -> R.string.signin_err_unknown
}

private fun currentMillis(): Long = System.currentTimeMillis()

@Composable
private fun Field(
    value: String,
    onValue: (String) -> Unit,
    placeholder: String,
    keyboard: KeyboardType,
    imeAction: ImeAction,
    onImeAction: () -> Unit,
    focusRequester: FocusRequester,
    light: Boolean = true,
    modifier: Modifier = Modifier,
) {
    val ink = if (light) MealsInk else Color.White
    Box(
        modifier
            .fillMaxWidth()
            .then(if (light) Modifier.background(Color.White, RoundedCornerShape(8.dp)) else Modifier)
            .border(1.dp, if (light) MealsLine else Line, RoundedCornerShape(8.dp))
            .padding(horizontal = 14.dp, vertical = 14.dp),
        contentAlignment = Alignment.CenterStart,
    ) {
        if (value.isEmpty()) {
            Text(
                placeholder,
                color = ink.copy(alpha = if (light) 0.4f else 0.25f),
                style = TextStyle(fontSize = 16.sp, textDirection = TextDirection.Ltr),
            )
        }
        BasicTextField(
            value = value,
            onValueChange = onValue,
            singleLine = true,
            // Phone numbers and codes read left to right in Arabic too; without this "+971" renders as "971+".
            textStyle = TextStyle(color = ink, fontSize = 16.sp, textDirection = TextDirection.Ltr),
            cursorBrush = SolidColor(if (light) MealsChilli else Accent),
            keyboardOptions = KeyboardOptions(keyboardType = keyboard, imeAction = imeAction),
            keyboardActions = KeyboardActions(
                onSend = { onImeAction() },
                onDone = { onImeAction() },
            ),
            modifier = Modifier.fillMaxWidth().focusRequester(focusRequester),
        )
    }
}
