package ae.marketplace.mobile.android

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Restaurant
import androidx.compose.material.icons.outlined.SmartDisplay
import androidx.compose.material.icons.rounded.CalendarMonth
import androidx.compose.material.icons.rounded.Person
import androidx.compose.material.icons.rounded.Restaurant
import androidx.compose.material.icons.rounded.SmartDisplay
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * Every top-level screen. Only [BarDestinations] are on the bar; Search, Sell
 * and Saved belong to the video marketplace and open from the feed.
 */
enum class Destination {
    Meals,
    MyMeals,
    Feed,
    Search,
    Sell,
    Saved,
    Me,
}

/** Meals is the product, so it and the customer's own meals lead. */
val BarDestinations = listOf(Destination.Meals, Destination.MyMeals, Destination.Feed, Destination.Me)

private val Chilli = Color(0xFFC43B16)
private val ChilliSoft = Color(0xFFFBE3DA)
private val Muted = Color(0xFF76675B)
private val Hairline = Color(0xFFF0E2D2)

/**
 * Height of the bar above the system navigation inset. Screens that draw under
 * the bar (the reel overlay, the sticky plans bar) clear this plus that inset.
 */
val NAV_BAR_HEIGHT = 64.dp

/**
 * The bottom bar: icon and label per destination.
 *
 * White on the meals tabs, with a Chilli Red selection pill. Over the video
 * feed it floats on a gradient scrim so the reel stays full-bleed.
 */
@Composable
fun BottomNav(
    selected: Destination,
    onSelect: (Destination) -> Unit,
    modifier: Modifier = Modifier,
) {
    val light = selected == Destination.Meals || selected == Destination.MyMeals
    Column(
        modifier
            .fillMaxWidth()
            .then(
                if (light) Modifier.background(Color.White)
                else Modifier.background(
                    Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.8f))),
                ),
            )
            .navigationBarsPadding(),
    ) {
        if (light) Box(Modifier.fillMaxWidth().height(1.dp).background(Hairline))
        Row(
            Modifier
                .fillMaxWidth()
                .height(NAV_BAR_HEIGHT),
            horizontalArrangement = Arrangement.SpaceEvenly,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            BarDestinations.forEach { destination ->
                NavItem(
                    destination = destination,
                    active = destination == selected,
                    light = light,
                    onClick = { onSelect(destination) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
private fun NavItem(
    destination: Destination,
    active: Boolean,
    light: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val tint = when {
        active && light -> Chilli
        active -> Color.White
        light -> Muted
        else -> Color.White.copy(alpha = 0.7f)
    }
    // The whole slot is the target, so there is no dead space between tabs.
    Column(
        modifier
            .fillMaxHeight()
            .selectable(selected = active, onClick = onClick, role = Role.Tab),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(
            Modifier
                .width(56.dp)
                .height(30.dp)
                .background(
                    when {
                        active && light -> ChilliSoft
                        active -> Color.White.copy(alpha = 0.18f)
                        else -> Color.Transparent
                    },
                    RoundedCornerShape(50),
                ),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = destination.icon(active),
                contentDescription = null,
                tint = tint,
                modifier = Modifier.size(22.dp),
            )
        }
        Text(
            text = stringResource(destination.labelRes()),
            color = tint,
            fontSize = 12.sp,
            fontWeight = if (active) FontWeight.SemiBold else FontWeight.Medium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.padding(top = 4.dp, start = 4.dp, end = 4.dp),
        )
    }
}

private fun Destination.icon(active: Boolean): ImageVector = when (this) {
    Destination.Meals -> if (active) Icons.Rounded.Restaurant else Icons.Outlined.Restaurant
    Destination.MyMeals -> if (active) Icons.Rounded.CalendarMonth else Icons.Outlined.CalendarMonth
    Destination.Feed -> if (active) Icons.Rounded.SmartDisplay else Icons.Outlined.SmartDisplay
    else -> if (active) Icons.Rounded.Person else Icons.Outlined.Person
}

private fun Destination.labelRes(): Int = when (this) {
    Destination.Meals -> R.string.nav_meals
    Destination.Feed -> R.string.nav_feed
    Destination.Search -> R.string.nav_search
    Destination.Sell -> R.string.nav_sell
    Destination.Saved -> R.string.nav_saved
    Destination.Me -> R.string.nav_me
    Destination.MyMeals -> R.string.nav_my_meals
}
