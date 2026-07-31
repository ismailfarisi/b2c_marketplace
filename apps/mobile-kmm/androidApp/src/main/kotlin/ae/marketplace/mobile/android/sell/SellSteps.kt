package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.android.PLACES
import ae.marketplace.mobile.android.Place
import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.model.ClockOption
import ae.marketplace.mobile.model.SellField
import ae.marketplace.mobile.model.SellVertical
import ae.marketplace.mobile.sell.FieldIssue
import ae.marketplace.mobile.sell.FieldProblem
import ae.marketplace.mobile.sell.SellUiState
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

internal val Accent = Color(0xFFB4652A)
internal val Ink = Color(0xFF0E0E10)
internal val Line = Color(0xFF2A2A2E)

// ─────────────────────────────────────────────────────────────────────────────
// 2 · What are you listing?
// ─────────────────────────────────────────────────────────────────────────────

/**
 * The tile grid, served.
 *
 * There is no list of verticals in this file. The tiles come from
 * `GET /sell/verticals`, which the subject registry builds by asking each
 * composer's vertical for its own facet — so property and midnight offers get a
 * tile the day their composers register, with no app release.
 *
 * The mock shows four tiles. One is built, so one is drawn. A tile that opens a
 * form nobody implemented is worse than an absent one, because the seller has
 * already filmed by the time they find out.
 */
@Composable
fun VerticalStep(
    state: SellUiState,
    onPick: (SellVertical) -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    StepScaffold(
        title = stringResource(R.string.sell_what_listing),
        onBack = onBack,
        modifier = modifier,
        footer = null,
    ) {
        if (state.verticals.isEmpty()) {
            Text(
                text = stringResource(R.string.sell_nothing_sellable),
                color = Color.White.copy(alpha = 0.55f),
                fontSize = 13.sp,
            )
            return@StepScaffold
        }
        // Two per row, matching the mock. chunked rather than a LazyVerticalGrid:
        // the list is single digits and a lazy grid inside a scrolling column is
        // a nested-scroll argument for no benefit.
        state.verticals.chunked(2).forEach { row ->
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                row.forEach { vertical ->
                    VerticalTile(
                        vertical = vertical,
                        active = state.selected?.key == vertical.key,
                        onClick = { onPick(vertical) },
                        modifier = Modifier.weight(1f),
                    )
                }
                // Keeps a lone tile half-width rather than letting it stretch
                // across the row and read as a different kind of control.
                if (row.size == 1) Box(Modifier.weight(1f))
            }
        }
    }
}

@Composable
private fun VerticalTile(
    vertical: SellVertical,
    active: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .border(1.dp, if (active) Accent else Line, RoundedCornerShape(12.dp))
            .background(if (active) Accent.copy(alpha = 0.12f) else Color.Transparent, RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(14.dp),
    ) {
        Text(glyph(vertical.icon), fontSize = 20.sp, color = if (active) Accent else Color.White)
        Text(
            text = vertical.label,
            color = Color.White,
            fontSize = 14.sp,
            fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
            modifier = Modifier.padding(top = 22.dp),
        )
    }
}

/**
 * The one place a client maps a server value onto something, and it is safe.
 *
 * The mapping is on the *glyph*, not on the vertical: a new vertical picks a
 * name that already exists here and needs no release. An unrecognised name falls
 * back rather than drawing nothing, so a server that adds a glyph this build
 * predates still produces a legible tile.
 */
private fun glyph(icon: String): String = when (icon) {
    "car" -> "🚗"
    "home" -> "🏠"
    "meal" -> "🍽"
    "moon" -> "🌙"
    else -> "🏷"
}

// ─────────────────────────────────────────────────────────────────────────────
// 3 · The fields
// ─────────────────────────────────────────────────────────────────────────────

/**
 * The vertical's fields, rendered from data.
 *
 * Read this file for the word "mileage" or "price" and it is not here. Every
 * input below is driven by [SellField.kind], its label, its unit and its bounds,
 * all of which arrived over the wire. The day a mulkiya reader lands and the car
 * form drops from four fields to three, this screen does not change.
 */
@Composable
fun DetailsStep(
    state: SellUiState,
    onValue: (String, String) -> Unit,
    onLookupQuery: (String, String) -> Unit,
    onLookupPick: (String, ae.marketplace.mobile.model.SellOption) -> Unit,
    onNext: () -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val form = state.form ?: return
    val issues = state.issues.associateBy { it.key }

    StepScaffold(
        title = stringResource(R.string.sell_three_fields),
        subtitle = state.selected?.label.orEmpty(),
        onBack = onBack,
        modifier = modifier,
        footer = {
            PrimaryButton(
                label = stringResource(R.string.sell_next_clock),
                enabled = !state.busy,
                onClick = onNext,
            )
        },
    ) {
        form.fields.forEach { field ->
            FieldInput(
                field = field,
                value = state.values[field.key].orEmpty(),
                display = state.lookupLabels[field.key],
                issue = issues[field.key],
                lookupOpen = state.lookupFor == field.key,
                lookupResults = state.lookupResults,
                onValue = { onValue(field.key, it) },
                onLookupQuery = { onLookupQuery(field.key, it) },
                onLookupPick = { onLookupPick(field.key, it) },
            )
        }

        form.trust?.let { trust ->
            // Drawn as a dashed invitation rather than a filled button, matching
            // the mock, because it is genuinely optional and a solid control
            // beside four required ones reads as a fifth requirement.
            //
            // Tapping it does nothing yet. That is stated on the row rather than
            // discovered by tapping — see the note in the summary; the document
            // upload endpoint does not exist.
            Column(
                Modifier
                    .padding(top = 6.dp)
                    .fillMaxWidth()
                    .border(1.dp, Line, RoundedCornerShape(10.dp))
                    .padding(vertical = 16.dp, horizontal = 14.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(trust.label, color = Color.White.copy(alpha = 0.75f), fontSize = 13.sp)
                Text(
                    text = stringResource(R.string.sell_trust_not_built),
                    color = Color.White.copy(alpha = 0.35f),
                    fontSize = 11.sp,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
    }
}

@Composable
private fun FieldInput(
    field: SellField,
    value: String,
    display: String?,
    issue: FieldIssue?,
    lookupOpen: Boolean,
    lookupResults: List<ae.marketplace.mobile.model.SellOption>,
    onValue: (String) -> Unit,
    onLookupQuery: (String) -> Unit,
    onLookupPick: (ae.marketplace.mobile.model.SellOption) -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(top = 14.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = field.label.uppercase(),
                color = Color.White.copy(alpha = 0.5f),
                fontSize = 10.sp,
                fontWeight = FontWeight.Medium,
            )
            if (field.unit.isNotEmpty()) {
                Text(
                    text = " · ${field.unit}",
                    color = Color.White.copy(alpha = 0.35f),
                    fontSize = 10.sp,
                )
            }
        }

        when (field.kind) {
            "select" -> SelectRow(field, value, onValue)
            "lookup" -> LookupRow(
                field = field,
                display = display,
                open = lookupOpen,
                results = lookupResults,
                onQuery = onLookupQuery,
                onPick = onLookupPick,
            )
            // integer, money, text, and anything this build does not recognise.
            // Falling back to a text box is what keeps a newer server's extra
            // field usable rather than invisible.
            else -> TextRow(field, value, onValue)
        }

        issue?.let {
            Text(
                text = issueText(field, it),
                color = Accent,
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 5.dp),
            )
        }
        if (issue == null && field.hint.isNotEmpty()) {
            Text(
                text = field.hint,
                color = Color.White.copy(alpha = 0.35f),
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 5.dp),
            )
        }
    }
}

@Composable
private fun TextRow(field: SellField, value: String, onValue: (String) -> Unit) {
    // The keyboard follows the kind. A numeric field that opens a QWERTY is a
    // field people mistype, and money in this flow is minor units — a decimal
    // point would be a value the server rejects.
    val keyboard = when (field.kind) {
        "integer", "money" -> KeyboardType.Number
        else -> KeyboardType.Text
    }
    Box(
        Modifier
            .padding(top = 6.dp)
            .fillMaxWidth()
            .border(1.dp, Line, RoundedCornerShape(8.dp))
            .padding(horizontal = 12.dp, vertical = 12.dp),
    ) {
        if (value.isEmpty() && field.placeholder.isNotEmpty()) {
            Text(field.placeholder, color = Color.White.copy(alpha = 0.25f), fontSize = 15.sp)
        }
        BasicTextField(
            value = value,
            onValueChange = onValue,
            singleLine = true,
            textStyle = TextStyle(color = Color.White, fontSize = 15.sp),
            cursorBrush = SolidColor(Accent),
            keyboardOptions = KeyboardOptions(keyboardType = keyboard),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@Composable
private fun SelectRow(field: SellField, value: String, onValue: (String) -> Unit) {
    // Chips rather than a dropdown. Seven short options fit on two rows and a
    // dropdown hides them behind a tap on the screen where abandonment matters.
    Column(Modifier.padding(top = 8.dp)) {
        field.options.chunked(4).forEach { row ->
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), modifier = Modifier.padding(bottom = 6.dp)) {
                row.forEach { option ->
                    val active = option.value == value
                    Box(
                        Modifier
                            .then(
                                if (active) Modifier.background(Accent, RoundedCornerShape(50))
                                else Modifier.border(1.dp, Line, RoundedCornerShape(50))
                            )
                            .clickable { onValue(option.value) }
                            .padding(horizontal = 13.dp, vertical = 7.dp),
                    ) {
                        Text(
                            option.label,
                            color = if (active) Color.White else Color.White.copy(alpha = 0.75f),
                            fontSize = 12.sp,
                        )
                    }
                }
            }
        }
    }
}

/**
 * The typeahead.
 *
 * The chosen row's *label* is shown and its opaque value is what gets posted.
 * This composable never parses either: "2021 Nissan Patrol" and
 * "nissan/patrol/2021" are both strings the server minted, and the moment this
 * file splits one on a slash the format cannot change without an app release.
 */
@Composable
private fun LookupRow(
    field: SellField,
    display: String?,
    open: Boolean,
    results: List<ae.marketplace.mobile.model.SellOption>,
    onQuery: (String) -> Unit,
    onPick: (ae.marketplace.mobile.model.SellOption) -> Unit,
) {
    var query by remember { mutableStateOf("") }
    val chosen = display.orEmpty()

    Box(
        Modifier
            .padding(top = 6.dp)
            .fillMaxWidth()
            .border(1.dp, Line, RoundedCornerShape(8.dp))
            .padding(horizontal = 12.dp, vertical = 12.dp),
    ) {
        if (query.isEmpty()) {
            Text(
                text = chosen.ifEmpty { field.placeholder },
                color = if (chosen.isEmpty()) Color.White.copy(alpha = 0.25f) else Color.White,
                fontSize = 15.sp,
            )
        }
        BasicTextField(
            value = query,
            onValueChange = { query = it; onQuery(it) },
            singleLine = true,
            textStyle = TextStyle(color = Color.White, fontSize = 15.sp),
            cursorBrush = SolidColor(Accent),
            modifier = Modifier.fillMaxWidth(),
        )
    }

    if (open && results.isNotEmpty()) {
        LazyColumn(
            Modifier
                .padding(top = 4.dp)
                .fillMaxWidth()
                // Bounded, and scrollable inside. An unbounded suggestion list
                // pushes the Next button off the screen and the seller cannot
                // reach it without dismissing the keyboard first.
                .heightIn(max = 200.dp)
                .background(Color(0xFF17171A), RoundedCornerShape(8.dp)),
        ) {
            items(results) { option ->
                Text(
                    text = option.label,
                    color = Color.White.copy(alpha = 0.9f),
                    fontSize = 14.sp,
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { query = ""; onPick(option) }
                        .padding(horizontal = 12.dp, vertical = 11.dp),
                )
            }
        }
    }
}

/**
 * Turns a [FieldIssue] into a sentence.
 *
 * Here rather than in shared code, and that is the point of the issue carrying a
 * code and a bound instead of a message. The wording lives in a string table a
 * translator can reach; shared code that returned "must be at least 1000" would
 * be an English sentence no Arabic build could fix.
 */
@Composable
private fun issueText(field: SellField, issue: FieldIssue): String = when (issue.problem) {
    FieldProblem.Required -> stringResource(R.string.sell_field_required)
    FieldProblem.NotANumber -> stringResource(R.string.sell_field_number)
    FieldProblem.NotAnOption -> stringResource(R.string.sell_field_option)
    // Money is minor units on the wire. Dividing here rather than in shared code
    // for the same reason the sentence lives here: it is a presentation decision,
    // and the field already told us which kind it is.
    FieldProblem.TooSmall -> stringResource(
        R.string.sell_field_min, major(field, issue.bound), field.unit,
    )
    FieldProblem.TooLarge -> stringResource(
        R.string.sell_field_max, major(field, issue.bound), field.unit,
    )
}

private fun major(field: SellField, bound: Long?): String {
    val n = bound ?: return ""
    return if (field.kind == "money") group(n / 100) else group(n)
}

private fun group(n: Long): String {
    val s = n.toString()
    if (s.length <= 3) return s
    return s.reversed().chunked(3).joinToString(",").reversed()
}

// ─────────────────────────────────────────────────────────────────────────────
// 4 · The clock and the place
// ─────────────────────────────────────────────────────────────────────────────

/**
 * "How long should it run?"
 *
 * The clock *is* the listing type — an open listing, an auction and a flash
 * window are different products with different overlays — which is why each card
 * states its consequence rather than just its duration. A seller is choosing
 * what their buyers will see.
 */
@Composable
fun ClockStep(
    state: SellUiState,
    onClock: (String) -> Unit,
    onPlace: (Place) -> Unit,
    onPublish: () -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val form = state.form ?: return

    StepScaffold(
        title = stringResource(R.string.sell_how_long),
        onBack = onBack,
        modifier = modifier,
        footer = {
            PrimaryButton(
                label = stringResource(R.string.sell_publish),
                enabled = state.canPublish,
                onClick = onPublish,
            )
        },
    ) {
        if (state.clockIsAChoice) {
            form.clocks.forEach { clock ->
                ClockCard(
                    clock = clock,
                    active = state.activeClock?.key == clock.key,
                    onClick = { onClock(clock.key) },
                )
            }
        } else {
            // One option is a statement, not a choice. A control that cannot
            // change anything still implies the alternatives exist — and for
            // cars today they do not: an auction needs a bid table and a flash
            // window needs a recurrence rule, and neither is built.
            state.activeClock?.let { ClockStatement(it) }
        }

        Text(
            text = stringResource(R.string.sell_emirate_area),
            color = Color.White.copy(alpha = 0.5f),
            fontSize = 10.sp,
            fontWeight = FontWeight.Medium,
            modifier = Modifier.padding(top = 22.dp, bottom = 4.dp),
        )
        PLACES.forEach { place ->
            val active = state.place == place.label
            Row(
                Modifier
                    .fillMaxWidth()
                    .clickable { onPlace(place) }
                    .padding(vertical = 11.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Box(
                    Modifier
                        .padding(start = 2.dp)
                        .background(if (active) Accent else Color.White.copy(alpha = 0.2f), RoundedCornerShape(50))
                        .padding(4.dp)
                )
                Text(
                    text = place.label,
                    color = if (active) Color.White else Color.White.copy(alpha = 0.75f),
                    fontSize = 15.sp,
                    fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
                )
            }
        }

        // Said plainly rather than left for the seller to discover. Buyers only
        // ever see listings that can reach them, so a car with no place picked
        // reaches nobody — which the seller would experience as zero views.
        if (state.place.isEmpty()) {
            Text(
                text = stringResource(R.string.sell_place_required),
                color = Accent,
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
    }
}

@Composable
private fun ClockCard(clock: ClockOption, active: Boolean, onClick: () -> Unit) {
    Column(
        Modifier
            .padding(bottom = 10.dp)
            .fillMaxWidth()
            .border(if (active) 2.dp else 1.dp, if (active) Accent else Line, RoundedCornerShape(10.dp))
            .clickable(onClick = onClick)
            .padding(14.dp),
    ) {
        Text(clock.label, color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.Medium)
        if (clock.detail.isNotEmpty()) {
            Text(
                clock.detail,
                color = Color.White.copy(alpha = 0.5f),
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
    }
}

@Composable
private fun ClockStatement(clock: ClockOption) {
    Column(
        Modifier
            .fillMaxWidth()
            .background(Accent.copy(alpha = 0.10f), RoundedCornerShape(10.dp))
            .padding(14.dp),
    ) {
        Text(clock.label, color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.Medium)
        if (clock.detail.isNotEmpty()) {
            Text(
                clock.detail,
                color = Color.White.copy(alpha = 0.55f),
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
        Text(
            text = stringResource(R.string.sell_only_term),
            color = Color.White.copy(alpha = 0.33f),
            fontSize = 11.sp,
            modifier = Modifier.padding(top = 8.dp),
        )
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared chrome
// ─────────────────────────────────────────────────────────────────────────────

@Composable
internal fun StepScaffold(
    title: String,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    subtitle: String = "",
    footer: (@Composable () -> Unit)?,
    content: @Composable () -> Unit,
) {
    Column(modifier.fillMaxSize().background(Ink)) {
        Row(
            Modifier
                .fillMaxWidth()
                .statusBarsPadding()
                .padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "‹",
                color = Color.White,
                fontSize = 26.sp,
                modifier = Modifier.clickable(onClick = onBack).padding(end = 14.dp),
            )
            Column(Modifier.weight(1f)) {
                Text(title, color = Color.White, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
                if (subtitle.isNotEmpty()) {
                    Text(subtitle, color = Color.White.copy(alpha = 0.45f), fontSize = 12.sp)
                }
            }
        }

        Column(
            Modifier
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 18.dp),
        ) {
            content()
            // Breathing room under the last control so the footer never sits
            // flush against it on a short screen.
            Box(Modifier.padding(bottom = 24.dp))
        }

        footer?.let {
            Box(Modifier.navigationBarsPadding().padding(horizontal = 18.dp, vertical = 12.dp)) { it() }
        }
    }
}

@Composable
internal fun PrimaryButton(label: String, enabled: Boolean, onClick: () -> Unit) {
    Box(
        Modifier
            .fillMaxWidth()
            .background(if (enabled) Accent else Accent.copy(alpha = 0.3f), RoundedCornerShape(10.dp))
            .clickable(enabled = enabled, onClick = onClick)
            .padding(vertical = 15.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
    }
}
