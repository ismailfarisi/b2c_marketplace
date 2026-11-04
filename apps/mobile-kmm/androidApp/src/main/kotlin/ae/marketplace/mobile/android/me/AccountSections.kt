package ae.marketplace.mobile.android.me

import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.push.Push
import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.SecondaryButton
import ae.marketplace.mobile.android.ui.Shape
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.android.ui.words
import ae.marketplace.mobile.me.AccountViewModel
import ae.marketplace.mobile.me.NotificationPreference
import ae.marketplace.mobile.model.Address
import ae.marketplace.mobile.network.Failure
import android.app.LocaleManager
import android.content.Intent
import android.os.Build
import android.os.LocaleList
import android.provider.Settings
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.core.app.NotificationManagerCompat
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.LifecycleOwner

/**
 * The account half of Me: who is signed in, saved addresses, the marketing
 * opt-out, notifications and language. Drawn first, because most people on
 * this tab are meal customers rather than sellers.
 */
@Composable
internal fun AccountSections(vm: AccountViewModel) {
    val s by vm.state.collectAsState()
    Column(Modifier.fillMaxWidth()) {
        when {
            s.loading && s.profile == null -> LoadingBlock(inline = true)
            s.error != null && s.profile == null -> FailureBlock(s.error!!, onRetry = { vm.load(force = true) })
            else -> {
                Profile(vm)
                Addresses(vm)
                Notifications(vm)
                Language()
            }
        }
    }
}

@Composable
private fun Heading(text: String) {
    Text(
        text = text,
        color = Palette.Text.copy(alpha = 0.6f),
        fontSize = Type.Caption,
        fontWeight = FontWeight.Medium,
        modifier = Modifier.padding(start = Space.Edge, end = Space.Edge, top = Space.XL, bottom = Space.Tight),
    )
}

@Composable
private fun Profile(vm: AccountViewModel) {
    val s by vm.state.collectAsState()
    val p = s.profile ?: return
    var editing by remember { mutableStateOf(false) }
    LaunchedEffect(s.profileSaved) { editing = false }
    Heading(stringResource(R.string.account_profile))
    Row(
        Modifier.fillMaxWidth().padding(horizontal = Space.Edge),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                p.displayName?.takeIf { it.isNotBlank() } ?: stringResource(R.string.account_no_name),
                color = Palette.Text, fontSize = Type.Body, fontWeight = FontWeight.SemiBold,
            )
            Text(p.email ?: stringResource(R.string.account_no_email), color = Palette.TextMuted, fontSize = Type.Secondary)
            // LTR isolate: a +971 number must not be reordered inside Arabic.
            Text(stringResource(R.string.account_phone, "\u2066${p.phone}\u2069"), color = Palette.TextMuted, fontSize = Type.Secondary)
        }
        SecondaryButton(stringResource(R.string.account_edit), { vm.clearProfileError(); editing = true })
    }
    if (editing) ProfileEditor(vm, onClose = { editing = false })
}

@Composable
private fun ProfileEditor(vm: AccountViewModel, onClose: () -> Unit) {
    val s by vm.state.collectAsState()
    val p = s.profile ?: return
    var name by remember { mutableStateOf(p.displayName.orEmpty()) }
    var email by remember { mutableStateOf(p.email.orEmpty()) }
    val rejected = s.profileError as? Failure.Rejected
    AlertDialog(
        onDismissRequest = { if (!s.saving) onClose() },
        containerColor = Palette.Surface,
        title = { Text(stringResource(R.string.account_profile), color = Palette.Text) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Space.Small)) {
                OutlinedTextField(
                    value = name, onValueChange = { name = it }, singleLine = true,
                    label = { Text(stringResource(R.string.account_name)) },
                    isError = rejected?.fields?.containsKey("display_name") == true,
                    supportingText = rejected?.fields?.get("display_name")?.let { { Text(it) } },
                )
                OutlinedTextField(
                    value = email, onValueChange = { email = it }, singleLine = true,
                    label = { Text(stringResource(R.string.account_email)) },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
                    isError = rejected?.fields?.containsKey("email") == true,
                    supportingText = rejected?.fields?.get("email")?.let { { Text(it) } },
                )
                Text(stringResource(R.string.account_phone_note), color = Palette.TextMuted, fontSize = Type.Secondary)
                val other = s.profileError?.takeIf { rejected == null || rejected.fields.isEmpty() }
                other?.let { Text(it.words(), color = Palette.Bad, fontSize = Type.Secondary) }
            }
        },
        confirmButton = {
            TextButton(onClick = { vm.saveProfile(name, email) }, enabled = !s.saving) {
                Text(stringResource(R.string.account_save), color = Palette.Accent)
            }
        },
        dismissButton = {
            TextButton(onClick = onClose, enabled = !s.saving) {
                Text(stringResource(R.string.account_cancel), color = Palette.Text)
            }
        },
    )
}

@Composable
private fun Addresses(vm: AccountViewModel) {
    val s by vm.state.collectAsState()
    var removing by remember { mutableStateOf<Address?>(null) }
    Heading(stringResource(R.string.account_addresses))
    if (s.addresses.isEmpty()) {
        Text(
            stringResource(R.string.account_no_addresses),
            color = Palette.TextMuted, fontSize = Type.Secondary,
            modifier = Modifier.padding(horizontal = Space.Edge),
        )
    }
    s.addresses.forEach { a ->
        Column(
            Modifier
                .fillMaxWidth()
                .padding(horizontal = Space.Edge, vertical = Space.Tight)
                .border(1.dp, Palette.Line, RoundedCornerShape(Shape.Field))
                .padding(Space.Medium),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(a.label, color = Palette.Text, fontSize = Type.Body, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                if (a.isDefault) Text(stringResource(R.string.account_default), color = Palette.Good, fontSize = Type.Caption, fontWeight = FontWeight.Bold)
            }
            Text(a.line, color = Palette.TextMuted, fontSize = Type.Secondary)
            Row(horizontalArrangement = Arrangement.spacedBy(Space.Large)) {
                val busy = s.addressBusy != null
                if (!a.isDefault) {
                    Text(
                        stringResource(R.string.account_make_default),
                        color = Palette.Accent, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.clickable(enabled = !busy, role = Role.Button) { vm.makeDefault(a.id) }
                            .defaultMinSize(minHeight = Space.Touch).padding(vertical = 14.dp),
                    )
                }
                Text(
                    stringResource(R.string.account_remove),
                    color = Palette.Bad, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.clickable(enabled = !busy, role = Role.Button) { removing = a }
                        .defaultMinSize(minHeight = Space.Touch).padding(vertical = 14.dp),
                )
            }
        }
    }
    s.addressError?.let {
        Text(it.words(), color = Palette.Bad, fontSize = Type.Secondary, modifier = Modifier.padding(horizontal = Space.Edge))
    }
    removing?.let { a ->
        AlertDialog(
            onDismissRequest = { removing = null },
            containerColor = Palette.Surface,
            title = { Text(stringResource(R.string.account_remove_title), color = Palette.Text) },
            text = { Text("${a.label} · ${a.line}\n\n" + stringResource(R.string.account_remove_body), color = Palette.TextMuted, fontSize = Type.Body) },
            confirmButton = {
                TextButton(onClick = { removing = null; vm.removeAddress(a.id) }) {
                    Text(stringResource(R.string.account_remove), color = Palette.Bad)
                }
            },
            dismissButton = {
                TextButton(onClick = { removing = null }) { Text(stringResource(R.string.account_keep), color = Palette.Text) }
            },
        )
    }
}

@Composable
private fun Notifications(vm: AccountViewModel) {
    val s by vm.state.collectAsState()
    val context = LocalContext.current
    // Re-read on every resume: the person may come back from Settings.
    var enabled by remember { mutableStateOf(NotificationManagerCompat.from(context).areNotificationsEnabled()) }
    val owner = context as? LifecycleOwner
    DisposableEffect(owner) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) enabled = NotificationManagerCompat.from(context).areNotificationsEnabled()
        }
        owner?.lifecycle?.addObserver(observer)
        onDispose { owner?.lifecycle?.removeObserver(observer) }
    }
    Heading(stringResource(R.string.account_notifications))
    if (Push.available && !enabled) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = Space.Edge),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Space.Medium),
        ) {
            Text(stringResource(R.string.account_push_off), color = Palette.Text, fontSize = Type.Secondary, modifier = Modifier.weight(1f))
            SecondaryButton(stringResource(R.string.account_open_settings), {
                runCatching {
                    context.startActivity(
                        Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                            .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName),
                    )
                }
            })
        }
    }
    Text(
        stringResource(R.string.account_marketing_note),
        color = Palette.TextMuted, fontSize = Type.Secondary,
        modifier = Modifier.padding(horizontal = Space.Edge, vertical = Space.Tight),
    )
    s.preferences.forEach { p -> PreferenceRow(p, s.preferenceErrors[p.channel]) { vm.setPreference(p, it) } }
}

@Composable
private fun PreferenceRow(p: NotificationPreference, error: Failure?, onChange: (Boolean) -> Unit) {
    val label = when (p.channel) {
        "push" -> stringResource(R.string.account_marketing_push)
        "in_app" -> stringResource(R.string.account_marketing_in_app)
        "email" -> stringResource(R.string.account_marketing_email)
        "whatsapp" -> stringResource(R.string.account_marketing_whatsapp)
        "sms" -> stringResource(R.string.account_marketing_sms)
        else -> p.channel
    }
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(enabled = p.optional, role = Role.Switch) { onChange(!p.allowed) }
            .defaultMinSize(minHeight = Space.Touch)
            .padding(horizontal = Space.Edge),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, color = Palette.Text, fontSize = Type.Body, modifier = Modifier.weight(1f))
        Switch(
            checked = p.allowed,
            onCheckedChange = null,
            enabled = p.optional,
            colors = SwitchDefaults.colors(checkedTrackColor = Palette.Accent),
        )
    }
    error?.let { Text(it.words(), color = Palette.Bad, fontSize = Type.Secondary, modifier = Modifier.padding(horizontal = Space.Edge)) }
}

/** Android 13+ per-app language; older versions follow the phone's language. */
@Composable
private fun Language() {
    if (Build.VERSION.SDK_INT < 33) return
    val context = LocalContext.current
    var choosing by remember { mutableStateOf(false) }
    Heading(stringResource(R.string.account_language))
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(role = Role.Button) { choosing = true }
            .defaultMinSize(minHeight = Space.Touch)
            .padding(horizontal = Space.Edge),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(stringResource(R.string.account_language_current), color = Palette.Text, fontSize = Type.Body, modifier = Modifier.weight(1f))
        androidx.compose.material3.Icon(
            androidx.compose.material.icons.Icons.AutoMirrored.Filled.KeyboardArrowRight,
            contentDescription = null,
            tint = Palette.TextFaint,
        )
    }
    if (choosing) {
        fun use(tag: String) {
            choosing = false
            context.getSystemService(LocaleManager::class.java)?.applicationLocales = LocaleList.forLanguageTags(tag)
        }
        AlertDialog(
            onDismissRequest = { choosing = false },
            containerColor = Palette.Surface,
            title = { Text(stringResource(R.string.account_language), color = Palette.Text) },
            text = {
                Column {
                    // Each language named in itself, so it is findable from either.
                    TextButton(onClick = { use("en") }) { Text("English", color = Palette.Text) }
                    TextButton(onClick = { use("ar") }) { Text("العربية", color = Palette.Text) }
                }
            },
            confirmButton = {},
            dismissButton = {
                TextButton(onClick = { choosing = false }) { Text(stringResource(R.string.account_cancel), color = Palette.Text) }
            },
        )
    }
}
