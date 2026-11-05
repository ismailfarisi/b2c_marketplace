package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.words
import ae.marketplace.mobile.android.PLACES
import ae.marketplace.mobile.android.Place
import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.model.ClockOption
import ae.marketplace.mobile.model.SellField
import ae.marketplace.mobile.model.SellVertical
import ae.marketplace.mobile.sell.FieldIssue
import ae.marketplace.mobile.sell.FieldProblem
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.isRetryable
import ae.marketplace.mobile.sell.SellUiState
import ae.marketplace.mobile.sell.SellValidation
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The sell flow's copies, now references. See ui.Palette for why one set.
internal val Accent = Palette.Accent
internal val Ink = Palette.Ink
internal val Line = Palette.Line

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
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    StepScaffold(
        title = stringResource(R.string.sell_what_listing),
        onBack = onBack,
        modifier = modifier,
        footer = null,
    ) {
        // Three different facts, and this step used to render all three as the
        // third. Someone who has just filmed a listing and is told the product
        // cannot sell it does not tap again — they leave.
        if (state.verticals.isEmpty()) {
            val failure = state.error
            when {
                failure != null -> FailureBlock(failure = failure, onRetry = onRetry)
                !state.verticalsLoaded -> CircularProgressIndicator(
                    color = Palette.Text.copy(alpha = 0.7f),
                    strokeWidth = 2.dp,
                    modifier = Modifier.size(24.dp),
                )
                else -> Text(
                    text = stringResource(R.string.sell_nothing_sellable),
                    color = Palette.Text.copy(alpha = 0.55f),
                    fontSize = 13.sp,
                )
            }
            return@StepScaffold
        }

        // A grid that loaded and a tap that failed. Picking a vertical fetches
        // its form, and when that fetch failed the view model set an error and
        // sent the seller back here — where nothing drew it, so the tile simply
        // did not respond. They tap it again, and again.
        state.error?.let {
            Text(
                text = it.words(),
                color = Palette.Bad,
                fontSize = 12.sp,
                modifier = Modifier.padding(bottom = 12.dp),
            )
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
        Text(glyph(vertical.icon), fontSize = 20.sp, color = if (active) Accent else Palette.Text)
        Text(
            text = vertical.label,
            color = Palette.Text,
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
    // A server refusal names fields by key; show each under its own box, and
    // anything that is not about a field (or a network/server failure) above
    // the button instead of nowhere.
    val rejected = state.error as? Failure.Rejected
    // Codes, not the server's English sentences: SellViewModel turns them into
    // `issues`, which are worded from the string table under each box.
    val serverFields = rejected?.fieldCodes.orEmpty()
    val formError = state.error?.takeIf { rejected == null || serverFields.keys.none { k -> form.fields.any { it.key == k } } }

    StepScaffold(
        title = stringResource(R.string.sell_three_fields),
        subtitle = state.selected?.label.orEmpty(),
        onBack = onBack,
        modifier = modifier,
        footer = {
            formError?.let { FailureBlock(it, onRetry = onNext) }
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
            // Bordered rather than filled, because for a car this is genuinely
            // optional and a solid control beside four required ones reads as a
            // fifth requirement.
            //
            // For property it is not optional at all — but this composable does
            // not know that and must not. What it knows is that the trust slot
            // may carry an identifier, and that the identifier may be required.
            // The vertical decides; the screen renders.
            Column(
                Modifier
                    .padding(top = 6.dp)
                    .fillMaxWidth()
                    .border(1.dp, Line, RoundedCornerShape(10.dp))
                    .padding(vertical = 14.dp, horizontal = 14.dp),
            ) {
                Text(
                    text = trust.label,
                    color = Palette.Text.copy(alpha = 0.75f),
                    fontSize = 13.sp,
                )

                trust.reference?.let { ref ->
                    // The same input the four fields use. A permit number is a
                    // text box; giving it a bespoke one here would be a second
                    // renderer to keep in step for no gain.
                    Box(Modifier.padding(top = 10.dp)) {
                        FieldInput(
                            field = ref,
                            value = state.values[ref.key].orEmpty(),
                            display = null,
                            issue = state.issues.firstOrNull { it.key == ref.key },
                            lookupResults = emptyList(),
                            // A reference is never a typeahead — the seam refuses a lookup
                            // kind for it, because a catalogue of every permit number ever
                            // issued is not a thing this platform has.
                            lookupOpen = false,
                            onValue = { onValue(ref.key, it) },
                            onLookupQuery = {},
                            onLookupPick = {},
                        )
                    }
                }

                // The document itself is still not collectable — there is no
                // upload endpoint for anything that is not a video. Said on the
                // row rather than discovered by tapping a control that does
                // nothing, and phrased so it is clear the number above *did*
                // land: an advertiser who reads "not built" next to a box they
                // just filled will assume they wasted their time.
                Text(
                    text = stringResource(R.string.sell_trust_doc_later),
                    color = Palette.Text.copy(alpha = 0.35f),
                    fontSize = 11.sp,
                    modifier = Modifier.padding(top = 8.dp),
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
    serverError: String? = null,
    onLookupQuery: (String) -> Unit,
    onLookupPick: (ae.marketplace.mobile.model.SellOption) -> Unit,
) {
    Column(Modifier.fillMaxWidth().padding(top = 14.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = field.label.uppercase(),
                color = Palette.Text.copy(alpha = 0.5f),
                fontSize = 10.sp,
                fontWeight = FontWeight.Medium,
            )
            if (field.unit.isNotEmpty()) {
                Text(
                    text = " · ${field.unit}",
                    color = Palette.Text.copy(alpha = 0.35f),
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
        if (issue == null && !serverError.isNullOrBlank()) {
            Text(
                text = serverError,
                color = Accent,
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 5.dp),
            )
        }
        if (issue == null && serverError.isNullOrBlank() && field.hint.isNotEmpty()) {
            Text(
                text = field.hint,
                color = Palette.Text.copy(alpha = 0.35f),
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 5.dp),
            )
        }
    }
}

@Composable
private fun TextRow(field: SellField, value: String, onValue: (String) -> Unit) {
    // The keyboard follows the kind. A numeric field that opens a QWERTY is a
    // field people mistype. Money is typed in dirhams (the shared view model
    // converts to minor units before validating and posting).
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
            Text(field.placeholder, color = Palette.Text.copy(alpha = 0.25f), fontSize = 15.sp)
        }
        BasicTextField(
            value = value,
            onValueChange = onValue,
            singleLine = true,
            textStyle = TextStyle(color = Palette.Text, fontSize = 15.sp),
            cursorBrush = SolidColor(Accent),
            keyboardOptions = KeyboardOptions(keyboardType = keyboard),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun SelectRow(field: SellField, value: String, onValue: (String) -> Unit) {
    // Chips rather than a dropdown. Seven short options fit on two rows and a
    // dropdown hides them behind a tap on the screen where abandonment matters.
    // A FlowRow, not fixed rows of four: at 320dp a fourth chip squeezed into
    // what was left of the row and wrapped its label one letter per line.
    FlowRow(
        Modifier.padding(top = 8.dp).fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        field.options.forEach { option ->
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
                    color = if (active) Color.White else Palette.Text.copy(alpha = 0.75f),
                    fontSize = 12.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
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
                color = if (chosen.isEmpty()) Palette.Text.copy(alpha = 0.25f) else Palette.Text,
                fontSize = 15.sp,
            )
        }
        BasicTextField(
            value = query,
            onValueChange = { query = it; onQuery(it) },
            singleLine = true,
            textStyle = TextStyle(color = Palette.Text, fontSize = 15.sp),
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
                .background(Palette.Surface, RoundedCornerShape(8.dp)),
        ) {
            items(results) { option ->
                Text(
                    text = option.label,
                    color = Palette.Text.copy(alpha = 0.9f),
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
    FieldProblem.Invalid -> stringResource(R.string.sell_field_invalid)
    // The bound is minor units for money; shared code turns it into what the
    // seller typed against (dirhams), so Android and iOS cannot disagree.
    FieldProblem.TooSmall -> stringResource(
        R.string.sell_field_min, SellValidation.boundText(field, issue), field.unit,
    )
    FieldProblem.TooLarge -> stringResource(
        R.string.sell_field_max, SellValidation.boundText(field, issue), field.unit,
    )
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
            color = Palette.Text.copy(alpha = 0.5f),
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
                        .background(if (active) Accent else Palette.Text.copy(alpha = 0.2f), RoundedCornerShape(50))
                        .padding(4.dp)
                )
                Text(
                    text = stringResource(place.labelRes),
                    color = if (active) Color.White else Palette.Text.copy(alpha = 0.75f),
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
        Text(clock.label, color = Palette.Text, fontSize = 15.sp, fontWeight = FontWeight.Medium)
        if (clock.detail.isNotEmpty()) {
            Text(
                clock.detail,
                color = Palette.Text.copy(alpha = 0.5f),
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
        Text(clock.label, color = Palette.Text, fontSize = 15.sp, fontWeight = FontWeight.Medium)
        if (clock.detail.isNotEmpty()) {
            Text(
                clock.detail,
                color = Palette.Text.copy(alpha = 0.55f),
                fontSize = 12.sp,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
        Text(
            text = stringResource(R.string.sell_only_term),
            color = Palette.Text.copy(alpha = 0.33f),
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
                color = Palette.Text,
                fontSize = 26.sp,
                modifier = Modifier.clickable(onClick = onBack).padding(end = 14.dp),
            )
            Column(Modifier.weight(1f)) {
                Text(title, color = Palette.Text, fontSize = 17.sp, fontWeight = FontWeight.SemiBold)
                if (subtitle.isNotEmpty()) {
                    Text(subtitle, color = Palette.Text.copy(alpha = 0.45f), fontSize = 12.sp)
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
            Column(
                Modifier.navigationBarsPadding().padding(horizontal = 18.dp, vertical = 12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) { it() }
        }
    }
}

@Composable
internal fun PrimaryButton(label: String, enabled: Boolean, onClick: () -> Unit, fill: Color = Accent) {
    Box(
        Modifier
            .fillMaxWidth()
            .background(if (enabled) fill else fill.copy(alpha = 0.3f), RoundedCornerShape(10.dp))
            .clickable(enabled = enabled, onClick = onClick)
            .padding(vertical = 15.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
    }
}
