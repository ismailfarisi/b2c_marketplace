package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.identity.SignInProblem
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
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
) {
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
            .background(Ink)
            .statusBarsPadding()
            .imePadding()
            .padding(horizontal = 22.dp, vertical = 18.dp),
    ) {
        val closeLabel = stringResource(R.string.action_close)
        Box(
            contentAlignment = Alignment.Center,
            modifier = Modifier
                .padding(bottom = 16.dp)
                .size(36.dp)
                .background(Palette.Surface, RoundedCornerShape(50))
                .clickable(onClick = onCancel)
                .semantics { contentDescription = closeLabel },
        ) {
            Icon(
                imageVector = Icons.Filled.Close,
                contentDescription = null,
                tint = Color.White,
                modifier = Modifier.size(18.dp),
            )
        }
        Text(
            text = stringResource(R.string.signin_title),
            color = Color.White,
            fontSize = 21.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            text = if (onCode) stringResource(R.string.signin_sent_to, state.phone.trim())
            else stringResource(R.string.signin_why),
            color = Color.White.copy(alpha = 0.55f),
            fontSize = 13.sp,
            modifier = Modifier.padding(top = 8.dp, bottom = 26.dp),
        )

        if (!onCode) {
            Field(
                value = state.phone,
                onValue = viewModel::onPhone,
                placeholder = stringResource(R.string.signin_phone_hint),
                keyboard = KeyboardType.Phone,
                imeAction = ImeAction.Send,
                onImeAction = { viewModel.sendCode(currentMillis()) },
                focusRequester = focus,
            )
            Box(Modifier.padding(top = 16.dp)) {
                PrimaryButton(
                    label = stringResource(R.string.signin_send_code),
                    enabled = state.phone.isNotBlank() && !state.busy,
                    onClick = { viewModel.sendCode(currentMillis()) },
                )
            }
        } else {
            Field(
                value = state.code,
                onValue = viewModel::onCode,
                placeholder = stringResource(R.string.signin_code_hint),
                keyboard = KeyboardType.NumberPassword,
                imeAction = ImeAction.Done,
                onImeAction = viewModel::verify,
                focusRequester = focus,
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
                PrimaryButton(
                    label = stringResource(R.string.signin_verify),
                    enabled = state.code.length == CODE_LENGTH && !state.busy,
                    onClick = viewModel::verify,
                )
            }

            Row(Modifier.fillMaxWidth().padding(top = 16.dp)) {
                Text(
                    text = stringResource(R.string.signin_change_number),
                    color = Color.White.copy(alpha = 0.5f),
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
                    color = if (cooldown > 0) Color.White.copy(alpha = 0.3f) else Accent,
                    fontSize = 13.sp,
                    modifier = Modifier.clickable(enabled = cooldown == 0 && !state.busy) {
                        viewModel.sendCode(currentMillis())
                    },
                )
            }
        }

        state.error?.let { problem ->
            Text(
                text = stringResource(problem.stringRes()),
                color = Accent,
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 14.dp),
            )
        }
    }
}

private const val CODE_LENGTH = 6

/**
 * Words for a problem, chosen by what the viewer should do next.
 *
 * Every one of these ends in an instruction. "Something went wrong" is the
 * message this screen used to show for all of them, and it is the one message
 * that cannot be acted on.
 */
private fun SignInProblem.stringRes(): Int = when (this) {
    SignInProblem.BadPhone -> R.string.signin_err_phone
    SignInProblem.WrongCode -> R.string.signin_err_code
    SignInProblem.NeedsNewCode -> R.string.signin_err_stale
    SignInProblem.TooMany -> R.string.signin_err_too_many
    SignInProblem.Offline -> R.string.signin_err_offline
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
) {
    Box(
        Modifier
            .fillMaxWidth()
            .border(1.dp, Line, RoundedCornerShape(8.dp))
            .padding(horizontal = 14.dp, vertical = 14.dp),
        contentAlignment = Alignment.CenterStart,
    ) {
        if (value.isEmpty()) {
            Text(placeholder, color = Color.White.copy(alpha = 0.25f), fontSize = 16.sp)
        }
        BasicTextField(
            value = value,
            onValueChange = onValue,
            singleLine = true,
            textStyle = TextStyle(color = Color.White, fontSize = 16.sp),
            cursorBrush = SolidColor(Accent),
            keyboardOptions = KeyboardOptions(keyboardType = keyboard, imeAction = imeAction),
            keyboardActions = KeyboardActions(
                onSend = { onImeAction() },
                onDone = { onImeAction() },
            ),
            modifier = Modifier.fillMaxWidth().focusRequester(focusRequester),
        )
    }
}
