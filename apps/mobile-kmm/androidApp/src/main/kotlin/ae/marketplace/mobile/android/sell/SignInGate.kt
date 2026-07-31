package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.identity.SignInViewModel
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The one screen between the Sell tab and the camera.
 *
 * It is here rather than at launch because this is the first thing in the
 * product that genuinely cannot be anonymous: a listing belongs to someone and a
 * buyer has to be able to message them. Everything before it — the feed, the
 * filters, the surfaces — works signed out, and asking a visitor for a phone
 * number before they have seen a reel is how a discovery app loses the visit.
 *
 * It says why it is asking. A sign-in wall with no explanation reads as a
 * data grab; "buyers message you here" is the actual reason and it is also the
 * seller's benefit.
 */
@Composable
fun SignInGate(
    viewModel: SignInViewModel,
    onSignedIn: () -> Unit,
    onCancel: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val state by viewModel.state.collectAsState()

    if (state.signedIn) {
        onSignedIn()
        return
    }
    if (state.checking) {
        Box(modifier.fillMaxSize().background(Ink))
        return
    }

    Column(
        modifier
            .fillMaxSize()
            .background(Ink)
            .statusBarsPadding()
            .padding(horizontal = 22.dp, vertical = 18.dp),
    ) {
        Text(
            text = "✕",
            color = Color.White,
            fontSize = 22.sp,
            modifier = Modifier.clickable(onClick = onCancel).padding(bottom = 24.dp),
        )
        Text(
            text = stringResource(R.string.signin_title),
            color = Color.White,
            fontSize = 21.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            text = stringResource(R.string.signin_why),
            color = Color.White.copy(alpha = 0.55f),
            fontSize = 13.sp,
            modifier = Modifier.padding(top = 8.dp, bottom = 26.dp),
        )

        if (state.challengeId.isEmpty()) {
            Field(
                value = state.phone,
                onValue = viewModel::onPhone,
                placeholder = stringResource(R.string.signin_phone_hint),
                keyboard = KeyboardType.Phone,
            )
            Box(Modifier.padding(top = 16.dp)) {
                PrimaryButton(
                    label = stringResource(R.string.signin_send_code),
                    enabled = state.phone.isNotBlank() && !state.busy,
                    onClick = viewModel::sendCode,
                )
            }
        } else {
            Field(
                value = state.code,
                onValue = viewModel::onCode,
                placeholder = stringResource(R.string.signin_code_hint),
                keyboard = KeyboardType.NumberPassword,
            )
            Box(Modifier.padding(top = 16.dp)) {
                PrimaryButton(
                    label = stringResource(R.string.signin_verify),
                    enabled = state.code.isNotBlank() && !state.busy,
                    onClick = viewModel::verify,
                )
            }
            Text(
                text = stringResource(R.string.signin_change_number),
                color = Color.White.copy(alpha = 0.5f),
                fontSize = 13.sp,
                modifier = Modifier
                    .padding(top = 16.dp)
                    .clickable(onClick = viewModel::changeNumber),
            )
        }

        state.error?.let {
            Text(it, color = Accent, fontSize = 12.sp, modifier = Modifier.padding(top = 14.dp))
        }
    }
}

@Composable
private fun Field(
    value: String,
    onValue: (String) -> Unit,
    placeholder: String,
    keyboard: KeyboardType,
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
            keyboardOptions = KeyboardOptions(keyboardType = keyboard),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
