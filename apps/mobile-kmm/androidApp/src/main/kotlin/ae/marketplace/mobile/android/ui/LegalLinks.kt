package ae.marketplace.mobile.android.ui

import ae.marketplace.mobile.android.BuildConfig
import ae.marketplace.mobile.android.R
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp

/** Terms and Privacy policy (and Help, on Me), opened on the web in the app's language. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LegalLinks(color: Color, modifier: Modifier = Modifier, showHelp: Boolean = false) {
    val context = LocalContext.current
    val lang = if (context.resources.configuration.locales[0].language == "ar") "ar" else "en"
    fun open(page: String) = context.startActivity(
        Intent(Intent.ACTION_VIEW, Uri.parse("${BuildConfig.WEB_BASE_URL}/$lang/$page")),
    )
    FlowRow(modifier, horizontalArrangement = Arrangement.spacedBy(20.dp)) {
        val pages = listOfNotNull(
            if (showHelp) R.string.legal_help to "support" else null,
            // The kitchen console is web-only; vendors reach it from here.
            if (showHelp) R.string.legal_kitchen to "console" else null,
            R.string.legal_terms to "terms",
            R.string.legal_privacy to "privacy",
        )
        for ((label, page) in pages) {
            Text(
                stringResource(label),
                color = color,
                fontSize = Type.Secondary,
                textDecoration = TextDecoration.Underline,
                modifier = Modifier
                    .clickable { open(page) }
                    .defaultMinSize(minHeight = Space.Touch)
                    .padding(vertical = 12.dp),
            )
        }
    }
}
