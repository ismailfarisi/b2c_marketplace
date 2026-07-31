package ae.marketplace.mobile.android.me

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.Shape
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.me.MeViewModel
import ae.marketplace.mobile.me.SearchFilters
import ae.marketplace.mobile.me.SearchSort
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp

/**
 * The facets the classifieds index has always accepted and the app never sent.
 *
 * # What is here and what is not
 *
 * Price, year, mileage, spec, body type and seller kind — plus sort, which is
 * kept visually apart because it reorders rather than hides, and conflating the
 * two is how someone ends up unable to find the filter that is excluding their
 * car.
 *
 * Fuel type and transmission are accepted by the server and not offered here.
 * That is a judgement rather than an oversight: in this market a used-car buyer
 * filters on spec and mileage first and almost never on gearbox, and every
 * additional row is one more thing between them and the results. The API keeps
 * accepting them, so adding a row later costs a line.
 *
 * # Why the values are hard-coded
 *
 * Regional spec and body type are enumerations in `vehicle_listings`, not
 * catalogue rows — a new one is a migration, which is already an app release's
 * worth of coordination. This is the opposite call to the feed's vertical
 * chips, which are *served*, because a vertical genuinely appears without a
 * schema change.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SearchFilterSheet(
    viewModel: MeViewModel,
    draft: SearchFilters,
    hasLocation: Boolean,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        sheetState = sheetState,
        containerColor = Palette.Ink,
        dragHandle = null,
    ) {
        Column(
            Modifier
                .padding(horizontal = Space.Edge)
                .padding(top = Space.Large)
                .navigationBarsPadding(),
        ) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = stringResource(R.string.filters_title),
                    color = Palette.Text,
                    fontSize = Type.Heading,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f),
                )
                // Only when there is something to clear. A permanently visible
                // Clear next to an untouched sheet is a control that does
                // nothing, which is how people learn to ignore controls.
                if (!draft.isEmpty) {
                    Text(
                        text = stringResource(R.string.filters_clear),
                        color = Palette.Accent,
                        fontSize = Type.Secondary,
                        modifier = Modifier
                            .clickable { viewModel.editFilters { SearchFilters(sort = it.sort) } }
                            .defaultMinSize(minHeight = Space.Touch)
                            .padding(vertical = Space.Medium, horizontal = Space.Small),
                    )
                }
            }

            SectionLabel(stringResource(R.string.filters_price))
            Row(horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
                // Minor units on the wire, major units on screen. The keypad
                // shows what a person says out loud; the multiply by 100 happens
                // once, here, rather than being a rule every caller remembers.
                NumberBox(
                    value = draft.priceMinMinor?.let { (it / 100).toString() }.orEmpty(),
                    placeholder = stringResource(R.string.filters_min),
                    modifier = Modifier.weight(1f),
                ) { v -> viewModel.editFilters { it.copy(priceMinMinor = v?.times(100)) } }
                NumberBox(
                    value = draft.priceMaxMinor?.let { (it / 100).toString() }.orEmpty(),
                    placeholder = stringResource(R.string.filters_max),
                    modifier = Modifier.weight(1f),
                ) { v -> viewModel.editFilters { it.copy(priceMaxMinor = v?.times(100)) } }
            }

            SectionLabel(stringResource(R.string.filters_year))
            Row(horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
                NumberBox(
                    value = draft.yearMin?.toString().orEmpty(),
                    placeholder = stringResource(R.string.filters_from),
                    modifier = Modifier.weight(1f),
                ) { v -> viewModel.editFilters { it.copy(yearMin = v?.toInt()) } }
                NumberBox(
                    value = draft.yearMax?.toString().orEmpty(),
                    placeholder = stringResource(R.string.filters_to),
                    modifier = Modifier.weight(1f),
                ) { v -> viewModel.editFilters { it.copy(yearMax = v?.toInt()) } }
            }

            SectionLabel(stringResource(R.string.filters_mileage))
            NumberBox(
                value = draft.mileageMax?.toString().orEmpty(),
                placeholder = stringResource(R.string.filters_mileage_hint),
                modifier = Modifier.fillMaxWidth(),
            ) { v -> viewModel.editFilters { it.copy(mileageMax = v?.toInt()) } }

            SectionLabel(stringResource(R.string.filters_spec))
            ChipRow(SPECS, draft.regionalSpecs) { value ->
                viewModel.editFilters { it.copy(regionalSpecs = it.toggle(it.regionalSpecs, value)) }
            }

            SectionLabel(stringResource(R.string.filters_body))
            ChipRow(BODY_TYPES, draft.bodyTypes) { value ->
                viewModel.editFilters { it.copy(bodyTypes = it.toggle(it.bodyTypes, value)) }
            }

            SectionLabel(stringResource(R.string.filters_seller))
            ChipRow(
                options = SELLERS,
                selected = setOfNotNull(draft.sellerKind),
            ) { value ->
                // Single-select: tapping the active one clears it back to
                // "both", which is what a chip that is already on should do.
                viewModel.editFilters { it.copy(sellerKind = if (it.sellerKind == value) null else value) }
            }

            SectionLabel(stringResource(R.string.filters_sort))
            ChipRow(
                // Nearest is only offered with a location. The server silently
                // falls back to newest without one, and a sort that looks
                // selected while producing a different order is worse than one
                // that is absent.
                options = sortOptions(hasLocation),
                selected = setOf(draft.sort ?: SearchSort.Newest.wire),
            ) { value ->
                viewModel.editFilters { it.copy(sort = value) }
            }

            Box(Modifier.padding(top = Space.XL, bottom = Space.Large)) {
                Box(
                    Modifier
                        .fillMaxWidth()
                        .background(Palette.Accent, RoundedCornerShape(Shape.Field))
                        .clickable {
                            viewModel.applyFilters()
                            onDismiss()
                        }
                        .defaultMinSize(minHeight = Space.Touch)
                        .padding(vertical = 14.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = stringResource(R.string.filters_apply),
                        color = Color.White,
                        fontSize = Type.Body,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
            }
        }
    }
}

private data class Opt(val value: String, val label: String)

// Mirrors the CHECK constraints on `vehicle_listings`. A value not in the
// column's constraint would be silently dropped by the server's csv() parse and
// return everything, which reads as the filter not working.
private val SPECS = listOf(
    Opt("gcc", "GCC"),
    Opt("us", "US"),
    Opt("euro", "Euro"),
    Opt("japan", "Japan"),
    Opt("other", "Other"),
)

private val BODY_TYPES = listOf(
    Opt("suv", "SUV"),
    Opt("sedan", "Sedan"),
    Opt("hatchback", "Hatchback"),
    Opt("coupe", "Coupe"),
    Opt("pickup", "Pickup"),
    Opt("van", "Van"),
)

private val SELLERS = listOf(
    Opt("private", "Private"),
    Opt("dealer", "Dealer"),
)

private fun sortOptions(hasLocation: Boolean): List<Opt> = buildList {
    add(Opt(SearchSort.Newest.wire, "Newest"))
    add(Opt(SearchSort.PriceAsc.wire, "Price ↑"))
    add(Opt(SearchSort.PriceDesc.wire, "Price ↓"))
    add(Opt(SearchSort.MileageAsc.wire, "Lowest km"))
    add(Opt(SearchSort.YearDesc.wire, "Newest year"))
    if (hasLocation) add(Opt(SearchSort.Distance.wire, "Nearest"))
}

@Composable
private fun SectionLabel(text: String) {
    Text(
        text = text.uppercase(),
        color = Palette.TextFaint,
        fontSize = Type.Micro,
        fontWeight = FontWeight.Bold,
        modifier = Modifier.padding(top = Space.Large, bottom = Space.Small),
    )
}

@Composable
private fun ChipRow(options: List<Opt>, selected: Set<String>, onToggle: (String) -> Unit) {
    Row(
        Modifier.horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(Space.Small),
    ) {
        options.forEach { opt ->
            val on = opt.value in selected
            Text(
                text = opt.label,
                color = if (on) Palette.Accent else Palette.TextMuted,
                fontSize = Type.Secondary,
                fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal,
                modifier = Modifier
                    .border(
                        1.dp,
                        if (on) Palette.Accent else Palette.Line,
                        RoundedCornerShape(50),
                    )
                    .background(
                        if (on) Palette.Accent.copy(alpha = 0.12f) else Color.Transparent,
                        RoundedCornerShape(50),
                    )
                    .clickable { onToggle(opt.value) }
                    .padding(horizontal = 14.dp, vertical = 10.dp),
            )
        }
    }
}

/**
 * A numeric box that reports null for empty.
 *
 * Null rather than zero, because they are different questions: "no minimum
 * price" and "a minimum price of nothing" produce the same number and only one
 * of them should reach the wire.
 */
@Composable
private fun NumberBox(
    value: String,
    placeholder: String,
    modifier: Modifier = Modifier,
    onValue: (Long?) -> Unit,
) {
    Box(
        modifier
            .border(1.dp, Palette.Line, RoundedCornerShape(Shape.Card))
            .padding(horizontal = Space.Medium, vertical = 13.dp),
        contentAlignment = Alignment.CenterStart,
    ) {
        if (value.isEmpty()) {
            Text(placeholder, color = Palette.TextGhost, fontSize = Type.Secondary)
        }
        BasicTextField(
            value = value,
            onValueChange = { raw ->
                // Digits only, and capped. Nine digits of price is a typo, and
                // without the cap it overflows the Long once multiplied by 100.
                val digits = raw.filter { it.isDigit() }.take(9)
                onValue(digits.toLongOrNull())
            },
            singleLine = true,
            textStyle = TextStyle(color = Palette.Text, fontSize = Type.Secondary),
            cursorBrush = SolidColor(Palette.Accent),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
