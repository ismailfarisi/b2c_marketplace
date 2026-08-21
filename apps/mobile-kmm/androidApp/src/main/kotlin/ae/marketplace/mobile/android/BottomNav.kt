package ae.marketplace.mobile.android

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Bookmark
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.outlined.Bookmark
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The five destinations.
 *
 * Ordering follows the mock and is not arbitrary. [Sell] sits in the middle
 * because it is the supply side of a marketplace whose binding constraint is
 * supply, and the middle slot is the one a thumb reaches without looking.
 */
enum class Destination {
    Feed,
    Search,
    Sell,
    Saved,
    Me,
}

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
 * Icon only, no labels — the Instagram convention. All five destinations are
 * built now, and the glyphs (home, search, create, bookmark, person) are the
 * ones a decade of social apps have taught; the words live on as content
 * descriptions for TalkBack instead.
 */
@Composable
fun BottomNav(
    selected: Destination,
    onSelect: (Destination) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxWidth()
            .navigationBarsPadding()
            .background(
                // Scrim rather than a flat fill. A solid bar over a dark reel
                // reads as a black band across the bottom of the video; a
                // gradient keeps the frame continuous and still gives the icons
                // something to sit on when the reel is bright.
                Brush.verticalGradient(
                    listOf(Color.Transparent, Color.Black.copy(alpha = 0.75f)),
                )
            ),
    ) {
        Row(
            // The system inset is applied to the Column above rather than here.
            // Inside a fixed height it eats the content box instead of sitting
            // below it.
            Modifier
                .fillMaxWidth()
                .height(NAV_BAR_HEIGHT),
            horizontalArrangement = Arrangement.SpaceEvenly,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Destination.entries.forEach { destination ->
                NavItem(
                    destination = destination,
                    active = destination == selected,
                    onClick = { onSelect(destination) },
                )
            }
        }
    }
}

@Composable
private fun NavItem(destination: Destination, active: Boolean, onClick: () -> Unit) {
    Box(
        contentAlignment = Alignment.Center,
        modifier = Modifier
            // clickable on the Box, not on the Icon, so the touch target is the
            // whole slot. A 26dp glyph is a coin-flip target and misses are
            // indistinguishable from an unresponsive app.
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 6.dp),
    ) {
        if (destination == Destination.Sell) {
            // The create slot, dressed as Instagram's: a bordered rounded
            // square rather than a bare glyph, because the one action the
            // marketplace cannot live without should not look like the four
            // that are browsing.
            Box(
                contentAlignment = Alignment.Center,
                modifier = Modifier
                    .size(34.dp)
                    .border(
                        width = 1.5.dp,
                        color = if (active) Color.White else Color.White.copy(alpha = 0.65f),
                        shape = RoundedCornerShape(9.dp),
                    ),
            ) {
                Icon(
                    imageVector = Icons.Filled.Add,
                    contentDescription = stringResource(destination.labelRes()),
                    tint = if (active) Color.White else Color.White.copy(alpha = 0.65f),
                    modifier = Modifier.size(22.dp),
                )
            }
        } else {
            Icon(
                imageVector = destination.icon(active),
                contentDescription = stringResource(destination.labelRes()),
                tint = if (active) Color.White else Color.White.copy(alpha = 0.65f),
                modifier = Modifier.size(26.dp),
            )
        }
    }
}

/**
 * Filled when active, outlined when not — the selected state is the glyph
 * itself rather than a rule or a tint, which is what keeps the bar legible
 * over an arbitrary video frame.
 */
private fun Destination.icon(active: Boolean): ImageVector = when (this) {
    Destination.Feed -> if (active) Icons.Filled.Home else Icons.Outlined.Home
    Destination.Search -> if (active) Icons.Filled.Search else Icons.Outlined.Search
    Destination.Saved -> if (active) Icons.Filled.Bookmark else Icons.Outlined.Bookmark
    Destination.Me -> if (active) Icons.Filled.Person else Icons.Outlined.Person
    // Never read: Sell renders through its own branch in NavItem.
    Destination.Sell -> Icons.Filled.Add
}

private fun Destination.labelRes(): Int = when (this) {
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
