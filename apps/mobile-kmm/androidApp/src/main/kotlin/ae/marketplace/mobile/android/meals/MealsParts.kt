package ae.marketplace.mobile.android.meals

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import coil3.compose.AsyncImagePainter
import coil3.compose.rememberAsyncImagePainter
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import ae.marketplace.mobile.model.localised
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import java.time.format.TextStyle
import java.util.Locale

/**
 * The meals tab's own accent set. The app shell stays dark for video; meals
 * borrow the web storefront's warm tones so the two read as one brand.
 */
internal object Warm {
    val Peach = Color(0xFFF6D9C2)
    val Cream = Color(0xFFFFF4EA)
    val Saffron = Color(0xFFE59A3B)
    val Leaf = Color(0xFF6BAF6E)
    val Card = Color(0xFF221D1A)
    val Covers = listOf(
        Color(0xFF8C4A2F), Color(0xFF3F6B4F), Color(0xFF7A5C2E), Color(0xFF3E5A7A),
        Color(0xFF7A3E5A), Color(0xFF5A6B2E),
    )
    val Weeks = listOf(Color(0xFFE59A3B), Color(0xFF6BAF6E), Color(0xFF5B9BD5), Color(0xFFC46B8C))
}

internal val Serif = FontFamily.Serif

internal fun money(minor: Long, currency: String): String =
    "$currency " + String.format(Locale.US, "%,.2f", minor / 100.0)

internal fun weekday(day: Int, arabic: Boolean, style: TextStyle = TextStyle.SHORT): String {
    // Weekday 0 is Sunday in the API; java.time counts Monday as 1.
    val dow = if (day == 0) DayOfWeek.SUNDAY else DayOfWeek.of(day)
    return dow.getDisplayName(style, if (arabic) Locale("ar") else Locale.ENGLISH)
}

internal fun shortDate(iso: String, arabic: Boolean): String = runCatching {
    LocalDate.parse(iso.take(10)).format(
        DateTimeFormatter.ofPattern("EEE d MMM", if (arabic) Locale("ar") else Locale.ENGLISH),
    )
}.getOrDefault(iso)

@Composable
internal fun slotLabel(slot: String): String = when (slot) {
    "breakfast" -> stringResource(R.string.meals_slot_breakfast)
    "lunch" -> stringResource(R.string.meals_slot_lunch)
    "dinner" -> stringResource(R.string.meals_slot_dinner)
    else -> slot
}

@Composable
internal fun statusLabel(status: String): String = when (status) {
    "active" -> stringResource(R.string.meals_status_active)
    "pending", "pending_payment" -> stringResource(R.string.meals_status_pending)
    "paused" -> stringResource(R.string.meals_status_paused)
    "cancelled" -> stringResource(R.string.meals_status_cancelled)
    "ended", "completed", "expired" -> stringResource(R.string.meals_status_ended)
    else -> status
}

internal fun pick(en: String, ar: String?, arabic: Boolean) = localised(en, ar ?: "", arabic)

/** A photo when the kitchen has one, otherwise a coloured cover with its initial. */
@Composable
internal fun KitchenCover(
    name: String,
    imageUrl: String?,
    cuisines: List<String>,
    modifier: Modifier = Modifier,
) {
    val tint = Warm.Covers[(name.hashCode() and 0x7fffffff) % Warm.Covers.size]
    Box(
        modifier.background(
            Brush.linearGradient(listOf(tint, tint.copy(alpha = 0.55f))),
            RoundedCornerShape(14.dp),
        ),
        contentAlignment = Alignment.Center,
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(
                name.take(1).uppercase(),
                color = Warm.Cream,
                fontFamily = Serif,
                fontSize = Type.Title * 1.6f,
                fontWeight = FontWeight.SemiBold,
            )
            if (cuisines.isNotEmpty()) {
                Text(
                    cuisines.take(2).joinToString(" · "),
                    color = Warm.Cream.copy(alpha = 0.8f),
                    fontSize = Type.Caption,
                )
            }
        }
        if (!imageUrl.isNullOrBlank()) {
            // Drawn over the monogram, and only once it loads: a broken link
            // leaves the designed cover rather than an empty box.
            val painter = rememberAsyncImagePainter(imageUrl)
            val state by painter.state.collectAsState()
            if (state is AsyncImagePainter.State.Success) {
                Image(
                    painter = painter,
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize().clip(RoundedCornerShape(14.dp)),
                )
            }
        }
    }
}

@Composable
internal fun Chip(label: String, selected: Boolean, onClick: () -> Unit, enabled: Boolean = true) {
    Text(
        label,
        color = when {
            selected -> Palette.Ink
            enabled -> Palette.Text
            else -> Palette.TextFaint
        },
        fontSize = Type.Secondary,
        fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
        modifier = Modifier
            .semantics { this.selected = selected; role = Role.Checkbox }
            .clickable(enabled = enabled, onClick = onClick)
            .background(if (selected) Warm.Peach else Palette.Surface, RoundedCornerShape(50))
            .border(1.dp, if (selected) Warm.Peach else Palette.Line, RoundedCornerShape(50))
            .defaultMinSize(minHeight = 36.dp)
            .padding(horizontal = 14.dp, vertical = 8.dp),
    )
}

@Composable
internal fun PrimaryButton(label: String, onClick: () -> Unit, enabled: Boolean = true, modifier: Modifier = Modifier) {
    Box(
        modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = Space.Touch)
            .background(if (enabled) Warm.Saffron else Palette.Line, RoundedCornerShape(12.dp))
            .clickable(enabled = enabled, onClick = onClick)
            .semantics { role = Role.Button },
        contentAlignment = Alignment.Center,
    ) {
        Text(
            label,
            color = if (enabled) Palette.Ink else Palette.TextFaint,
            fontWeight = FontWeight.SemiBold,
            fontSize = Type.Body,
        )
    }
}

@Composable
internal fun TopBar(title: String, onBack: (() -> Unit)?, action: (@Composable () -> Unit)? = null) {
    Row(
        Modifier
            .fillMaxWidth()
            .statusBarsPadding()
            .padding(horizontal = Space.Small, vertical = Space.Small),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (onBack != null) {
            Text(
                "‹ " + stringResource(R.string.meals_back),
                color = Palette.TextMuted,
                fontSize = Type.Body,
                modifier = Modifier
                    .clickable(onClick = onBack)
                    .defaultMinSize(minHeight = Space.Touch)
                    .padding(horizontal = Space.Small, vertical = 14.dp),
            )
        }
        Text(
            title,
            color = Palette.Text,
            fontSize = Type.Heading,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f).padding(horizontal = Space.Small),
        )
        action?.invoke()
    }
}

@Composable
internal fun SectionTitle(text: String) {
    Text(
        text,
        color = Palette.Text,
        fontFamily = Serif,
        fontSize = Type.Title,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(start = Space.Edge, end = Space.Edge, top = Space.XL, bottom = Space.Medium),
    )
}

@Composable
internal fun Card(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Column(
        modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = 6.dp)
            .background(Warm.Card, RoundedCornerShape(16.dp))
            .border(1.dp, Palette.Line, RoundedCornerShape(16.dp))
            .padding(Space.Large),
        verticalArrangement = Arrangement.spacedBy(Space.Small),
    ) { content() }
}

@Composable
internal fun Line(label: String, value: String, strong: Boolean = false) {
    Row(Modifier.fillMaxWidth()) {
        Text(label, color = if (strong) Palette.Text else Palette.TextMuted, fontSize = Type.Body, modifier = Modifier.weight(1f))
        Text(value, color = Palette.Text, fontSize = Type.Body, fontWeight = if (strong) FontWeight.Bold else FontWeight.Normal)
    }
}

@Composable
internal fun WeekBars(modifier: Modifier = Modifier) {
    Row(modifier, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        Warm.Weeks.forEach {
            Box(Modifier.weight(1f).height(6.dp).background(it, RoundedCornerShape(3.dp)))
        }
    }
}
