package ae.marketplace.mobile.android.meals

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.model.MealTags
import ae.marketplace.mobile.model.NextWeekPreview
import ae.marketplace.mobile.model.RotationRecap
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

/** "Lebanese home cooking", from the kitchen's first cuisine tag. */
@Composable
private fun cuisineLine(cuisines: List<String>, arabic: Boolean): String {
    val first = cuisines.firstOrNull() ?: return stringResource(R.string.meals_next_week_home_style)
    return stringResource(R.string.meals_next_week_home_cooking, MealTags.label(first, arabic))
}

/** The "Next week" card at the top of My meals: who cooks and what, per day. */
@Composable
internal fun NextWeekCard(items: List<NextWeekPreview>, arabic: Boolean, onKitchen: (String) -> Unit) {
    val p = items.firstOrNull() ?: return
    val kitchen = pick(p.kitchenName, p.kitchenNameAr, arabic)
    Card {
        Row(
            Modifier.fillMaxWidth().clickable { onKitchen(p.branchId) },
            verticalAlignment = Alignment.CenterVertically,
        ) {
            KitchenCover(
                kitchen, p.coverImageUrl, p.cuisines, Modifier.size(72.dp),
                fallbackUrl = p.meals.firstNotNullOfOrNull { m -> m.shown?.imageUrl },
            )
            Column(Modifier.weight(1f).padding(start = Space.Large)) {
                Text(
                    stringResource(R.string.meals_next_week_kicker, shortDate(p.startsOn, arabic) + " – " + shortDate(p.endsOn, arabic)),
                    color = M.Chilli, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold,
                )
                Text(
                    stringResource(R.string.meals_next_week_title, kitchen, cuisineLine(p.cuisines, arabic)),
                    color = M.Ink, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold,
                )
                Text(stringResource(R.string.meals_next_week_open), color = M.Chilli, fontSize = Type.Secondary)
            }
        }
        LazyRow(Modifier.padding(top = Space.Small), horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
            items(p.meals, key = { it.date + it.mealSlot }) { m ->
                val d = m.shown
                val name = d?.let { pick(it.name, it.nameAr, arabic) } ?: stringResource(R.string.meals_next_week_no_dishes)
                Column(Modifier.width(112.dp)) {
                    KitchenCover(name, d?.imageUrl, emptyList(), Modifier.size(112.dp, 76.dp))
                    Text(shortDate(m.date, arabic) + " · " + slotLabel(m.mealSlot), color = M.Muted, fontSize = Type.Secondary)
                    Text(name, color = M.Ink, fontSize = Type.Secondary, maxLines = 2)
                }
            }
        }
        if (items.size > 1) {
            Text(stringResource(R.string.meals_next_week_more, items.size - 1), color = M.Muted, fontSize = Type.Secondary)
        }
    }
}

/** The end-of-cycle recap inside a rotation card, from week 4 on. */
@Composable
internal fun RecapBlock(recap: RotationRecap, arabic: Boolean, onLink: (String) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = Space.Small)) {
        Text(
            stringResource(if (recap.ended) R.string.meals_recap_ended else R.string.meals_recap_title),
            color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold,
        )
        Row(Modifier.fillMaxWidth().padding(vertical = Space.Small)) {
            RecapStat(recap.kitchenCount, stringResource(R.string.meals_recap_kitchens))
            RecapStat(recap.cuisineCount, stringResource(R.string.meals_recap_cuisines))
            RecapStat(recap.dishesDelivered, stringResource(R.string.meals_recap_dishes))
            RecapStat(recap.daysSkipped, stringResource(R.string.meals_recap_skipped))
        }
        recap.favourite?.let { f ->
            Text(
                stringResource(R.string.meals_recap_favourite, pick(f.kitchenName, f.kitchenNameAr, arabic), f.rating),
                color = M.Leaf, fontSize = Type.Secondary,
            )
        }
        PrimaryButton(stringResource(R.string.meals_recap_renew), { onLink(recap.renewPath) }, modifier = Modifier.padding(top = Space.Small))
    }
}

@Composable
private fun androidx.compose.foundation.layout.RowScope.RecapStat(n: Int, label: String) {
    Column(Modifier.weight(1f)) {
        Text(n.toString(), color = M.Chilli, fontSize = Type.Heading, fontWeight = FontWeight.Bold)
        Text(label, color = M.Muted, fontSize = Type.Secondary)
    }
}
