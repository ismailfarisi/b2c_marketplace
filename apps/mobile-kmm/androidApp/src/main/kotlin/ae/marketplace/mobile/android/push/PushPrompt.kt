package ae.marketplace.mobile.android.push

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.Type
import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.core.content.ContextCompat

/**
 * Asks for notifications after sign-in, with a sentence first saying what
 * they are for. Only when push can work in this build (Firebase configured),
 * only on Android 13+, and only once: "Not now" is remembered, and Me shows
 * an Open Settings row while notifications are off.
 */
@Composable
fun PushPrompt(authorised: Boolean) {
    val context = LocalContext.current
    var show by remember { mutableStateOf(false) }
    val request = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    LaunchedEffect(authorised) {
        show = authorised && Push.available && Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED &&
            !prefs(context).getBoolean(ASKED, false)
    }
    if (!show) return
    fun answered() {
        prefs(context).edit().putBoolean(ASKED, true).apply()
        show = false
    }
    AlertDialog(
        onDismissRequest = { answered() },
        containerColor = Palette.Surface,
        title = { Text(stringResource(R.string.push_rationale_title), color = Palette.Text) },
        text = { Text(stringResource(R.string.push_rationale_body), color = Palette.TextMuted, fontSize = Type.Body) },
        confirmButton = {
            TextButton(onClick = {
                answered()
                if (Build.VERSION.SDK_INT >= 33) request.launch(Manifest.permission.POST_NOTIFICATIONS)
            }) { Text(stringResource(R.string.push_rationale_allow), color = Palette.Accent) }
        },
        dismissButton = {
            TextButton(onClick = { answered() }) {
                Text(stringResource(R.string.push_rationale_later), color = Palette.Text)
            }
        },
    )
}

private const val ASKED = "push_rationale_answered"
private fun prefs(context: Context) = context.getSharedPreferences("push", Context.MODE_PRIVATE)
