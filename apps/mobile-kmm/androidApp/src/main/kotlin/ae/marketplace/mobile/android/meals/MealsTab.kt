package ae.marketplace.mobile.android.meals

import androidx.compose.foundation.layout.imePadding
import androidx.compose.ui.semantics.selected
import ae.marketplace.mobile.model.MealTags
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.runtime.rememberCoroutineScope
import kotlinx.coroutines.launch
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.runtime.derivedStateOf
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.Role
import androidx.compose.material3.TextButton
import androidx.compose.material3.AlertDialog
import androidx.compose.ui.text.style.TextAlign
import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.me.BottomPad
import ae.marketplace.mobile.android.ui.EmptyBlock
import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.FailureStrip
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.SecondaryButton
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.android.ui.rememberArabic
import ae.marketplace.mobile.android.ui.words
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.meals.MealsViewModel
import ae.marketplace.mobile.model.DayDish
import ae.marketplace.mobile.model.HomeSection
import ae.marketplace.mobile.model.Kitchen
import ae.marketplace.mobile.model.LatLng
import ae.marketplace.mobile.model.MealPlan
import ae.marketplace.mobile.model.MySubscription
import ae.marketplace.mobile.model.Rotation
import ae.marketplace.mobile.model.UpcomingMeal
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
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
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

private enum class Page { Home, Kitchen, Checkout, Rotation, Paid, Mine, Subscription }

/**
 * The meals tab. Its pages stack inside the tab, so the bottom bar stays put
 * and system back walks back through them.
 *
 * [requireAccount] runs its block once the viewer is signed in, showing the
 * sign-in gate first when they are not.
 */
@Composable
fun MealsTab(
    viewModel: MealsViewModel,
    location: LatLng,
    requireAccount: (() -> Unit) -> Unit,
    modifier: Modifier = Modifier,
    /** The My meals tab: same flow, opened on the customer's subscriptions. */
    startOnMine: Boolean = false,
) {
    val ctx = LocalContext.current
    val prefs = remember { ctx.getSharedPreferences("meals", android.content.Context.MODE_PRIVATE) }
    var place by remember { mutableStateOf(readPlace(prefs)) }
    LaunchedEffect(location, place) {
        val p = place
        if (p != null) viewModel.setLocation(p.lat, p.lng) else viewModel.setLocation(location.lat, location.lng)
    }
    val stack = remember { mutableStateListOf(Page.Home) }
    LaunchedEffect(Unit) {
        if (startOnMine) { viewModel.loadMine(); stack.add(Page.Mine) }
    }
    val page = stack.last()
    fun go(p: Page) { stack.add(p) }
    fun back() { if (stack.size > 1) stack.removeAt(stack.lastIndex) }
    fun home(then: Page? = null) { stack.clear(); stack.add(Page.Home); then?.let { stack.add(it) } }

    BackHandler(enabled = stack.size > 1) { back() }

    val context = LocalContext.current
    // A redirect means the provider has not confirmed yet, so the next page
    // must not say the payment went through.
    var redirected by remember { mutableStateOf(false) }
    val openPayment: (String) -> Unit = { url ->
        redirected = true
        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
        viewModel.paymentOpened()
    }

    Box(modifier.fillMaxSize().background(M.Paper)) {
        when (page) {
            Page.Home -> HomePage(
                viewModel,
                onKitchen = { viewModel.openKitchen(it); go(Page.Kitchen) },
                onRotation = { requireAccount { viewModel.startRotation(); go(Page.Rotation) } },
                place = place,
                onPlace = { place = it; writePlace(prefs, it) },
            )
            Page.Kitchen -> KitchenPage(
                viewModel,
                onBack = ::back,
                onSubscribe = { plan ->
                    requireAccount {
                        viewModel.startCheckout(plan)
                        // The address picked on home leads at checkout.
                        place?.takeIf { it.kind == "address" }?.let { viewModel.selectAddress(it.id) }
                        go(Page.Checkout)
                    }
                },
            )
            Page.Checkout -> CheckoutPage(viewModel, location, onBack = ::back, onPaid = { go(Page.Paid) }, openPayment = openPayment)
            Page.Rotation -> RotationPage(viewModel, onBack = ::back, onPaid = { go(Page.Paid) }, openPayment = openPayment)
            Page.Paid -> PaidPage(redirected, onMine = { viewModel.loadMine(force = true); home(Page.Mine) })
            Page.Mine -> MinePage(viewModel, onBack = ::back, onOpen = { viewModel.openSubscription(it); go(Page.Subscription) })
            Page.Subscription -> SubscriptionPage(viewModel, onBack = ::back, openPayment = openPayment)
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Home
// ─────────────────────────────────────────────────────────────────────────────

@Composable
private fun HomePage(
    vm: MealsViewModel,
    onKitchen: (String) -> Unit,
    onRotation: () -> Unit,
    place: ChosenPlace?,
    onPlace: (ChosenPlace?) -> Unit,
) {
    val s by vm.store.collectAsState()
    val arabic = rememberArabic()
    var picking by remember { mutableStateOf(false) }
    if (picking) {
        PlacePicker(vm, s.addresses, place, arabic, onDismiss = { picking = false }) { onPlace(it); picking = false }
    }
    LazyColumn(Modifier.fillMaxSize().background(M.Blush).statusBarsPadding().background(M.Paper)) {
        item {
            Column(
                Modifier
                    .fillMaxWidth()
                    .background(Brush.verticalGradient(listOf(M.Blush, M.Paper)))
                    .padding(start = Space.Edge, end = Space.Edge, top = Space.Large, bottom = Space.Medium),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            stringResource(R.string.meals_title),
                            color = M.Ink,
                            fontFamily = Serif,
                            fontSize = Type.Title * 1.3f,
                            fontWeight = FontWeight.SemiBold,
                        )
                        Text(stringResource(R.string.meals_subtitle), color = M.Muted, fontSize = Type.Secondary)
                    }
                }
                Row(
                    Modifier.padding(top = Space.Large).horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(Space.Small),
                ) {
                    Chip(stringResource(R.string.meals_slot_all), s.slot == null, { vm.selectSlot(null) })
                    listOf("breakfast", "lunch", "dinner").forEach { slot ->
                        Chip(slotLabel(slot), s.slot == slot, { vm.selectSlot(slot) })
                    }
                }
            }
        }
        val failure = s.error
        when {
            failure != null && s.kitchens.isEmpty() -> item { FailureBlock(failure, onRetry = vm::loadStore) }
            s.loading && s.kitchens.isEmpty() -> item { LoadingBlock() }
            else -> {
                // Kitchens first: the choice is the page. Merchandising follows.
                val listed = s.kitchens.map { it.branchId }.toSet()
                item(key = "near") { NearHeader(s.kitchens.size, place, arabic) { picking = true } }
                if (s.kitchens.isEmpty()) item { EmptyBlock(stringResource(R.string.meals_none)) }
                items(s.kitchens.take(3), key = { it.branchId }) { KitchenCard(it, arabic) { onKitchen(it.branchId) } }
                if (s.slot == null) item(key = "rotation") { RotationPromo(onRotation) }
                items(s.kitchens.drop(3), key = { it.branchId }) { KitchenCard(it, arabic) { onKitchen(it.branchId) } }
                s.sections.filter { it.type == "banner_carousel" }
                    .forEach { section -> item(key = section.id) { Section(section, arabic, onKitchen, onRotation) } }
                if (s.dishes.isNotEmpty()) item(key = "dishes") { DishRail(s.dishes, arabic, onKitchen) }
                // Staff rails skip kitchens the list above already shows.
                s.sections.filter { it.type == "kitchen_rail" }
                    .map { it.copy(kitchens = it.kitchens.orEmpty().filterNot { k -> k.branchId in listed }) }
                    .filter { !it.kitchens.isNullOrEmpty() }
                    .forEach { section -> item(key = section.id) { Section(section, arabic, onKitchen, onRotation) } }
                item(key = "office") { OfficeLink(arabic) }
            }
        }
        item { BottomPad() }
    }
}

/** Where the customer chose to browse kitchens for; kept on the device. */
internal data class ChosenPlace(
    val kind: String,
    val id: String,
    val label: String,
    val labelAr: String?,
    val lat: Double,
    val lng: Double,
) {
    fun name(arabic: Boolean) = if (arabic) labelAr ?: label else label
}

private fun readPlace(p: android.content.SharedPreferences): ChosenPlace? {
    val id = p.getString("place_id", null) ?: return null
    return ChosenPlace(
        p.getString("place_kind", "area") ?: "area",
        id,
        p.getString("place_label", "") ?: "",
        p.getString("place_label_ar", null),
        p.getFloat("place_lat", 0f).toDouble(),
        p.getFloat("place_lng", 0f).toDouble(),
    )
}

private fun writePlace(p: android.content.SharedPreferences, c: ChosenPlace?) {
    p.edit().apply {
        if (c == null) {
            listOf("place_id", "place_kind", "place_label", "place_label_ar", "place_lat", "place_lng").forEach(::remove)
        } else {
            putString("place_id", c.id)
            putString("place_kind", c.kind)
            putString("place_label", c.label)
            putString("place_label_ar", c.labelAr)
            putFloat("place_lat", c.lat.toFloat())
            putFloat("place_lng", c.lng.toFloat())
        }
    }.apply()
}

@Composable
private fun NearHeader(count: Int, place: ChosenPlace?, arabic: Boolean, onChange: () -> Unit) {
    val title = when {
        place != null && count == 0 -> stringResource(R.string.meals_none_near, place.name(arabic))
        place != null -> stringResource(R.string.meals_near_place, count, place.name(arabic))
        else -> stringResource(R.string.meals_near_count, count)
    }
    Row(
        Modifier.fillMaxWidth().padding(start = Space.Edge, end = Space.Small, top = Space.Large, bottom = Space.Small),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, color = M.Ink, fontFamily = Serif, fontSize = Type.Title, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
        Text(
            stringResource(R.string.meals_change),
            color = M.Chilli,
            fontSize = Type.Body,
            fontWeight = FontWeight.SemiBold,
            textDecoration = androidx.compose.ui.text.style.TextDecoration.Underline,
            modifier = Modifier
                .semantics { role = Role.Button }
                .clickable(onClick = onChange)
                .padding(horizontal = 12.dp, vertical = 14.dp),
        )
    }
}

@OptIn(androidx.compose.material3.ExperimentalMaterial3Api::class)
@Composable
private fun PlacePicker(
    vm: MealsViewModel,
    addresses: List<ae.marketplace.mobile.model.Address>,
    current: ChosenPlace?,
    arabic: Boolean,
    onDismiss: () -> Unit,
    onPick: (ChosenPlace?) -> Unit,
) {
    LaunchedEffect(Unit) { vm.loadPlaceAddresses() }
    var q by remember { mutableStateOf("") }
    val areas = ae.marketplace.mobile.model.MealAreas.search(q)
    androidx.compose.material3.ModalBottomSheet(onDismissRequest = onDismiss, containerColor = M.Surface) {
        LazyColumn(
            Modifier.fillMaxWidth().imePadding().padding(horizontal = Space.Edge),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            item { Text(stringResource(R.string.meals_where), color = M.Ink, fontSize = Type.Heading, fontWeight = FontWeight.Bold) }
            // Search sits first so its results stay above the keyboard.
            item { Field(q, { q = it }, Modifier.fillMaxWidth(), placeholder = stringResource(R.string.meals_search_areas)) }
            if (q.isBlank()) item { PickRow(stringResource(R.string.meals_use_location), null, current == null) { onPick(null) } }
            if (q.isBlank() && addresses.isNotEmpty()) {
                item { PickLabel(stringResource(R.string.meals_your_addresses)) }
                items(addresses, key = { "a" + it.id }) { a ->
                    PickRow(a.label, a.line, current?.kind == "address" && current.id == a.id) {
                        onPick(ChosenPlace("address", a.id, a.area?.takeIf { it.isNotBlank() } ?: a.label, null, a.point.lat, a.point.lng))
                    }
                }
            }
            if (q.isBlank()) item { PickLabel(stringResource(R.string.meals_choose_area)) }
            if (areas.isEmpty()) item { Text(stringResource(R.string.meals_no_area), color = M.Muted, fontSize = Type.Secondary) }
            items(areas, key = { "z" + it.id }) { a ->
                PickRow(a.name(arabic), a.city(arabic), current?.kind == "area" && current.id == a.id) {
                    onPick(ChosenPlace("area", a.id, a.en, a.ar, a.lat, a.lng))
                }
            }
            item { Spacer(Modifier.height(32.dp)) }
        }
    }
}

@Composable
private fun PickLabel(text: String) {
    Text(text, color = M.Muted, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(top = Space.Medium))
}

@Composable
private fun PickRow(title: String, detail: String?, selected: Boolean, onClick: () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .semantics { this.selected = selected; role = Role.Button }
            .clickable(onClick = onClick)
            .border(1.dp, if (selected) M.Chilli else androidx.compose.ui.graphics.Color.Transparent, RoundedCornerShape(12.dp))
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Text(title, color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
        if (!detail.isNullOrBlank()) Text(detail, color = M.Muted, fontSize = Type.Secondary)
    }
}

@Composable
private fun OfficeLink(arabic: Boolean) {
    val context = LocalContext.current
    Text(
        stringResource(R.string.meals_office_lunch),
        color = M.Ink,
        fontSize = Type.Body,
        fontWeight = FontWeight.SemiBold,
        textDecoration = androidx.compose.ui.text.style.TextDecoration.Underline,
        modifier = Modifier
            .padding(horizontal = Space.Edge, vertical = Space.Medium)
            .clickable {
                val url = ae.marketplace.mobile.android.BuildConfig.WEB_BASE_URL + (if (arabic) "/ar" else "/en") + "/business"
                context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
            }
            .padding(vertical = 12.dp),
    )
}

@Composable
private fun RotationPromo(onClick: () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = Space.Small)
            .background(Brush.linearGradient(listOf(M.Char, M.ChilliDeep)), RoundedCornerShape(18.dp))
                        .clickable(onClick = onClick)
            .padding(Space.Large),
        verticalArrangement = Arrangement.spacedBy(Space.Small),
    ) {
        WeekBars(Modifier.fillMaxWidth())
        Text(stringResource(R.string.meals_rotation_title), color = M.OnAccent, fontFamily = Serif, fontSize = Type.Heading * 1.15f, fontWeight = FontWeight.ExtraBold)
        Text(stringResource(R.string.meals_rotation_body), color = M.OnAccent.copy(alpha = 0.8f), fontSize = Type.Secondary)
        Text(stringResource(R.string.meals_rotation_cta) + " →", color = M.Saffron, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun Section(section: HomeSection, arabic: Boolean, onKitchen: (String) -> Unit, onRotation: () -> Unit) {
    val title = section.settings.title?.let { pick(it, section.settings.titleAr, arabic) }
        ?: section.collection?.let { pick(it.title, it.titleAr, arabic) }
    when (section.type) {
        "kitchen_rail" -> {
            val kitchens = section.kitchens.orEmpty()
            if (kitchens.isEmpty()) return
            title?.let { SectionTitle(it) }
            LazyRow(
                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = Space.Edge),
                horizontalArrangement = Arrangement.spacedBy(Space.Medium),
            ) {
                items(kitchens, key = { it.branchId }) { k ->
                    Column(Modifier.width(220.dp).clickable { onKitchen(k.branchId) }) {
                        KitchenCover(pick(k.name, k.nameAr, arabic), k.coverImageUrl, k.cuisines.map { MealTags.label(it, arabic) }, Modifier.fillMaxWidth().height(124.dp), fallbackUrl = k.weekDishes.firstOrNull { it.imageUrl != null }?.imageUrl)
                        Text(pick(k.name, k.nameAr, arabic), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
                        Text(stringResource(R.string.meals_from_meal, money(k.fromMealPriceMinor, k.currency)), color = M.Chilli, fontSize = Type.Secondary)
                    }
                }
            }
        }
        "banner_carousel" -> {
            val banners = section.banners.orEmpty()
            if (banners.isEmpty()) return
            LazyRow(
                contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = Space.Edge, vertical = Space.Small),
                horizontalArrangement = Arrangement.spacedBy(Space.Medium),
            ) {
                items(banners, key = { it.id }) { b ->
                    Column(
                        Modifier
                            .width(300.dp)
                            .background(M.Chilli, RoundedCornerShape(16.dp))
                            .padding(Space.Large),
                    ) {
                        Text(pick(b.title, b.titleAr, arabic), color = M.OnAccent, fontFamily = Serif, fontSize = Type.Heading, fontWeight = FontWeight.Bold)
                        b.body?.let { Text(pick(it, b.bodyAr, arabic), color = M.OnAccent.copy(alpha = 0.75f), fontSize = Type.Secondary) }
                    }
                }
            }
        }
        // The rotation card already leads the page; the cuisine strip needs the
        // collection pages, which come with search.
        else -> Unit
    }
}

@Composable
private fun KitchenCard(k: Kitchen, arabic: Boolean, onClick: () -> Unit) {
    val name = pick(k.name, k.nameAr, arabic)
    val dishPhoto = k.weekDishes.firstOrNull { it.imageUrl != null }?.imageUrl
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = Space.Small)
            .background(M.Surface, RoundedCornerShape(20.dp))
            .border(1.dp, M.Line, RoundedCornerShape(20.dp))
            .clickable(onClick = onClick),
    ) {
        Box {
            KitchenCover(name, k.coverImageUrl, k.cuisines.map { MealTags.label(it, arabic) }, Modifier.fillMaxWidth().aspectRatio(1.8f), fallbackUrl = dishPhoto)
            Text(
                stringResource(R.string.meals_from_meal, money(k.fromMealPriceMinor, k.currency)),
                color = M.Ink,
                fontSize = Type.Secondary,
                fontWeight = FontWeight.Bold,
                modifier = Modifier
                    .align(Alignment.BottomStart)
                    .padding(Space.Medium)
                    .background(M.Surface, RoundedCornerShape(50))
                    .padding(horizontal = 12.dp, vertical = 6.dp),
            )
            Row(
                Modifier.align(Alignment.TopStart).padding(Space.Medium),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                if (k.rating != null && k.ratingCount > 0) {
                    Text(
                        "★ " + String.format(java.util.Locale.US, "%.1f", k.rating),
                        color = M.Ink,
                        fontSize = Type.Caption,
                        fontWeight = FontWeight.Bold,
                        modifier = Modifier.background(M.Saffron, RoundedCornerShape(50)).padding(horizontal = 8.dp, vertical = 4.dp),
                    )
                }
                if (k.foodSafetyVerified) {
                    Text(
                        "✓ " + stringResource(R.string.meals_verified),
                        color = M.Leaf,
                        fontSize = Type.Caption,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.background(M.LeafSoft, RoundedCornerShape(50)).padding(horizontal = 8.dp, vertical = 4.dp),
                    )
                }
            }
        }
        Column(Modifier.padding(Space.Medium), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(name, color = M.Ink, fontFamily = Serif, fontSize = Type.Heading, fontWeight = FontWeight.ExtraBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                MealTags.line(k.cuisines + k.dietaryTags, arabic),
                color = M.Muted,
                fontSize = Type.Secondary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (k.weekDishes.isNotEmpty()) {
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                    k.weekDishes.take(4).forEach { d ->
                        KitchenCover(pick(d.name, d.nameAr, arabic), d.imageUrl, emptyList(), Modifier.size(40.dp))
                    }
                    Text(
                        pick(k.weekDishes.first().name, k.weekDishes.first().nameAr, arabic),
                        color = M.Muted,
                        fontSize = Type.Caption,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                }
            }
            if (k.rating == null || k.ratingCount == 0) RatingText(k.rating, k.ratingCount)
        }
    }
}

/** "On the menu tomorrow": dishes first, so customers can browse food, not only kitchens. */
@Composable
private fun DishRail(dishes: List<DayDish>, arabic: Boolean, onKitchen: (String) -> Unit) {
    if (dishes.isEmpty()) return
    SectionTitle(stringResource(R.string.meals_tomorrow))
    LazyRow(
        contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = Space.Edge),
        horizontalArrangement = Arrangement.spacedBy(Space.Medium),
    ) {
        items(dishes, key = { it.branchId + it.slot + it.name }) { d ->
            Column(
                Modifier
                    .width(176.dp)
                    .background(M.Surface, RoundedCornerShape(18.dp))
                    .border(1.dp, M.Line, RoundedCornerShape(18.dp))
                    .clickable { onKitchen(d.branchId) },
            ) {
                Box {
                    KitchenCover(pick(d.name, d.nameAr, arabic), d.imageUrl, emptyList(), Modifier.fillMaxWidth().aspectRatio(1.15f))
                    if (d.hasAlternative) {
                        Text(
                            stringResource(R.string.meals_two_choices),
                            color = M.OnAccent,
                            fontSize = Type.Caption,
                            fontWeight = FontWeight.Bold,
                            modifier = Modifier.align(Alignment.TopStart).padding(8.dp).background(M.Chilli, RoundedCornerShape(50)).padding(horizontal = 8.dp, vertical = 3.dp),
                        )
                    }
                }
                Column(Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(pick(d.name, d.nameAr, arabic), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.Bold, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    Text(pick(d.kitchenName, d.kitchenNameAr, arabic) + " · " + slotLabel(d.slot), color = M.Muted, fontSize = Type.Caption, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(stringResource(R.string.meals_from_meal, money(d.fromMealPriceMinor, d.currency)), color = M.Chilli, fontSize = Type.Secondary, fontWeight = FontWeight.Bold)
                }
            }
        }
    }
}

@Composable
private fun RatingText(rating: Double?, count: Int) {
    if (rating == null || count == 0) {
        Text(stringResource(R.string.meals_new), color = M.Leaf, fontSize = Type.Caption, fontWeight = FontWeight.SemiBold)
    } else {
        Text(stringResource(R.string.meals_rating, String.format(java.util.Locale.US, "%.1f", rating), count), color = M.Chilli, fontSize = Type.Secondary)
    }
}

@Composable
private fun VerifiedBadge() {
    Text(
        "✓ " + stringResource(R.string.meals_verified),
        color = M.Leaf,
        fontSize = Type.Caption,
        modifier = Modifier
            .border(1.dp, M.Leaf.copy(alpha = 0.5f), RoundedCornerShape(50))
            .padding(horizontal = 8.dp, vertical = 3.dp),
    )
}

// ─────────────────────────────────────────────────────────────────────────────
// Kitchen
// ─────────────────────────────────────────────────────────────────────────────

@Composable
private fun KitchenPage(vm: MealsViewModel, onBack: () -> Unit, onSubscribe: (MealPlan) -> Unit) {
    val s by vm.kitchen.collectAsState()
    val arabic = rememberArabic()
    val k = s.kitchen
    val failure = s.error
    when {
        failure != null -> Column(Modifier.fillMaxSize()) {
            TopBar("", onBack)
            FailureBlock(failure, onRetry = { vm.openKitchen(s.branchId) })
        }
        k == null -> Column(Modifier.fillMaxSize()) { TopBar("", onBack); LoadingBlock() }
        else -> KitchenBody(k, s.reviews, arabic, onBack, onSubscribe)
    }
}

@Composable
private fun KitchenBody(
    k: ae.marketplace.mobile.model.KitchenDetail,
    reviews: List<ae.marketplace.mobile.model.KitchenReview>,
    arabic: Boolean,
    onBack: () -> Unit,
    onSubscribe: (MealPlan) -> Unit,
) {
    val name = pick(k.name, k.nameAr, arabic)
    val byDay = remember(k) { k.menu.slots.groupBy { it.weekday }.toSortedMap() }
    var day by remember(k) { mutableStateOf(byDay.keys.firstOrNull()) }
    val list = rememberLazyListState()
    val scope = rememberCoroutineScope()
    // hero, info, then (menu title, tabs, day, week) when there is a menu.
    val plansIndex = 2 + if (byDay.isEmpty()) 0 else 4
    val plansShown by remember {
        derivedStateOf { list.layoutInfo.visibleItemsInfo.any { it.key == "plans" || it.index > plansIndex } }
    }
    val dishPhoto = k.menu.slots.firstNotNullOfOrNull { it.default?.imageUrl ?: it.alternative?.imageUrl }

    Box(Modifier.fillMaxSize()) {
        LazyColumn(Modifier.fillMaxSize(), state = list) {
            item(key = "hero") {
                Box {
                    KitchenCover(name, k.coverImageUrl, k.cuisines.map { MealTags.label(it, arabic) }, Modifier.fillMaxWidth().aspectRatio(1.35f), fallbackUrl = dishPhoto)
                    Text(
                        if (arabic) "→" else "←",
                        color = M.Ink,
                        fontSize = Type.Heading,
                        fontWeight = FontWeight.Bold,
                        textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                        modifier = Modifier
                            .statusBarsPadding()
                            .padding(Space.Medium)
                            .size(Space.Touch)
                            .background(M.Surface, RoundedCornerShape(50))
                            .clickable(onClick = onBack)
                            .semantics { role = Role.Button; contentDescription = "back" }
                            .padding(top = 11.dp),
                    )
                }
            }
            item(key = "info") {
                Column(
                    Modifier
                        .fillMaxWidth()
                        .padding(horizontal = Space.Edge)
                        .padding(top = Space.Large),
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(name, color = M.Ink, fontFamily = Serif, fontSize = Type.Title * 1.25f, fontWeight = FontWeight.ExtraBold)
                    if (k.cuisines.isNotEmpty()) Text(MealTags.line(k.cuisines, arabic), color = M.Muted, fontSize = Type.Secondary)
                    Row(horizontalArrangement = Arrangement.spacedBy(Space.Small), verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            stringResource(R.string.meals_from_meal, money(k.fromMealPriceMinor, k.currency)),
                            color = M.OnAccent,
                            fontSize = Type.Secondary,
                            fontWeight = FontWeight.Bold,
                            modifier = Modifier.background(M.Chilli, RoundedCornerShape(50)).padding(horizontal = 12.dp, vertical = 6.dp),
                        )
                        RatingText(k.rating, k.ratingCount)
                        if (k.foodSafetyVerified) VerifiedBadge()
                    }
                    k.description?.let { Text(pick(it, k.descriptionAr, arabic), color = M.Muted, fontSize = Type.Body) }
                    if (k.deliversHere == false) {
                        Text(stringResource(R.string.meals_not_here), color = Palette.Bad, fontSize = Type.Secondary)
                    }
                }
            }
            if (byDay.isNotEmpty()) {
                item(key = "menuTitle") { SectionTitle(stringResource(R.string.meals_weekly_menu)) }
                item(key = "tabs") {
                    if (k.menu.slots.any { it.alternative != null }) {
                        Text(
                            stringResource(R.string.meals_choice_help),
                            color = M.Muted,
                            fontSize = Type.Secondary,
                            modifier = Modifier.padding(horizontal = Space.Edge).padding(bottom = Space.Small),
                        )
                    }
                    Row(
                        Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = Space.Edge),
                        horizontalArrangement = Arrangement.spacedBy(Space.Small),
                    ) {
                        byDay.keys.forEach { d -> Chip(weekday(d, arabic), day == d, { day = d }) }
                    }
                }
                item(key = "day") {
                    Column(
                        Modifier.padding(horizontal = Space.Edge).padding(top = Space.Medium),
                        verticalArrangement = Arrangement.spacedBy(Space.Medium),
                    ) {
                        byDay[day].orEmpty().forEach { slot -> DishCard(slot, arabic) }
                    }
                }
                item(key = "week") {
                    SectionTitle(stringResource(R.string.meals_week_glance))
                    Column(
                        Modifier
                            .fillMaxWidth()
                            .padding(horizontal = Space.Edge)
                            .background(M.Surface, RoundedCornerShape(18.dp))
                            .border(1.dp, M.Line, RoundedCornerShape(18.dp))
                            .padding(Space.Medium),
                        verticalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        byDay.forEach { (d, slots) ->
                            Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.clickable { day = d }) {
                                Text(weekday(d, arabic), color = M.Chilli, fontSize = Type.Secondary, fontWeight = FontWeight.Bold, modifier = Modifier.width(56.dp))
                                Text(
                                    slots.mapNotNull { it.default?.let { c -> pick(c.name, c.nameAr, arabic) } }.joinToString(" · "),
                                    color = M.Ink,
                                    fontSize = Type.Secondary,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                }
            }
            if (k.plans.isNotEmpty()) {
                item(key = "plans") { SectionTitle(stringResource(R.string.meals_plans)) }
                items(k.plans, key = { it.id }) { p ->
                    PlanCard(p, arabic, popular = p.id == k.popularPlanId, enabled = k.deliversHere != false) { onSubscribe(p) }
                }
            }
            if (reviews.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.meals_reviews)) }
                items(reviews.take(5), key = { it.id }) { r ->
                    Card {
                        Text("★".repeat(r.rating) + "☆".repeat(5 - r.rating), color = M.Saffron, fontSize = Type.Secondary)
                        if (r.comment.isNotBlank()) Text(r.comment, color = M.Ink, fontSize = Type.Body)
                        Text(
                            stringResource(R.string.meals_subscriber) + " · " + shortDate(r.createdAt, arabic),
                            color = M.Muted,
                            fontSize = Type.Caption,
                        )
                    }
                }
            }
            item { Spacer(Modifier.height(96.dp)); BottomPad() }
        }
        if (list.firstVisibleItemIndex > 0) {
            Box(Modifier.fillMaxWidth().background(M.Paper).statusBarsPadding())
        }
        if (k.plans.isNotEmpty() && !plansShown) {
            Row(
                Modifier
                    .align(Alignment.BottomCenter)
                    .fillMaxWidth()
                    .background(M.Surface)
                    .border(1.dp, M.Line)
                    .navigationBarsPadding()
                    .padding(bottom = ae.marketplace.mobile.android.NAV_BAR_HEIGHT)
                    .padding(horizontal = Space.Edge, vertical = Space.Medium),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Space.Medium),
            ) {
                Text(
                    stringResource(R.string.meals_from_meal, money(k.fromMealPriceMinor, k.currency)),
                    color = M.Ink,
                    fontSize = Type.Body,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier.weight(1f),
                )
                PrimaryButton(
                    stringResource(R.string.meals_see_plans),
                    { scope.launch { list.animateScrollToItem(plansIndex) } },
                    modifier = Modifier.width(160.dp),
                )
            }
        }
    }
}

@Composable
private fun DishCard(slot: ae.marketplace.mobile.model.MenuSlot, arabic: Boolean) {
    val main = slot.default ?: slot.alternative ?: return
    Column(
        Modifier
            .fillMaxWidth()
            .background(M.Surface, RoundedCornerShape(18.dp))
            .border(1.dp, M.Line, RoundedCornerShape(18.dp)),
    ) {
        Box {
            KitchenCover(pick(main.name, main.nameAr, arabic), main.imageUrl, emptyList(), Modifier.fillMaxWidth().aspectRatio(2f))
            Text(
                slotLabel(slot.slot),
                color = M.Ink,
                fontSize = Type.Caption,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.align(Alignment.TopStart).padding(10.dp).background(M.SaffronSoft, RoundedCornerShape(50)).padding(horizontal = 10.dp, vertical = 4.dp),
            )
        }
        Column(Modifier.padding(Space.Medium), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(pick(main.name, main.nameAr, arabic), color = M.Ink, fontSize = Type.Heading, fontWeight = FontWeight.Bold)
            val alt = slot.alternative
            if (slot.default != null && alt != null) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
                    KitchenCover(pick(alt.name, alt.nameAr, arabic), alt.imageUrl, emptyList(), Modifier.size(36.dp))
                    Text(stringResource(R.string.meals_or, pick(alt.name, alt.nameAr, arabic)), color = M.Muted, fontSize = Type.Secondary)
                }
            }
        }
    }
}

@Composable
private fun PlanCard(p: MealPlan, arabic: Boolean, popular: Boolean, enabled: Boolean, onSubscribe: () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = 6.dp)
            .background(M.Surface, RoundedCornerShape(18.dp))
            .border(if (popular) 2.dp else 1.dp, if (popular) M.Chilli else M.Line, RoundedCornerShape(18.dp))
            .padding(Space.Large),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        if (popular) {
            Text(
                stringResource(R.string.meals_popular),
                color = M.OnAccent,
                fontSize = Type.Caption,
                fontWeight = FontWeight.Bold,
                modifier = Modifier.background(M.Chilli, RoundedCornerShape(50)).padding(horizontal = 10.dp, vertical = 4.dp),
            )
        }
        Text(pick(p.name, p.nameAr, arabic), color = M.Ink, fontSize = Type.Heading, fontWeight = FontWeight.Bold)
        val slots = p.mealSlots.map { slotLabel(it) }
        Text(stringResource(R.string.meals_meal_price, money(p.mealPriceMinor, p.currency)), color = M.Ink, fontFamily = Serif, fontSize = Type.Title, fontWeight = FontWeight.ExtraBold)
        Text(stringResource(R.string.meals_cycle_from, money(p.priceMinor, p.currency)), color = M.Muted, fontSize = Type.Secondary)
        Text(
            stringResource(R.string.meals_plan_summary, p.mealsPerCycle, p.daysPerWeek) +
                slots.joinToString(prefix = " · ", separator = ", ").takeIf { slots.isNotEmpty() }.orEmpty(),
            color = M.Muted,
            fontSize = Type.Secondary,
        )
        PrimaryButton(stringResource(R.string.meals_subscribe), onSubscribe, enabled = enabled)
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Checkout
// ─────────────────────────────────────────────────────────────────────────────

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CheckoutPage(
    vm: MealsViewModel,
    location: LatLng,
    onBack: () -> Unit,
    onPaid: () -> Unit,
    openPayment: (String) -> Unit,
) {
    val s by vm.checkout.collectAsState()
    val arabic = rememberArabic()
    val plan = s.plan ?: return
    LaunchedEffect(s.paymentUrl, s.subscriptionId) {
        s.paymentUrl?.let(openPayment)
        if (s.subscriptionId != null) onPaid()
    }
    Column(Modifier.fillMaxSize()) {
        TopBar(stringResource(R.string.meals_checkout), onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            Card {
                Text(s.kitchen?.let { pick(it.name, it.nameAr, arabic) } ?: "", color = M.Muted, fontSize = Type.Secondary)
                Text(pick(plan.name, plan.nameAr, arabic), color = M.Ink, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold)
                Text(stringResource(R.string.meals_meal_price, money(plan.mealPriceMinor, plan.currency)), color = M.Chilli, fontSize = Type.Body)
            }
            Label(stringResource(R.string.meals_address))
            s.addresses.forEach { a ->
                Choice(a.label, a.line, a.id == s.addressId) { vm.selectAddress(a.id) }
            }
            AddressForm(s.addresses.isEmpty(), s.busy) { label, building, area, city ->
                vm.addAddress(label, building, area, city, location)
            }
            Label(stringResource(R.string.meals_days, plan.daysPerWeek))
            FlowRow(Modifier.padding(horizontal = Space.Edge), horizontalArrangement = Arrangement.spacedBy(Space.Small), verticalArrangement = Arrangement.spacedBy(Space.Small)) {
                plan.deliveryWeekdays.sorted().forEach { d -> Chip(weekday(d, arabic), d in s.weekdays, { vm.toggleWeekday(d) }) }
            }
            Label(stringResource(R.string.meals_meals, plan.mealsPerDay))
            FlowRow(Modifier.padding(horizontal = Space.Edge), horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
                plan.mealSlots.forEach { slot -> Chip(slotLabel(slot), slot in s.slots, { vm.toggleSlot(slot) }) }
            }
            val windows = s.kitchen?.deliveryWindows.orEmpty()
            if (windows.isNotEmpty()) {
                Label(stringResource(R.string.meals_window))
                FlowRow(Modifier.padding(horizontal = Space.Edge), horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
                    windows.forEach { w -> Chip(w.label, w.id == s.windowId, { vm.selectWindow(w.id) }) }
                }
            }
            Label(stringResource(R.string.meals_start))
            Field(s.startDate, vm::setStartDate, Modifier.padding(horizontal = Space.Edge))
            Label(stringResource(R.string.meals_promo))
            Row(Modifier.padding(horizontal = Space.Edge), verticalAlignment = Alignment.CenterVertically) {
                Field(s.promoCode, vm::setPromo, Modifier.weight(1f))
                Spacer(Modifier.width(Space.Small))
                SecondaryButton(stringResource(R.string.meals_apply), vm::applyPromo)
            }
            val promoCode = (s.error as? Failure.Rejected)?.fieldCodes?.get("promo_code")
            promoCode?.let {
                Text(
                    stringResource(promoReason(it)),
                    color = Palette.Bad,
                    fontSize = Type.Secondary,
                    modifier = Modifier.padding(horizontal = Space.Edge, vertical = 6.dp),
                )
            }
            s.preview?.let { p ->
                Spacer(Modifier.height(Space.Medium))
                Card {
                    Text(shortDate(p.startsOn, arabic) + " – " + shortDate(p.endsOn, arabic), color = M.Muted, fontSize = Type.Secondary)
                    Line(stringResource(R.string.meals_subtotal), money(p.subtotalMinor, p.currency))
                    if (p.discountMinor > 0) Line(stringResource(R.string.meals_discount), "−" + money(p.discountMinor, p.currency))
                    Line(stringResource(R.string.meals_delivery), money(p.deliveryMinor, p.currency))
                    Line(stringResource(R.string.meals_total), money(p.totalMinor, p.currency), strong = true)
                }
            }
            if (promoCode == null) s.error?.let { FailureStrip(it, onRetry = null) }
            Spacer(Modifier.height(Space.XL))
        }
        Box(Modifier.padding(Space.Edge)) {
            PrimaryButton(
                stringResource(R.string.meals_pay, s.preview?.let { money(it.totalMinor, it.currency) } ?: ""),
                vm::subscribe,
                enabled = s.preview != null && !s.busy,
            )
        }
        BottomPad()
    }
}

@Composable
private fun AddressForm(startOpen: Boolean, busy: Boolean, onSave: (String, String, String, String) -> Unit) {
    var open by rememberSaveable { mutableStateOf(startOpen) }
    var label by rememberSaveable { mutableStateOf("") }
    var building by rememberSaveable { mutableStateOf("") }
    var area by rememberSaveable { mutableStateOf("") }
    var city by rememberSaveable { mutableStateOf("Dubai") }
    if (!open && !startOpen) {
        Text(
            "+ " + stringResource(R.string.meals_add_address),
            color = M.Chilli,
            fontSize = Type.Body,
            modifier = Modifier.clickable { open = true }.padding(horizontal = Space.Edge, vertical = Space.Medium),
        )
        return
    }
    Card {
        Text(stringResource(R.string.meals_add_address), color = M.Ink, fontWeight = FontWeight.SemiBold, fontSize = Type.Body)
        Field(label, { label = it }, placeholder = stringResource(R.string.meals_label))
        Field(building, { building = it }, placeholder = stringResource(R.string.meals_building))
        Field(area, { area = it }, placeholder = stringResource(R.string.meals_area))
        Field(city, { city = it }, placeholder = stringResource(R.string.meals_city))
        PrimaryButton(stringResource(R.string.meals_save_address), { onSave(label, building, area, city); open = false }, enabled = !busy)
    }
}

@Composable
private fun Label(text: String) {
    Text(text, color = M.Muted, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(start = Space.Edge, end = Space.Edge, top = Space.Large, bottom = Space.Small))
}

@Composable
private fun Choice(title: String, detail: String, selected: Boolean, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = 4.dp)
            .background(M.Surface, RoundedCornerShape(12.dp))
            .border(1.dp, if (selected) M.Chilli else M.Line, RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(Space.Medium),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(18.dp).border(2.dp, if (selected) M.Chilli else M.Faint, RoundedCornerShape(50)).padding(4.dp)) {
            if (selected) Box(Modifier.fillMaxSize().background(M.Chilli, RoundedCornerShape(50)))
        }
        Column(Modifier.padding(start = Space.Medium)) {
            Text(title, color = M.Ink, fontSize = Type.Body)
            if (detail.isNotBlank()) Text(detail, color = M.Muted, fontSize = Type.Secondary)
        }
    }
}

@Composable
private fun Field(value: String, onChange: (String) -> Unit, modifier: Modifier = Modifier, placeholder: String = "") {
    BasicTextField(
        value = value,
        onValueChange = onChange,
        singleLine = true,
        textStyle = TextStyle(color = M.Ink, fontSize = Type.Body),
        cursorBrush = SolidColor(M.Chilli),
        modifier = modifier
            .fillMaxWidth()
            .background(M.Surface, RoundedCornerShape(10.dp))
            .border(1.dp, M.Line, RoundedCornerShape(10.dp))
            .padding(horizontal = Space.Medium, vertical = 14.dp),
        decorationBox = { inner ->
            if (value.isEmpty() && placeholder.isNotEmpty()) Text(placeholder, color = M.Faint, fontSize = Type.Body)
            inner()
        },
    )
}

// ─────────────────────────────────────────────────────────────────────────────
// Rotation
// ─────────────────────────────────────────────────────────────────────────────

@Composable
private fun RotationPage(vm: MealsViewModel, onBack: () -> Unit, onPaid: () -> Unit, openPayment: (String) -> Unit) {
    val s by vm.rotation.collectAsState()
    val arabic = rememberArabic()
    LaunchedEffect(s.paymentUrl, s.rotationId) {
        s.paymentUrl?.let(openPayment)
        if (s.rotationId != null) onPaid()
    }
    Column(Modifier.fillMaxSize()) {
        TopBar(stringResource(R.string.meals_rotation), onBack)
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            Label(stringResource(R.string.meals_address))
            s.addresses.forEach { a -> Choice(a.label, a.line, a.id == s.addressId) { vm.selectRotationAddress(a.id) } }
            Label(stringResource(R.string.meals_pick_four, s.picked.size))
            if (s.busy && s.candidates.isEmpty()) LoadingBlock(inline = true)
            s.candidates.forEach { k ->
                val price = s.plans[k.branchId]?.rotationWeekPriceMinor ?: k.rotationWeekPriceMinor ?: 0
                Choice(
                    pick(k.name, k.nameAr, arabic),
                    money(price, k.currency) + " / " + stringResource(R.string.meals_week, 1).substringBefore(' ').lowercase(),
                    k.branchId in s.picked,
                ) { vm.togglePick(k.branchId) }
            }
            s.quote?.let { q ->
                Spacer(Modifier.height(Space.Medium))
                Card {
                    WeekBars(Modifier.fillMaxWidth())
                    q.weeks.sortedBy { it.week }.forEach { w ->
                        Line(
                            stringResource(R.string.meals_week, w.week) + " · " + pick(w.kitchenName, w.kitchenNameAr, arabic),
                            money(w.priceMinor, q.currency),
                        )
                    }
                    if (q.discountMinor > 0) Line(stringResource(R.string.meals_discount), "−" + money(q.discountMinor, q.currency))
                    Line(stringResource(R.string.meals_total), money(q.amountMinor, q.currency), strong = true)
                }
            }
            s.error?.let { FailureStrip(it, onRetry = null) }
        }
        Box(Modifier.padding(Space.Edge)) {
            PrimaryButton(
                stringResource(R.string.meals_pay, s.quote?.let { money(it.amountMinor, it.currency) } ?: ""),
                vm::payRotation,
                enabled = s.quote != null && !s.busy,
            )
        }
        BottomPad()
    }
}

@Composable
private fun PaidPage(redirected: Boolean, onMine: () -> Unit) {
    Column(
        Modifier.fillMaxSize().padding(Space.XL),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text("✓", color = M.Leaf, fontSize = Type.Title * 2.5f)
        Text(stringResource(if (redirected) R.string.meals_paying else R.string.meals_paid), textAlign = TextAlign.Center, color = M.Ink, fontSize = Type.Heading, modifier = Modifier.padding(vertical = Space.Large))
        PrimaryButton(stringResource(R.string.meals_view_subs), onMine)
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// My meals
// ─────────────────────────────────────────────────────────────────────────────

@Composable
private fun MinePage(vm: MealsViewModel, onBack: () -> Unit, onOpen: (String) -> Unit) {
    val s by vm.mine.collectAsState()
    val arabic = rememberArabic()
    LaunchedEffect(Unit) { vm.loadMine() }
    Column(Modifier.fillMaxSize()) {
        TopBar(stringResource(R.string.meals_my), onBack)
        val failure = s.error
        when {
            failure != null && !s.loaded -> FailureBlock(failure, onRetry = { vm.loadMine(force = true) })
            !s.loaded -> LoadingBlock()
            s.subscriptions.isEmpty() && s.rotations.isEmpty() -> EmptyBlock(stringResource(R.string.meals_no_subs))
            else -> LazyColumn(Modifier.fillMaxSize()) {
                if (s.rotations.isNotEmpty()) {
                    item { SectionTitle(stringResource(R.string.meals_rotations)) }
                    items(s.rotations, key = { it.id }) { r -> RotationCard(r, arabic, vm, onOpen) }
                }
                if (s.subscriptions.isNotEmpty()) {
                    item { SectionTitle(stringResource(R.string.meals_subscriptions)) }
                    items(s.subscriptions, key = { it.id }) { sub -> SubscriptionRow(sub, arabic) { onOpen(sub.id) } }
                }
                item { BottomPad() }
            }
        }
    }
}

@Composable
private fun RotationCard(r: Rotation, arabic: Boolean, vm: MealsViewModel, onOpen: (String) -> Unit) {
    var confirming by remember { mutableStateOf(false) }
    Card {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(r.rotationNo, color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
            StatusPill(r.status)
        }
        Text(shortDate(r.startDate, arabic) + " – " + shortDate(r.endDate, arabic), color = M.Muted, fontSize = Type.Secondary)
        r.weeks.sortedBy { it.week }.forEach { w ->
            Row(
                Modifier.fillMaxWidth().clickable(enabled = w.subscriptionId != null) { w.subscriptionId?.let(onOpen) }.padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(Modifier.size(10.dp).background(Warm.Weeks[(w.week - 1).mod(Warm.Weeks.size)], RoundedCornerShape(50)))
                Text(
                    stringResource(R.string.meals_week, w.week) + " · " + pick(w.kitchenName, w.kitchenNameAr, arabic),
                    color = M.Ink,
                    fontSize = Type.Body,
                    modifier = Modifier.weight(1f).padding(start = Space.Small),
                )
                Text("›", color = M.Faint, fontSize = Type.Heading)
            }
        }
        if (r.status == "active" || r.status == "pending") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.meals_auto_renew), color = M.Ink, fontSize = Type.Body, modifier = Modifier.weight(1f))
                RenewSwitch(r.autoRenew) { vm.setRotationRenewal(r.id, it) }
            }
            Text(
                stringResource(R.string.meals_cancel_rotation),
                color = Palette.Bad,
                fontSize = Type.Secondary,
                modifier = Modifier.clickable { confirming = true }.padding(vertical = Space.Small),
            )
            if (confirming) ConfirmCancel(onConfirm = { confirming = false; vm.cancelRotation(r.id) }, onDismiss = { confirming = false })
        }
    }
}

@Composable
private fun SubscriptionRow(s: MySubscription, arabic: Boolean, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = 6.dp)
            .background(M.Surface, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .padding(Space.Large),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(pick(s.kitchenName, s.kitchenNameAr, arabic), color = M.Ink, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold)
            Text(pick(s.planName, s.planNameAr, arabic), color = M.Muted, fontSize = Type.Secondary)
            s.upcoming.firstOrNull()?.let {
                Text(shortDate(it.date, arabic) + " · " + slotLabel(it.mealSlot), color = M.Chilli, fontSize = Type.Secondary)
            }
        }
        StatusPill(s.status)
    }
}

@Composable
private fun StatusPill(status: String) {
    val tint = when (status) {
        "active" -> M.Leaf
        "pending", "pending_payment" -> M.Chilli
        "cancelled" -> Palette.Bad
        else -> M.Muted
    }
    Text(
        statusLabel(status),
        color = tint,
        fontSize = Type.Caption,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.border(1.dp, tint.copy(alpha = 0.5f), RoundedCornerShape(50)).padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

@Composable
private fun RenewSwitch(checked: Boolean, onChange: (Boolean) -> Unit) {
    Switch(
        checked = checked,
        onCheckedChange = onChange,
        colors = SwitchDefaults.colors(checkedTrackColor = M.Chilli, checkedThumbColor = M.OnAccent),
    )
}

@Composable
private fun SubscriptionPage(vm: MealsViewModel, onBack: () -> Unit, openPayment: (String) -> Unit) {
    val s by vm.sub.collectAsState()
    val arabic = rememberArabic()
    LaunchedEffect(s.paymentUrl) { s.paymentUrl?.let(openPayment) }
    val sub = s.subscription
    Column(Modifier.fillMaxSize()) {
        TopBar(sub?.let { pick(it.kitchenName, it.kitchenNameAr, arabic) } ?: "", onBack)
        val failure = s.error
        when {
            sub == null && failure != null -> FailureBlock(failure, onRetry = null)
            sub == null -> LoadingBlock()
            else -> LazyColumn(Modifier.fillMaxSize()) {
                item {
                    Card {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(pick(sub.planName, sub.planNameAr, arabic), color = M.Ink, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                            StatusPill(sub.status)
                        }
                        val end = sub.cycleEndsOn ?: sub.endDate
                        Text(shortDate(sub.startDate, arabic) + (end?.let { " – " + shortDate(it, arabic) } ?: ""), color = M.Muted, fontSize = Type.Secondary)
                        if (sub.rotationId == null && sub.status in setOf("active", "paused")) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(stringResource(R.string.meals_auto_renew), color = M.Ink, fontSize = Type.Body, modifier = Modifier.weight(1f))
                                RenewSwitch(sub.renewalMode == "auto") { vm.setAutoRenew(it) }
                            }
                        }
                        if (sub.pendingOrderId != null) PrimaryButton(stringResource(R.string.meals_pay_due), vm::payPending, enabled = !s.busy)
                        if (sub.canRenew) PrimaryButton(stringResource(R.string.meals_renew), vm::renew, enabled = !s.busy)
                    }
                    s.error?.let { FailureStrip(it, onRetry = null) }
                }
                if (sub.upcoming.isNotEmpty()) {
                    item { SectionTitle(stringResource(R.string.meals_upcoming)) }
                    items(sub.upcoming, key = { it.date + it.mealSlot }) { m -> MealCard(m, arabic, s.busy, vm) }
                }
                if (sub.rotationId == null && sub.status == "active" && sub.pauseAllowanceDays > 0) {
                    item { PauseCard(sub, s.busy, vm) }
                }
                if (sub.rotationId == null && sub.status in setOf("active", "paused", "pending")) {
                    item {
                        var confirming by remember { mutableStateOf(false) }
                        Text(
                            stringResource(R.string.meals_cancel),
                            color = Palette.Bad,
                            fontSize = Type.Secondary,
                            modifier = Modifier.clickable(enabled = !s.busy) { confirming = true }.padding(Space.Edge),
                        )
                        if (confirming) ConfirmCancel(onConfirm = { confirming = false; vm.cancel() }, onDismiss = { confirming = false })
                    }
                }
                item { BottomPad() }
            }
        }
    }
}

@Composable
private fun PauseCard(sub: MySubscription, busy: Boolean, vm: MealsViewModel) {
    val left = (sub.pauseAllowanceDays - sub.pauseDaysUsed).coerceAtLeast(0)
    val first = sub.upcoming.firstOrNull { !it.locked }?.date ?: return
    var from by remember(first) { mutableStateOf(first) }
    var to by remember(first) { mutableStateOf(first) }
    Card {
        Text(stringResource(R.string.meals_pause_title), color = M.Ink, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
        Text(stringResource(R.string.meals_pause_left, left, sub.pauseAllowanceDays), color = M.Muted, fontSize = Type.Secondary)
        Row(horizontalArrangement = Arrangement.spacedBy(Space.Small)) {
            Field(from, { from = it }, Modifier.weight(1f), placeholder = stringResource(R.string.meals_pause_from))
            Field(to, { to = it }, Modifier.weight(1f), placeholder = stringResource(R.string.meals_pause_to))
        }
        PrimaryButton(stringResource(R.string.meals_pause), { vm.pause(from.trim(), to.trim()) }, enabled = !busy && left > 0)
    }
}

@Composable
private fun ConfirmCancel(onConfirm: () -> Unit, onDismiss: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = M.Surface,
        text = { Text(stringResource(R.string.meals_cancel_confirm), color = M.Ink, fontSize = Type.Body) },
        confirmButton = {
            TextButton(onClick = onConfirm) { Text(stringResource(R.string.meals_cancel_yes), color = Palette.Bad) }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.meals_keep), color = M.Ink) }
        },
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun MealCard(m: UpcomingMeal, arabic: Boolean, busy: Boolean, vm: MealsViewModel) {
    Card {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(shortDate(m.date, arabic) + " · " + slotLabel(m.mealSlot), color = M.Chilli, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
            if (m.locked) {
                Text(stringResource(R.string.meals_locked), color = M.Faint, fontSize = Type.Caption)
            } else {
                Text(
                    stringResource(R.string.meals_skip),
                    color = M.Muted,
                    fontSize = Type.Secondary,
                    modifier = Modifier.clickable(enabled = !busy) { vm.skip(m.date, m.mealSlot) }.padding(Space.Small),
                )
            }
        }
        val chosen = m.chosenDishId ?: m.options.firstOrNull { it.isDefault }?.id
        FlowRow(horizontalArrangement = Arrangement.spacedBy(Space.Small), verticalArrangement = Arrangement.spacedBy(Space.Small)) {
            m.options.forEach { o ->
                Chip(pick(o.name, o.nameAr, arabic), o.id == chosen, { vm.choose(m.date, m.mealSlot, o.id) }, enabled = !m.locked && !busy)
            }
        }
    }
}

private fun promoReason(code: String): Int = when (code) {
    "unknown" -> R.string.meals_promo_unknown
    "not_live" -> R.string.meals_promo_not_live
    "wrong_kitchen", "wrong_product" -> R.string.meals_promo_wrong_kitchen
    "new_customers_only" -> R.string.meals_promo_new_only
    "already_used" -> R.string.meals_promo_used
    "exhausted" -> R.string.meals_promo_exhausted
    else -> R.string.meals_promo_invalid
}
