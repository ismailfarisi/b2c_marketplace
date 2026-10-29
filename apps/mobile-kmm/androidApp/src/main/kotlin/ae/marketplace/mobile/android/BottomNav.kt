package ae.marketplace.mobile.android

import ae.marketplace.mobile.android.ui.Palette
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The five destinations.
 *
 * Only [Feed] is built. The other four are listed anyway, and that is a
 * deliberate choice rather than an oversight: the bar's job is to tell a viewer
 * what the product *is*, and a bar with one item does not do that. What it must
 * not do is lie — tapping Search reaches a screen that says Search is not built
 * yet, instead of a tab that swallows the tap and reads as a freeze.
 *
 * Ordering follows the mock and is not arbitrary. [Sell] sits in the middle
 * because it is the supply side of a marketplace whose binding constraint is
 * supply, and the middle slot is the one a thumb reaches without looking.
 */
enum class Destination {
    Meals,
    Feed,
    Search,
    Sell,
    Saved,
    Me,
}

// Was a private copy of the same hex in four files, two of which had drifted.
// See ae.marketplace.mobile.android.ui.Palette.
private val Accent = Palette.Accent
private val MealsChilli = Color(0xFFC43B16)

/**
 * NAV_BAR_HEIGHT excludes the system navigation inset, which the bar adds on top
 * via navigationBarsPadding. Callers that need to clear the bar — the reel
 * overlay, whose price and CTA would otherwise sit under it — add this plus the
 * same inset.
 */
val NAV_BAR_HEIGHT = 52.dp

/**
 * The bottom bar.
 *
 * Drawn over the reel rather than beside it. A feed that letterboxed its video
 * above a solid bar would give back the full-bleed frame the whole product is
 * built on, so the bar floats and earns its legibility from a gradient scrim
 * instead of an opaque background.
 *
 * Text only, no icons, matching the mock. That is also the honest choice while
 * four of five destinations are unbuilt: an icon set implies a finished
 * information architecture, and this one is not finished.
 */
@Composable
fun BottomNav(
    selected: Destination,
    onSelect: (Destination) -> Unit,
    modifier: Modifier = Modifier,
) {
    // Meals is a light, food-photo page; the scrim is for video only.
    val light = selected == Destination.Meals
    Column(
        modifier
            .fillMaxWidth()
            .then(if (light) Modifier.background(Color.White) else Modifier)
            .navigationBarsPadding()
            .then(if (light) Modifier else Modifier.background(
                // Scrim rather than a flat fill. A solid bar over a dark reel
                // reads as a black band across the bottom of the video; a
                // gradient keeps the frame continuous and still gives the labels
                // something to sit on when the reel is bright.
                Brush.verticalGradient(
                    listOf(Color.Transparent, Color.Black.copy(alpha = 0.75f)),
                )
            )),
    ) {
        Row(
            // The system inset is applied to the Column above rather than here.
            // Inside a fixed height it eats the content box instead of sitting
            // below it, and the first casualty is the 2dp rule under the active
            // label — which is the only thing distinguishing selected from not.
            Modifier
                .fillMaxWidth()
                .height(NAV_BAR_HEIGHT),
            horizontalArrangement = Arrangement.SpaceEvenly,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Destination.entries.forEach { destination ->
                NavItem(
                    label = stringResource(destination.labelRes()),
                    active = destination == selected,
                    light = light,
                    onClick = { onSelect(destination) },
                )
            }
        }
    }
}

@Composable
private fun NavItem(label: String, active: Boolean, light: Boolean, onClick: () -> Unit) {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier
            // clickable on the Column, not on the Text, so the touch target is
            // the whole slot. A 12sp word is a 30-pixel target and misses are
            // indistinguishable from an unresponsive app.
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 6.dp),
    ) {
        Text(
            text = label,
            color = when {
                active && light -> MealsChilli
                active -> Accent
                light -> Color(0xFF76675B)
                else -> Color.White.copy(alpha = 0.7f)
            },
            fontSize = 12.sp,
            fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
        )
        Box(
            Modifier
                .padding(top = 3.dp)
                .height(2.dp)
                .width(if (active) 14.dp else 0.dp)
                .background(if (active) Accent else Color.Transparent),
        )
    }
}

private fun Destination.labelRes(): Int = when (this) {
    Destination.Meals -> R.string.nav_meals
    Destination.Feed -> R.string.nav_feed
    Destination.Search -> R.string.nav_search
    Destination.Sell -> R.string.nav_sell
    Destination.Saved -> R.string.nav_saved
    Destination.Me -> R.string.nav_me
}

/**
 * What an unbuilt destination shows.
 *
 * It names the destination and says plainly that it is not built. The
 * alternative — an empty screen, or a spinner that never resolves — is
 * indistinguishable from a bug, and the person most likely to hit it is someone
 * demoing the app.
 */
@Composable
fun NotBuiltYet(destination: Destination, modifier: Modifier = Modifier) {
    Box(modifier.fillMaxWidth(), Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(
                text = stringResource(destination.labelRes()),
                color = Color.White,
                fontSize = 20.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = stringResource(R.string.nav_not_built),
                color = Color.White.copy(alpha = 0.55f),
                fontSize = 13.sp,
                modifier = Modifier.padding(top = 8.dp, start = 32.dp, end = 32.dp),
            )
        }
    }
}
