package ae.marketplace.mobile.android.meals

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.FailureStrip
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.meals.KitchenMenu
import ae.marketplace.mobile.model.MenuChoice
import ae.marketplace.mobile.model.PromoOffer
import ae.marketplace.mobile.network.Failure
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

/** A weekday tab on the kitchen menu; [hasChoice] adds the dot the help text refers to (web: ms-day-dot). */
@Composable
internal fun DayChip(label: String, selected: Boolean, hasChoice: Boolean, onClick: () -> Unit) {
    val choice = stringResource(R.string.meals_two_choices)
    Row(
        Modifier
            .semantics {
                this.selected = selected
                role = Role.Tab
                if (hasChoice) contentDescription = "$label, $choice"
            }
            .clickable(onClick = onClick)
            .background(if (selected) M.Chilli else M.Surface, RoundedCornerShape(50))
            .border(1.dp, if (selected) M.Chilli else M.Line, RoundedCornerShape(50))
            .defaultMinSize(minHeight = 36.dp)
            .padding(horizontal = 14.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(
            label,
            color = if (selected) M.OnAccent else M.Ink,
            fontSize = Type.Secondary,
            fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
        )
        if (hasChoice) {
            Box(Modifier.size(7.dp).background(if (selected) M.OnAccent else M.Chilli, RoundedCornerShape(50)))
        }
    }
}

/** "640 kcal · Gluten, Sesame" under a dish name; nothing when the kitchen entered neither. */
@Composable
internal fun DishFacts(d: MenuChoice, arabic: Boolean) {
    val parts = listOfNotNull(
        d.calories?.let { stringResource(R.string.meals_kcal, it) },
        KitchenMenu.allergenLine(d, arabic).takeIf { it.isNotEmpty() }?.let { stringResource(R.string.meals_allergens, it) },
    )
    if (parts.isNotEmpty()) Text(parts.joinToString(" · "), color = M.Muted, fontSize = Type.Secondary)
}

@Composable
internal fun offerLabel(o: PromoOffer): String {
    val parts = mutableListOf<String>()
    o.trialDays?.takeIf { it > 0 }?.let { parts += stringResource(R.string.meals_offer_trial, it) }
    when (o.discountType) {
        "percent" -> {
            val pct = KitchenMenu.percent(o.percentBp)
            val cap = o.maxDiscountMinor
            parts += if (cap != null && cap > 0) {
                stringResource(R.string.meals_offer_percent_cap, pct, money(cap, o.currency))
            } else {
                stringResource(R.string.meals_offer_percent, pct)
            }
        }
        "fixed" -> parts += stringResource(R.string.meals_offer_fixed, money(o.amountMinor, o.currency))
    }
    return parts.joinToString(" · ")
}

/** The kitchen's public promos and trials, as on the web page; applied at checkout. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun OfferChips(offers: List<PromoOffer>, arabic: Boolean) {
    if (offers.isEmpty()) return
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        offers.forEach { o ->
            Column(
                Modifier
                    .background(M.SaffronSoft, RoundedCornerShape(12.dp))
                    .border(1.dp, M.Saffron, RoundedCornerShape(12.dp))
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            ) {
                val label = offerLabel(o)
                if (label.isNotEmpty()) Text(label, color = M.Ink, fontSize = Type.Secondary, fontWeight = FontWeight.Bold)
                val name = pick(o.name, o.nameAr, arabic) +
                    if (o.newCustomersOnly) " · " + stringResource(R.string.meals_offer_new_only) else ""
                Text(name, color = M.Muted, fontSize = Type.Caption)
            }
        }
    }
}

@Composable
internal fun KitchenReply(reply: String?) {
    if (reply.isNullOrBlank()) return
    Column(
        Modifier
            .padding(top = 4.dp)
            .background(M.Paper, RoundedCornerShape(10.dp))
            .padding(10.dp),
    ) {
        Text(stringResource(R.string.meals_kitchen_reply), color = M.Ink, fontSize = Type.Caption, fontWeight = FontWeight.Bold)
        Text(reply, color = M.Ink, fontSize = Type.Secondary)
    }
}

/** Reviews failed while the kitchen loaded: say so, with a retry, instead of hiding the section. */
@Composable
internal fun ReviewsFailed(failure: Failure, onRetry: () -> Unit) {
    Column(Modifier.padding(horizontal = 0.dp)) {
        Text(
            stringResource(R.string.meals_reviews_failed),
            color = M.Ink,
            fontSize = Type.Secondary,
            modifier = Modifier.padding(horizontal = ae.marketplace.mobile.android.ui.Space.Edge),
        )
        FailureStrip(failure, onRetry = onRetry)
    }
}
