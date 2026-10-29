package ae.marketplace.mobile.android.meals

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
) {
    LaunchedEffect(location) { viewModel.setLocation(location.lat, location.lng) }
    val stack = remember { mutableStateListOf(Page.Home) }
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

    Box(modifier.fillMaxSize().background(Palette.Ink)) {
        when (page) {
            Page.Home -> HomePage(
                viewModel,
                onKitchen = { viewModel.openKitchen(it); go(Page.Kitchen) },
                onRotation = { requireAccount { viewModel.startRotation(); go(Page.Rotation) } },
                onMine = { requireAccount { viewModel.loadMine(); go(Page.Mine) } },
            )
            Page.Kitchen -> KitchenPage(
                viewModel,
                onBack = ::back,
                onSubscribe = { plan -> requireAccount { viewModel.startCheckout(plan); go(Page.Checkout) } },
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
    onMine: () -> Unit,
) {
    val s by vm.store.collectAsState()
    val arabic = rememberArabic()
    LazyColumn(Modifier.fillMaxSize()) {
        item {
            Column(
                Modifier
                    .fillMaxWidth()
                    .background(Brush.verticalGradient(listOf(Warm.Peach.copy(alpha = 0.22f), Palette.Ink)))
                    .statusBarsPadding()
                    .padding(start = Space.Edge, end = Space.Edge, top = Space.Large, bottom = Space.Medium),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(
                            stringResource(R.string.meals_title),
                            color = Warm.Cream,
                            fontFamily = Serif,
                            fontSize = Type.Title * 1.3f,
                            fontWeight = FontWeight.SemiBold,
                        )
                        Text(stringResource(R.string.meals_subtitle), color = Palette.TextMuted, fontSize = Type.Secondary)
                    }
                    Text(
                        stringResource(R.string.meals_my),
                        color = Palette.Ink,
                        fontSize = Type.Secondary,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier
                            .clickable(onClick = onMine)
                            .background(Warm.Peach, RoundedCornerShape(50))
                            .padding(horizontal = 14.dp, vertical = 10.dp),
                    )
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
                if (s.slot == null) item { RotationPromo(onRotation) }
                s.sections.forEach { section -> item(key = section.id) { Section(section, arabic, onKitchen, onRotation) } }
                item { SectionTitle(stringResource(R.string.meals_near_you)) }
                if (s.kitchens.isEmpty()) item { EmptyBlock(stringResource(R.string.meals_none)) }
                items(s.kitchens, key = { it.branchId }) { KitchenCard(it, arabic) { onKitchen(it.branchId) } }
            }
        }
        item { BottomPad() }
    }
}

@Composable
private fun RotationPromo(onClick: () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = Space.Small)
            .background(Brush.linearGradient(listOf(androidx.compose.ui.graphics.Color(0xFF2B211B), androidx.compose.ui.graphics.Color(0xFF15110F))), RoundedCornerShape(18.dp))
            .border(1.dp, Warm.Saffron.copy(alpha = 0.35f), RoundedCornerShape(18.dp))
            .clickable(onClick = onClick)
            .padding(Space.Large),
        verticalArrangement = Arrangement.spacedBy(Space.Small),
    ) {
        WeekBars(Modifier.fillMaxWidth())
        Text(stringResource(R.string.meals_rotation_title), color = Warm.Cream, fontFamily = Serif, fontSize = Type.Heading * 1.15f, fontWeight = FontWeight.SemiBold)
        Text(stringResource(R.string.meals_rotation_body), color = Palette.TextMuted, fontSize = Type.Secondary)
        Text(stringResource(R.string.meals_rotation_cta) + " →", color = Warm.Saffron, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
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
                        KitchenCover(pick(k.name, k.nameAr, arabic), k.coverImageUrl, k.cuisines, Modifier.fillMaxWidth().height(124.dp))
                        Text(pick(k.name, k.nameAr, arabic), color = Palette.Text, fontSize = Type.Body, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(top = 6.dp))
                        Text(stringResource(R.string.meals_from_meal, money(k.fromMealPriceMinor, k.currency)), color = Warm.Saffron, fontSize = Type.Secondary)
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
                            .background(Warm.Peach, RoundedCornerShape(16.dp))
                            .padding(Space.Large),
                    ) {
                        Text(pick(b.title, b.titleAr, arabic), color = Palette.Ink, fontFamily = Serif, fontSize = Type.Heading, fontWeight = FontWeight.Bold)
                        b.body?.let { Text(pick(it, b.bodyAr, arabic), color = Palette.Ink.copy(alpha = 0.75f), fontSize = Type.Secondary) }
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
    Column(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = Space.Small)
            .background(Warm.Card, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick),
    ) {
        KitchenCover(name, k.coverImageUrl, k.cuisines, Modifier.fillMaxWidth().aspectRatio(2.2f))
        Column(Modifier.padding(Space.Medium), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(name, color = Palette.Text, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
                RatingText(k.rating, k.ratingCount)
            }
            Text(
                (k.cuisines + listOfNotNull(k.city)).joinToString(" · "),
                color = Palette.TextMuted,
                fontSize = Type.Secondary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.meals_from_meal, money(k.fromMealPriceMinor, k.currency)), color = Warm.Saffron, fontSize = Type.Body, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                if (k.foodSafetyVerified) VerifiedBadge()
            }
        }
    }
}

@Composable
private fun RatingText(rating: Double?, count: Int) {
    if (rating == null || count == 0) {
        Text(stringResource(R.string.meals_new), color = Warm.Leaf, fontSize = Type.Caption, fontWeight = FontWeight.SemiBold)
    } else {
        Text(stringResource(R.string.meals_rating, String.format(java.util.Locale.US, "%.1f", rating), count), color = Warm.Saffron, fontSize = Type.Secondary)
    }
}

@Composable
private fun VerifiedBadge() {
    Text(
        "✓ " + stringResource(R.string.meals_verified),
        color = Warm.Leaf,
        fontSize = Type.Caption,
        modifier = Modifier
            .border(1.dp, Warm.Leaf.copy(alpha = 0.5f), RoundedCornerShape(50))
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
    Column(Modifier.fillMaxSize()) {
        TopBar(k?.let { pick(it.name, it.nameAr, arabic) } ?: "", onBack)
        val failure = s.error
        when {
            failure != null -> FailureBlock(failure, onRetry = { vm.openKitchen(s.branchId) })
            k == null -> LoadingBlock()
            else -> LazyColumn(Modifier.fillMaxSize()) {
                item {
                    KitchenCover(pick(k.name, k.nameAr, arabic), k.coverImageUrl, k.cuisines, Modifier.fillMaxWidth().padding(horizontal = Space.Edge).aspectRatio(2f))
                    Column(Modifier.padding(Space.Edge), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(pick(k.name, k.nameAr, arabic), color = Warm.Cream, fontFamily = Serif, fontSize = Type.Title * 1.2f, fontWeight = FontWeight.SemiBold)
                        Row(horizontalArrangement = Arrangement.spacedBy(Space.Small), verticalAlignment = Alignment.CenterVertically) {
                            RatingText(k.rating, k.ratingCount)
                            if (k.foodSafetyVerified) VerifiedBadge()
                        }
                        k.description?.let { Text(pick(it, k.descriptionAr, arabic), color = Palette.TextMuted, fontSize = Type.Body) }
                        if (k.deliversHere == false) {
                            Text(stringResource(R.string.meals_not_here), color = Palette.Bad, fontSize = Type.Secondary)
                        }
                    }
                }
                if (k.plans.isNotEmpty()) {
                    item { SectionTitle(stringResource(R.string.meals_plans)) }
                    items(k.plans, key = { it.id }) { p ->
                        PlanCard(p, arabic, enabled = k.deliversHere != false) { onSubscribe(p) }
                    }
                }
                val byDay = k.menu.slots.groupBy { it.weekday }.toSortedMap()
                if (byDay.isNotEmpty()) {
                    item { SectionTitle(stringResource(R.string.meals_weekly_menu)) }
                    byDay.forEach { (day, slots) ->
                        item(key = "day$day") {
                            Card {
                                Text(weekday(day, arabic, java.time.format.TextStyle.FULL), color = Warm.Peach, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold)
                                slots.forEach { slot ->
                                    Row {
                                        Text(slotLabel(slot.slot), color = Palette.TextFaint, fontSize = Type.Secondary, modifier = Modifier.width(84.dp))
                                        Column(Modifier.weight(1f)) {
                                            slot.default?.let { Text(pick(it.name, it.nameAr, arabic), color = Palette.Text, fontSize = Type.Body) }
                                            slot.alternative?.let {
                                                Text(stringResource(R.string.meals_or, pick(it.name, it.nameAr, arabic)), color = Palette.TextMuted, fontSize = Type.Secondary)
                                            }
                                        }
                                    }
                                }
                            }
                        }
                    }
                }
                if (s.reviews.isNotEmpty()) {
                    item { SectionTitle(stringResource(R.string.meals_reviews)) }
                    items(s.reviews.take(5), key = { it.id }) { r ->
                        Card {
                            Text("★".repeat(r.rating) + "☆".repeat(5 - r.rating), color = Warm.Saffron, fontSize = Type.Secondary)
                            if (r.comment.isNotBlank()) Text(r.comment, color = Palette.Text, fontSize = Type.Body)
                        }
                    }
                }
                item { BottomPad() }
            }
        }
    }
}

@Composable
private fun PlanCard(p: MealPlan, arabic: Boolean, enabled: Boolean, onSubscribe: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = 6.dp)
            .height(IntrinsicSize.Min)
            .background(Warm.Card, RoundedCornerShape(16.dp))
            .border(1.dp, Palette.Line, RoundedCornerShape(16.dp)),
    ) {
        Box(Modifier.width(5.dp).fillMaxHeight().background(Warm.Saffron, RoundedCornerShape(topStart = 16.dp, bottomStart = 16.dp)))
        Column(Modifier.padding(Space.Large), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(pick(p.name, p.nameAr, arabic), color = Palette.Text, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold)
            Text(stringResource(R.string.meals_meal_price, money(p.mealPriceMinor, p.currency)), color = Warm.Cream, fontFamily = Serif, fontSize = Type.Title, fontWeight = FontWeight.SemiBold)
            Text(
                stringResource(R.string.meals_plan_summary, p.mealsPerCycle, p.daysPerWeek) + " · " + stringResource(R.string.meals_cycle_from, money(p.priceMinor, p.currency)),
                color = Palette.TextMuted,
                fontSize = Type.Secondary,
            )
            PrimaryButton(stringResource(R.string.meals_subscribe), onSubscribe, enabled = enabled)
        }
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
                Text(s.kitchen?.let { pick(it.name, it.nameAr, arabic) } ?: "", color = Palette.TextMuted, fontSize = Type.Secondary)
                Text(pick(plan.name, plan.nameAr, arabic), color = Palette.Text, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold)
                Text(stringResource(R.string.meals_meal_price, money(plan.mealPriceMinor, plan.currency)), color = Warm.Saffron, fontSize = Type.Body)
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
                    Text(shortDate(p.startsOn, arabic) + " – " + shortDate(p.endsOn, arabic), color = Palette.TextMuted, fontSize = Type.Secondary)
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
            color = Warm.Saffron,
            fontSize = Type.Body,
            modifier = Modifier.clickable { open = true }.padding(horizontal = Space.Edge, vertical = Space.Medium),
        )
        return
    }
    Card {
        Text(stringResource(R.string.meals_add_address), color = Palette.Text, fontWeight = FontWeight.SemiBold, fontSize = Type.Body)
        Field(label, { label = it }, placeholder = stringResource(R.string.meals_label))
        Field(building, { building = it }, placeholder = stringResource(R.string.meals_building))
        Field(area, { area = it }, placeholder = stringResource(R.string.meals_area))
        Field(city, { city = it }, placeholder = stringResource(R.string.meals_city))
        PrimaryButton(stringResource(R.string.meals_save_address), { onSave(label, building, area, city); open = false }, enabled = !busy)
    }
}

@Composable
private fun Label(text: String) {
    Text(text, color = Palette.TextMuted, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(start = Space.Edge, end = Space.Edge, top = Space.Large, bottom = Space.Small))
}

@Composable
private fun Choice(title: String, detail: String, selected: Boolean, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = 4.dp)
            .background(Warm.Card, RoundedCornerShape(12.dp))
            .border(1.dp, if (selected) Warm.Saffron else Palette.Line, RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(Space.Medium),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(18.dp).border(2.dp, if (selected) Warm.Saffron else Palette.TextFaint, RoundedCornerShape(50)).padding(4.dp)) {
            if (selected) Box(Modifier.fillMaxSize().background(Warm.Saffron, RoundedCornerShape(50)))
        }
        Column(Modifier.padding(start = Space.Medium)) {
            Text(title, color = Palette.Text, fontSize = Type.Body)
            if (detail.isNotBlank()) Text(detail, color = Palette.TextMuted, fontSize = Type.Secondary)
        }
    }
}

@Composable
private fun Field(value: String, onChange: (String) -> Unit, modifier: Modifier = Modifier, placeholder: String = "") {
    BasicTextField(
        value = value,
        onValueChange = onChange,
        singleLine = true,
        textStyle = TextStyle(color = Palette.Text, fontSize = Type.Body),
        cursorBrush = SolidColor(Warm.Saffron),
        modifier = modifier
            .fillMaxWidth()
            .background(Palette.Surface, RoundedCornerShape(10.dp))
            .border(1.dp, Palette.Line, RoundedCornerShape(10.dp))
            .padding(horizontal = Space.Medium, vertical = 14.dp),
        decorationBox = { inner ->
            if (value.isEmpty() && placeholder.isNotEmpty()) Text(placeholder, color = Palette.TextFaint, fontSize = Type.Body)
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
        Text("✓", color = Warm.Leaf, fontSize = Type.Title * 2.5f)
        Text(stringResource(if (redirected) R.string.meals_paying else R.string.meals_paid), textAlign = TextAlign.Center, color = Palette.Text, fontSize = Type.Heading, modifier = Modifier.padding(vertical = Space.Large))
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
            Text(r.rotationNo, color = Palette.Text, fontSize = Type.Body, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
            StatusPill(r.status)
        }
        Text(shortDate(r.startDate, arabic) + " – " + shortDate(r.endDate, arabic), color = Palette.TextMuted, fontSize = Type.Secondary)
        r.weeks.sortedBy { it.week }.forEach { w ->
            Row(
                Modifier.fillMaxWidth().clickable(enabled = w.subscriptionId != null) { w.subscriptionId?.let(onOpen) }.padding(vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Box(Modifier.size(10.dp).background(Warm.Weeks[(w.week - 1).mod(Warm.Weeks.size)], RoundedCornerShape(50)))
                Text(
                    stringResource(R.string.meals_week, w.week) + " · " + pick(w.kitchenName, w.kitchenNameAr, arabic),
                    color = Palette.Text,
                    fontSize = Type.Body,
                    modifier = Modifier.weight(1f).padding(start = Space.Small),
                )
                Text("›", color = Palette.TextFaint, fontSize = Type.Heading)
            }
        }
        if (r.status == "active" || r.status == "pending") {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.meals_auto_renew), color = Palette.Text, fontSize = Type.Body, modifier = Modifier.weight(1f))
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
            .background(Warm.Card, RoundedCornerShape(16.dp))
            .clickable(onClick = onClick)
            .padding(Space.Large),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(pick(s.kitchenName, s.kitchenNameAr, arabic), color = Palette.Text, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold)
            Text(pick(s.planName, s.planNameAr, arabic), color = Palette.TextMuted, fontSize = Type.Secondary)
            s.upcoming.firstOrNull()?.let {
                Text(shortDate(it.date, arabic) + " · " + slotLabel(it.mealSlot), color = Warm.Peach, fontSize = Type.Secondary)
            }
        }
        StatusPill(s.status)
    }
}

@Composable
private fun StatusPill(status: String) {
    val tint = when (status) {
        "active" -> Warm.Leaf
        "pending", "pending_payment" -> Warm.Saffron
        "cancelled" -> Palette.Bad
        else -> Palette.TextMuted
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
        colors = SwitchDefaults.colors(checkedTrackColor = Warm.Saffron, checkedThumbColor = Palette.Ink),
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
                            Text(pick(sub.planName, sub.planNameAr, arabic), color = Palette.Text, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
                            StatusPill(sub.status)
                        }
                        val end = sub.cycleEndsOn ?: sub.endDate
                        Text(shortDate(sub.startDate, arabic) + (end?.let { " – " + shortDate(it, arabic) } ?: ""), color = Palette.TextMuted, fontSize = Type.Secondary)
                        if (sub.rotationId == null && sub.status in setOf("active", "paused")) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(stringResource(R.string.meals_auto_renew), color = Palette.Text, fontSize = Type.Body, modifier = Modifier.weight(1f))
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
        Text(stringResource(R.string.meals_pause_title), color = Palette.Text, fontSize = Type.Body, fontWeight = FontWeight.SemiBold)
        Text(stringResource(R.string.meals_pause_left, left, sub.pauseAllowanceDays), color = Palette.TextMuted, fontSize = Type.Secondary)
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
        containerColor = Warm.Card,
        text = { Text(stringResource(R.string.meals_cancel_confirm), color = Palette.Text, fontSize = Type.Body) },
        confirmButton = {
            TextButton(onClick = onConfirm) { Text(stringResource(R.string.meals_cancel_yes), color = Palette.Bad) }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.meals_keep), color = Palette.Text) }
        },
    )
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun MealCard(m: UpcomingMeal, arabic: Boolean, busy: Boolean, vm: MealsViewModel) {
    Card {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(shortDate(m.date, arabic) + " · " + slotLabel(m.mealSlot), color = Warm.Peach, fontSize = Type.Secondary, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f))
            if (m.locked) {
                Text(stringResource(R.string.meals_locked), color = Palette.TextFaint, fontSize = Type.Caption)
            } else {
                Text(
                    stringResource(R.string.meals_skip),
                    color = Palette.TextMuted,
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
