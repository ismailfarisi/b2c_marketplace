package ae.marketplace.mobile.android

import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.feed.FeedUiState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// Was a private copy of the same hex in four files, two of which had drifted.
// See ae.marketplace.mobile.android.ui.Palette.
private val Accent = Palette.Accent
private val Sheet = Palette.Surface

/**
 * A named point the feed can be served for.
 *
 * Hard-coded, and that is a statement about the schema rather than a shortcut:
 * there is no place taxonomy anywhere in the product — no emirate, district or
 * area table — so a district list has to come from somewhere and geography is
 * the one thing here that is not a product decision.
 *
 * When a places table lands this list is deleted and the sheet reads it from the
 * server, the same way the vertical chips already do.
 */
data class Place(val label: String, val lat: Double, val lng: Double)

val PLACES = listOf(
    Place("Dubai Marina", 25.0805, 55.1403),
    Place("Downtown Dubai", 25.1972, 55.2744),
    Place("Business Bay", 25.1857, 55.2766),
    Place("Al Quoz", 25.1420, 55.2330),
    Place("Al Barsha", 25.1120, 55.1960),
    Place("Deira", 25.2710, 55.3120),
)

/**
 * The filter sheet.
 *
 * Two axes, because those are the two the feed actually has: which vertical, and
 * where. Everything else a marketplace usually filters on — price, mileage,
 * make — belongs to the classifieds search surface, where `vehicles.Filter`
 * already implements it against a schema that supports it. Putting a price
 * slider on a feed whose candidates are ranked reels would be a control with
 * nothing behind it.
 *
 * Applied on tap, with no Apply button. Each change is a new query and the feed
 * behind the sheet reloads immediately; a confirm step on a two-control sheet
 * adds a tap and hides the result of the choice being made.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FilterSheet(
    state: FeedUiState,
    onVertical: (String?) -> Unit,
    onPlace: (Place) -> Unit,
    onUseMyLocation: () -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
        containerColor = Sheet,
    ) {
        Column(
            Modifier
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(horizontal = 20.dp)
                .padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            // The vertical row is drawn only when the server offered one. A row
            // holding nothing but "All" is a control that cannot change
            // anything, and it invites the reading that the feed is filtered
            // when it is not.
            if (state.verticals.isNotEmpty()) {
                SectionLabel(stringResource(R.string.filter_show))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Chip(
                        label = stringResource(R.string.filter_all),
                        active = state.selectedVertical == null,
                        onClick = { onVertical(null) },
                    )
                    state.verticals.forEach { vertical ->
                        Chip(
                            // The vertical's own label, rendered as given. There
                            // is no `when (key)` here: the moment one exists,
                            // shipping real estate means shipping a release.
                            label = vertical.label,
                            active = state.selectedVertical == vertical.key,
                            onClick = { onVertical(vertical.key) },
                        )
                    }
                }
            }

            SectionLabel(stringResource(R.string.filter_location))
            PlaceRow(
                label = stringResource(R.string.filter_use_my_location),
                active = state.locationLabel.isEmpty(),
                onClick = onUseMyLocation,
            )
            PLACES.forEach { place ->
                PlaceRow(
                    label = place.label,
                    active = state.locationLabel == place.label,
                    onClick = { onPlace(place) },
                )
            }

            // Said plainly rather than implied by the absence of a slider. A
            // viewer who expects a distance control and does not find one
            // assumes it is hidden; this tells them the feed does not work that
            // way.
            Text(
                text = stringResource(R.string.filter_reach_note),
                color = Color.White.copy(alpha = 0.4f),
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
    }
}

@Composable
private fun SectionLabel(text: String) {
    Text(
        text = text,
        color = Color.White.copy(alpha = 0.5f),
        fontSize = 11.sp,
        fontWeight = FontWeight.Medium,
        modifier = Modifier.padding(top = 8.dp),
    )
}

@Composable
private fun Chip(label: String, active: Boolean, onClick: () -> Unit) {
    val shape = RoundedCornerShape(50)
    Box(
        Modifier
            .then(if (active) Modifier.background(Accent, shape) else Modifier)
            .then(if (active) Modifier else Modifier.border(1.dp, Color.White.copy(alpha = 0.25f), shape))
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 9.dp),
    ) {
        Text(
            text = label,
            color = if (active) Color.White else Color.White.copy(alpha = 0.8f),
            fontSize = 13.sp,
            fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
        )
    }
}

/**
 * A location row.
 *
 * A list rather than chips, because place names are long and unequal — six of
 * them wrapped across a chip row reflows every time the selection changes, and a
 * control that moves under the thumb is a control that gets mis-tapped.
 */
@Composable
private fun PlaceRow(label: String, active: Boolean, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            Modifier
                .size(8.dp)
                .background(if (active) Accent else Color.White.copy(alpha = 0.2f), RoundedCornerShape(50)),
        )
        Text(
            text = label,
            color = if (active) Color.White else Color.White.copy(alpha = 0.75f),
            fontSize = 15.sp,
            fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
        )
    }
}

/**
 * The button that opens the sheet, and the summary of what is currently on.
 *
 * It states the active filters rather than showing a bare icon. A funnel glyph
 * tells a viewer that filtering exists; "Cars · Al Quoz" tells them why the feed
 * they are looking at is short, which is the question they actually have.
 */
@Composable
fun FilterButton(state: FeedUiState, onClick: () -> Unit, modifier: Modifier = Modifier) {
    val vertical = state.verticals.firstOrNull { it.key == state.selectedVertical }?.label
    val place = state.locationLabel.ifEmpty { stringResource(R.string.filter_nearby_you) }
    val summary = listOfNotNull(vertical, place).joinToString(" · ")

    Box(
        modifier
            .border(1.dp, Color.White.copy(alpha = 0.35f), RoundedCornerShape(50))
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
    ) {
        Text(summary, color = Color.White, fontSize = 11.sp, fontWeight = FontWeight.Medium)
    }
}
