package ae.marketplace.mobile.android.meals

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.EmptyBlock
import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.FailureStrip
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.android.ui.rememberArabic
import ae.marketplace.mobile.meals.MealsViewModel
import ae.marketplace.mobile.meals.StorefrontUiState
import ae.marketplace.mobile.model.Banner
import ae.marketplace.mobile.model.HomeSection
import ae.marketplace.mobile.model.MealTags
import ae.marketplace.mobile.network.Failure
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp

/** The slots the storefront filters by, as on web (Suhoor and Iftar included). */
internal val FilterSlots = listOf("breakfast", "lunch", "dinner", "suhoor", "iftar")

/** Kitchen/dish search and the rotation-kitchens toggle, under the slot chips. */
@Composable
internal fun DiscoveryFilters(s: StorefrontUiState, vm: MealsViewModel, arabic: Boolean) {
    Column(Modifier.padding(top = Space.Medium), verticalArrangement = Arrangement.spacedBy(Space.Small)) {
        val hint = stringResource(R.string.meals_search_kitchens)
        BasicTextField(
            value = s.query,
            onValueChange = vm::setQuery,
            singleLine = true,
            textStyle = TextStyle(color = M.Ink, fontSize = Type.Body),
            cursorBrush = SolidColor(M.Chilli),
            modifier = Modifier
                .fillMaxWidth()
                .background(M.Surface, RoundedCornerShape(10.dp))
                .border(1.dp, M.Line, RoundedCornerShape(10.dp))
                .padding(horizontal = Space.Medium, vertical = 12.dp),
            decorationBox = { inner ->
                if (s.query.isEmpty()) Text(hint, color = M.Faint, fontSize = Type.Body)
                inner()
            },
        )
        Row(
            Modifier.horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(Space.Small),
        ) {
            Chip(stringResource(R.string.meals_rotation_filter), s.rotationOnly, { vm.setRotationOnly(!s.rotationOnly) })
            s.cuisine?.let { c -> Chip(MealTags.label(c, arabic) + " ×", true, { vm.selectCuisine(null) }) }
        }
    }
}

/**
 * A refetch over a list that is still on screen: a spinner while it runs,
 * and the failure with Retry when it fails, so an old list never passes for
 * the answer to the new place or filter.
 */
@Composable
internal fun StoreRefreshNote(s: StorefrontUiState, onRetry: () -> Unit) {
    val failure: Failure? = s.error
    when {
        failure != null -> FailureStrip(failure, onRetry = onRetry, modifier = Modifier.padding(horizontal = Space.Edge))
        s.loading -> Row(
            Modifier.fillMaxWidth().padding(horizontal = Space.Edge, vertical = Space.Small),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Space.Small),
        ) {
            CircularProgressIndicator(Modifier.size(16.dp), color = M.Chilli, strokeWidth = 2.dp)
            Text(stringResource(R.string.meals_updating), color = M.Muted, fontSize = Type.Secondary)
        }
    }
}

/** Filters matched nothing: say so, and offer the way back. */
@Composable
internal fun FilteredEmpty(onClear: () -> Unit) {
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
        EmptyBlock(stringResource(R.string.meals_filters_empty))
        Chip(stringResource(R.string.meals_clear_filters), false, onClear)
    }
}

/** cuisine_strip: tap a cuisine to filter the list by it. */
@Composable
internal fun CuisineStrip(section: HomeSection, s: StorefrontUiState, arabic: Boolean, onPick: (String?) -> Unit) {
    // A cuisine every kitchen serves narrows nothing, so it isn't offered.
    val tiles = section.cuisines.orEmpty().filter {
        it.cuisine == s.cuisine || (it.kitchens > 0 && (s.unfilteredCount == 0 || it.kitchens < s.unfilteredCount))
    }
    if (tiles.size < 2) return
    val title = section.settings.title?.let { pick(it, section.settings.titleAr, arabic) }
        ?: stringResource(R.string.meals_cuisines)
    SectionTitle(title)
    LazyRow(
        contentPadding = PaddingValues(horizontal = Space.Edge),
        horizontalArrangement = Arrangement.spacedBy(Space.Small),
    ) {
        items(tiles, key = { it.cuisine }) { c ->
            val on = c.cuisine == s.cuisine
            Chip(MealTags.label(c.cuisine, arabic), on, { onPick(if (on) null else c.cuisine) })
        }
    }
}

/** A merchandising banner: its photo, copy and CTA; the whole card follows its link. */
@Composable
internal fun BannerCard(b: Banner, arabic: Boolean, onOpen: (String) -> Unit) {
    val link = b.linkUrl?.takeIf { it.isNotBlank() }
    val cta = b.cta?.let { pick(it, b.ctaAr, arabic) }
    Box(
        Modifier
            .width(300.dp)
            .height(160.dp)
            .background(M.Chilli, RoundedCornerShape(16.dp))
            .then(
                if (link != null) {
                    Modifier.semantics { role = Role.Button }.clickable { onOpen(link) }
                } else {
                    Modifier
                },
            ),
    ) {
        if (!b.imageUrl.isNullOrBlank()) {
            KitchenCover(b.title, b.imageUrl, emptyList(), Modifier.fillMaxSize())
            Box(
                Modifier.fillMaxSize().background(
                    Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.65f))),
                    RoundedCornerShape(16.dp),
                ),
            )
        }
        Column(Modifier.align(Alignment.BottomStart).padding(Space.Large), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(pick(b.title, b.titleAr, arabic), color = M.OnAccent, fontFamily = Serif, fontSize = Type.Heading, fontWeight = FontWeight.Bold)
            b.body?.let { Text(pick(it, b.bodyAr, arabic), color = M.OnAccent.copy(alpha = 0.8f), fontSize = Type.Secondary) }
            if (cta != null && link != null) {
                Text("$cta →", color = Warm.Saffron, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
            }
        }
    }
}

@Composable
internal fun BannerRail(section: HomeSection, arabic: Boolean, onOpen: (String) -> Unit) {
    val banners = section.banners.orEmpty()
    if (banners.isEmpty()) return
    LazyRow(
        contentPadding = PaddingValues(horizontal = Space.Edge, vertical = Space.Small),
        horizontalArrangement = Arrangement.spacedBy(Space.Medium),
    ) {
        items(banners, key = { it.id }) { b -> BannerCard(b, arabic, onOpen) }
    }
}

/** A collection's "See all": its kitchens that deliver to the current point. */
@Composable
internal fun CollectionPage(vm: MealsViewModel, onBack: () -> Unit, onKitchen: (String) -> Unit) {
    val s by vm.collection.collectAsState()
    val arabic = rememberArabic()
    val c = s.collection
    val failure = s.error
    Column(Modifier.fillMaxSize().statusBarsPadding()) {
        TopBar(c?.let { pick(it.title, it.titleAr, arabic) } ?: "", onBack)
        when {
            failure == Failure.NotFound -> EmptyBlock(stringResource(R.string.meals_collection_gone))
            failure != null -> FailureBlock(failure, onRetry = vm::retryCollection)
            c == null -> LoadingBlock()
            else -> LazyColumn(Modifier.fillMaxSize()) {
                val sub = c.subtitle?.let { pick(it, c.subtitleAr, arabic) }
                if (sub != null) {
                    item { Text(sub, color = M.Muted, fontSize = Type.Secondary, modifier = Modifier.padding(horizontal = Space.Edge)) }
                }
                if (c.kitchens.isEmpty()) item { EmptyBlock(stringResource(R.string.meals_none)) }
                items(c.kitchens, key = { it.branchId }) { k -> KitchenCard(k, arabic) { onKitchen(k.branchId) } }
                item { ae.marketplace.mobile.android.me.BottomPad() }
            }
        }
    }
}
