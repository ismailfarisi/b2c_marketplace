import SwiftUI
import Shared

// Light food-led storefront palette, same values as the Android meals tab (M).
enum Warm {
    static let paper = Color(red: 1.0, green: 0.973, blue: 0.941)
    static let ink = Color(red: 0.118, green: 0.078, blue: 0.063)
    static let muted = Color(red: 0.463, green: 0.404, blue: 0.357)
    static let faint = Color(red: 0.659, green: 0.596, blue: 0.537)
    static let line = Color(red: 0.941, green: 0.886, blue: 0.824)
    static let chilli = Color(red: 0.769, green: 0.231, blue: 0.086)
    static let chilliDeep = Color(red: 0.659, green: 0.188, blue: 0.059)
    static let saffron = Color(red: 0.965, green: 0.698, blue: 0.106)
    static let saffronSoft = Color(red: 1.0, green: 0.945, blue: 0.8)
    static let leaf = Color(red: 0.153, green: 0.424, blue: 0.267)
    static let leafSoft = Color(red: 0.890, green: 0.949, blue: 0.910)
    static let blush = Color(red: 0.992, green: 0.902, blue: 0.847)
    static let char = Color(red: 0.133, green: 0.086, blue: 0.059)
    static let card = Color.white
    static let covers: [Color] = [
        Color(red: 0.549, green: 0.290, blue: 0.184), Color(red: 0.247, green: 0.420, blue: 0.310),
        Color(red: 0.478, green: 0.361, blue: 0.180), Color(red: 0.243, green: 0.353, blue: 0.478),
        Color(red: 0.478, green: 0.243, blue: 0.353), Color(red: 0.353, green: 0.420, blue: 0.180),
    ]
    static let weeks: [Color] = [
        Color(red: 0.898, green: 0.604, blue: 0.231), Color(red: 0.420, green: 0.686, blue: 0.431),
        Color(red: 0.357, green: 0.608, blue: 0.835), Color(red: 0.769, green: 0.420, blue: 0.549),
    ]
}

/// The meals pages are light; the app-wide Palette is the dark video one.
private enum MP {
    static let surface = Color.white
    static let line = Warm.line
    static let bad = Color(red: 0.749, green: 0.161, blue: 0.129)
    static let text = Warm.ink
    static let textMuted = Warm.muted
    static let textFaint = Warm.faint
}

private func display(_ size: CGFloat) -> Font { .system(size: size, weight: .bold) }

private func tr(_ key: String, _ args: CVarArg...) -> String {
    String(format: NSLocalizedString(key, comment: ""), arguments: args)
}

private func pick(_ en: String, _ ar: String?) -> String { localised(en, ar ?? "") }

private func money(_ minor: Int64, _ currency: String) -> String {
    let f = NumberFormatter()
    f.numberStyle = .decimal
    f.locale = Locale(identifier: "en_US_POSIX")
    f.groupingSeparator = ","
    f.usesGroupingSeparator = true
    f.minimumFractionDigits = 2
    f.maximumFractionDigits = 2
    var s = f.string(from: NSNumber(value: Double(minor) / 100)) ?? "\(minor)"
    // Whole amounts drop their zero decimals (AED 60, not AED 60.00), as on the web.
    if s.hasSuffix(".00") { s.removeLast(3) }
    return currency + " " + s
}

private func tagLine(_ tags: [String]) -> String { MealTags.shared.line(tags: tags, arabic: isArabicLocale) }
private func tagLabel(_ tag: String) -> String { MealTags.shared.label(tag: tag, arabic: isArabicLocale) }

/// Where the customer chose to browse kitchens for; kept on the device.
private struct ChosenPlace: Codable, Equatable {
    let kind: String
    let id: String
    let label: String
    let labelAr: String?
    let lat: Double
    let lng: Double

    var name: String { isArabicLocale ? (labelAr ?? label) : label }

    static func decode(_ raw: String) -> ChosenPlace? {
        raw.data(using: .utf8).flatMap { try? JSONDecoder().decode(ChosenPlace.self, from: $0) }
    }

    var encoded: String {
        (try? JSONEncoder().encode(self)).flatMap { String(data: $0, encoding: .utf8) } ?? ""
    }
}

private var mealsLocale: Locale { Locale(identifier: isArabicLocale ? "ar" : "en") }

/// The API counts Sunday as 0, which is also where `weekdaySymbols` starts.
private func weekday(_ day: Int32, full: Bool = false) -> String {
    let f = DateFormatter()
    f.locale = mealsLocale
    let symbols = full ? f.weekdaySymbols ?? [] : f.shortWeekdaySymbols ?? []
    let i = Int(day)
    return symbols.indices.contains(i) ? symbols[i] : "\(day)"
}

private func shortDate(_ iso: String) -> String {
    let parse = DateFormatter()
    parse.locale = Locale(identifier: "en_US_POSIX")
    parse.dateFormat = "yyyy-MM-dd"
    guard let date = parse.date(from: String(iso.prefix(10))) else { return iso }
    let out = DateFormatter()
    out.locale = mealsLocale
    out.dateFormat = "EEE d MMM"
    return out.string(from: date)
}

private func slotLabel(_ slot: String) -> String {
    switch slot {
    case "breakfast", "lunch", "dinner": return NSLocalizedString("meals_slot_" + slot, comment: "")
    default: return slot
    }
}

private func statusLabel(_ status: String) -> String {
    switch status {
    case "active": return NSLocalizedString("meals_status_active", comment: "")
    case "pending", "pending_payment": return NSLocalizedString("meals_status_pending", comment: "")
    case "paused": return NSLocalizedString("meals_status_paused", comment: "")
    case "cancelled": return NSLocalizedString("meals_status_cancelled", comment: "")
    case "ended", "completed", "expired": return NSLocalizedString("meals_status_ended", comment: "")
    default: return status
    }
}

private func promoReason(_ code: String) -> String {
    let key: String
    switch code {
    case "unknown": key = "meals_promo_unknown"
    case "not_live": key = "meals_promo_not_live"
    case "wrong_kitchen", "wrong_product": key = "meals_promo_wrong_kitchen"
    case "new_customers_only": key = "meals_promo_new_only"
    case "already_used": key = "meals_promo_used"
    case "exhausted": key = "meals_promo_exhausted"
    default: key = "meals_promo_invalid"
    }
    return NSLocalizedString(key, comment: "")
}

private enum MealsPage {
    case home, kitchen, checkout, rotation, paid, mine, subscription
}

/// The meals tab: storefront, kitchen, checkout, rotation and My meals, each
/// reading one StateFlow of the shared MealsViewModel.
struct MealsScreen: View {
    let viewModel: MealsViewModel
    let lat: Double
    let lng: Double
    /// Runs the action now when signed in, otherwise after sign-in.
    let requireAccount: (@escaping () -> Void) -> Void
    /// The My meals tab: same flow, opened on the customer's subscriptions.
    var startOnMine = false

    @State private var stack: [MealsPage] = [.home]
    @AppStorage("meals.place") private var placeRaw = ""
    private var place: ChosenPlace? { ChosenPlace.decode(placeRaw) }
    @State private var redirected = false
    @Environment(\.openURL) private var openURL

    private func go(_ p: MealsPage) { stack.append(p) }
    private func back() { if stack.count > 1 { stack.removeLast() } }

    private func openPayment(_ url: String) {
        redirected = true
        if let u = URL(string: url) { openURL(u) }
        viewModel.paymentOpened()
    }

    var body: some View {
        ZStack {
            Warm.paper.ignoresSafeArea()
            switch stack.last ?? .home {
            case .home:
                MealsHomePage(
                    vm: viewModel,
                    onKitchen: { id in viewModel.openKitchen(branchId: id); go(.kitchen) },
                    onRotation: { requireAccount { viewModel.startRotation(); go(.rotation) } },
                    onMine: { requireAccount { viewModel.loadMine(force: false); go(.mine) } },
                    place: place,
                    onPlace: { placeRaw = $0?.encoded ?? "" }
                )
            case .kitchen:
                KitchenPage(vm: viewModel, onBack: back) { plan in
                    requireAccount {
                        viewModel.startCheckout(plan: plan)
                        // The address picked on home leads at checkout.
                        if let p = place, p.kind == "address" { viewModel.selectAddress(id: p.id) }
                        go(.checkout)
                    }
                }
            case .checkout:
                CheckoutPage(vm: viewModel, lat: lat, lng: lng, onBack: back,
                             onPaid: { go(.paid) }, openPayment: openPayment)
            case .rotation:
                RotationPage(vm: viewModel, onBack: back, onPaid: { go(.paid) }, openPayment: openPayment)
            case .paid:
                PaidPage(redirected: redirected) {
                    viewModel.loadMine(force: true)
                    stack = [.home, .mine]
                }
            case .mine:
                MinePage(vm: viewModel, onBack: back) { id in
                    viewModel.openSubscription(id: id)
                    go(.subscription)
                }
            case .subscription:
                SubscriptionPage(vm: viewModel, onBack: back, openPayment: openPayment)
            }
        }
        .environment(\.colorScheme, .light)
        .task(id: "\(lat),\(lng),\(placeRaw)") {
            viewModel.setLocation(lat: place?.lat ?? lat, lng: place?.lng ?? lng)
        }
        .onAppear {
            if startOnMine && stack.count == 1 {
                viewModel.loadMine(force: false)
                stack = [.home, .mine]
            }
        }
    }
}

// MARK: - Parts

private struct TopBar: View {
    let title: String
    let onBack: () -> Void

    var body: some View {
        HStack(spacing: Space.small) {
            Button(action: onBack) {
                Image(systemName: "chevron.backward")
                    .font(.system(size: 18, weight: .semibold))
                    .foregroundStyle(MP.text)
                    .frame(minWidth: Space.touch, minHeight: Space.touch)
            }
            .accessibilityLabel(Text("meals_back"))
            Text(title)
                .font(.system(size: TypeScale.heading, weight: .semibold))
                .foregroundStyle(MP.text)
                .lineLimit(1)
            Spacer()
        }
        .padding(.horizontal, Space.small)
    }
}

private struct SectionTitle: View {
    let text: String
    var body: some View {
        Text(text)
            .font(display(TypeScale.heading + 2))
            .foregroundStyle(Warm.ink)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Space.edge)
            .padding(.top, Space.xl)
            .padding(.bottom, Space.small)
    }
}

private struct FieldLabel: View {
    let text: String
    var body: some View {
        Text(text)
            .font(.system(size: TypeScale.secondary, weight: .semibold))
            .foregroundStyle(MP.textMuted)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Space.edge)
            .padding(.top, Space.large)
            .padding(.bottom, Space.small)
    }
}

private struct CardBox<Content: View>: View {
    @ViewBuilder let content: Content
    var body: some View {
        VStack(alignment: .leading, spacing: Space.small) { content }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(Space.large)
            .background(Warm.card, in: RoundedRectangle(cornerRadius: 16))
            .overlay(RoundedRectangle(cornerRadius: 16).stroke(Warm.line, lineWidth: 1))
            .padding(.horizontal, Space.edge)
            .padding(.vertical, 6)
    }
}

private struct Chip: View {
    let label: String
    let selected: Bool
    var enabled: Bool = true
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(.system(size: TypeScale.secondary, weight: selected ? .semibold : .regular))
                .foregroundStyle(selected ? Color.white : MP.text)
                .padding(.horizontal, 14)
                .padding(.vertical, 9)
                .background(selected ? Warm.chilli : MP.surface, in: Capsule())
                .overlay(Capsule().stroke(selected ? Color.clear : MP.line, lineWidth: 1))
        }
        .disabled(!enabled)
        .opacity(enabled ? 1 : 0.5)
    }
}

private struct PrimaryButton: View {
    let label: String
    var enabled: Bool = true
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(.system(size: TypeScale.body, weight: .semibold))
                .foregroundStyle(Color.white)
                .frame(maxWidth: .infinity, minHeight: 48)
                .background(enabled ? Warm.chilli : Warm.chilli.opacity(0.35), in: RoundedRectangle(cornerRadius: 12))
        }
        .disabled(!enabled)
    }
}

private struct SecondaryButton: View {
    let label: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(.system(size: TypeScale.secondary, weight: .semibold))
                .foregroundStyle(MP.text)
                .padding(.horizontal, Space.large)
                .frame(minHeight: 46)
                .overlay(RoundedRectangle(cornerRadius: 10).stroke(MP.line, lineWidth: 1))
        }
    }
}

private struct InputField: View {
    let placeholder: String
    @Binding var text: String

    var body: some View {
        TextField(placeholder, text: $text)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .font(.system(size: TypeScale.body))
            .foregroundStyle(MP.text)
            .tint(Warm.chilli)
            .padding(.horizontal, Space.medium)
            .padding(.vertical, 14)
            .background(MP.surface, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).stroke(MP.line, lineWidth: 1))
    }
}

private struct PriceLine: View {
    let label: String
    let value: String
    var strong = false

    var body: some View {
        HStack {
            Text(label).foregroundStyle(strong ? MP.text : MP.textMuted)
            Spacer()
            Text(value).foregroundStyle(strong ? Warm.ink : MP.text)
        }
        .font(strong ? display(TypeScale.heading) : .system(size: TypeScale.body))
    }
}

private struct Choice: View {
    let title: String
    let detail: String
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: Space.medium) {
                Circle()
                    .strokeBorder(selected ? Warm.chilli : MP.textFaint, lineWidth: 2)
                    .background(Circle().fill(selected ? Warm.chilli : .clear).padding(4))
                    .frame(width: 18, height: 18)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.system(size: TypeScale.body)).foregroundStyle(MP.text)
                    if !detail.isEmpty {
                        Text(detail).font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                    }
                }
                Spacer()
            }
            .padding(Space.medium)
            .background(Warm.card, in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).stroke(selected ? Warm.chilli : MP.line, lineWidth: 1))
        }
        .padding(.horizontal, Space.edge)
        .padding(.vertical, 4)
    }
}

private struct WeekBars: View {
    var body: some View {
        HStack(spacing: 6) {
            ForEach(0..<4, id: \.self) { i in
                Capsule().fill(Warm.weeks[i]).frame(height: 6)
            }
        }
    }
}

/// A photo when the kitchen has one that loads, otherwise a coloured cover
/// with its initial and cuisines.
private struct KitchenCover: View {
    let name: String
    let imageUrl: String?
    let cuisines: [String]

    var body: some View {
        let tint = Warm.covers[abs(name.hashValue) % Warm.covers.count]
        ZStack {
            LinearGradient(colors: [tint, tint.opacity(0.6)], startPoint: .topLeading, endPoint: .bottomTrailing)
            VStack(spacing: 4) {
                Text(String(name.prefix(1)).uppercased())
                    .font(display(40))
                    .foregroundStyle(Color.white)
                if !cuisines.isEmpty {
                    Text(cuisines.prefix(3).joined(separator: " · "))
                        .font(.system(size: TypeScale.caption))
                        .foregroundStyle(Color.white.opacity(0.8))
                }
            }
            if let imageUrl, let url = URL(string: imageUrl) {
                AsyncImage(url: url) { phase in
                    if case .success(let image) = phase {
                        image.resizable().scaledToFill()
                    }
                }
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 14))
    }
}

private struct RatingText: View {
    let rating: KotlinDouble?
    let count: Int32

    var body: some View {
        if let rating, count > 0 {
            Text(tr("meals_rating", String(format: "%.1f", rating.doubleValue), Int(count)))
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Warm.chilli)
        } else {
            Text("meals_new")
                .font(.system(size: TypeScale.caption, weight: .semibold))
                .foregroundStyle(Warm.leaf)
        }
    }
}

private struct VerifiedBadge: View {
    var body: some View {
        Text("✓ " + NSLocalizedString("meals_verified", comment: ""))
            .font(.system(size: TypeScale.caption))
            .foregroundStyle(Warm.leaf)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .overlay(Capsule().stroke(Warm.leaf.opacity(0.5), lineWidth: 1))
    }
}

private struct StatusPill: View {
    let status: String

    var body: some View {
        let tint: Color = switch status {
        case "active": Warm.leaf
        case "pending", "pending_payment": Warm.chilli
        case "cancelled": MP.bad
        default: MP.textMuted
        }
        Text(statusLabel(status))
            .font(.system(size: TypeScale.caption, weight: .semibold))
            .foregroundStyle(tint)
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .overlay(Capsule().stroke(tint.opacity(0.5), lineWidth: 1))
    }
}

private struct RenewToggle: View {
    let on: Bool
    let onChange: (Bool) -> Void

    var body: some View {
        Toggle(isOn: Binding(get: { on }, set: onChange)) {
            Text("meals_auto_renew")
                .font(.system(size: TypeScale.body))
                .foregroundStyle(MP.text)
        }
        .tint(Warm.chilli)
    }
}

// MARK: - Home

private struct MealsHomePage: View {
    let vm: MealsViewModel
    let onKitchen: (String) -> Void
    let onRotation: () -> Void
    let onMine: () -> Void
    let place: ChosenPlace?
    let onPlace: (ChosenPlace?) -> Void

    @State private var current: StorefrontUiState?
    @State private var picking = false
    @Environment(\.openURL) private var openURL
    private var s: StorefrontUiState { current ?? vm.store.value }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                header
                if let failure = s.error, s.kitchens.isEmpty {
                    FailureBlock(failure: failure) { vm.loadStore() }
                } else if s.loading && s.kitchens.isEmpty {
                    LoadingBlock()
                } else {
                    // Kitchens first: the choice is the page. Merchandising follows.
                    nearHeader
                    if s.kitchens.isEmpty {
                        EmptyBlock(message: NSLocalizedString("meals_none", comment: ""))
                    }
                    ForEach(Array(s.kitchens.prefix(3)), id: \.branchId) { k in kitchenButton(k) }
                    if s.slot == nil { rotationPromo }
                    ForEach(Array(s.kitchens.dropFirst(3)), id: \.branchId) { k in kitchenButton(k) }
                    ForEach(s.sections.filter { $0.type == "banner_carousel" }, id: \.id) { section in sectionView(section) }
                    if s.slot == nil && !s.dishes.isEmpty { dishRail }
                    ForEach(s.sections.filter { $0.type == "kitchen_rail" }, id: \.id) { section in sectionView(section) }
                    officeLink
                }
                Color.clear.frame(height: navBarHeight + Space.xl)
            }
        }
        .task {
            for await state in vm.store { current = state }
        }
        .sheet(isPresented: $picking) {
            PlacePicker(vm: vm, current: place) { onPlace($0); picking = false }
        }
    }

    private func kitchenButton(_ k: Kitchen) -> some View {
        Button { onKitchen(k.branchId) } label: { KitchenCard(k: k) }
            .buttonStyle(.plain)
    }

    private var nearHeader: some View {
        let title: String
        if let p = place {
            title = s.kitchens.isEmpty ? tr("meals_none_near", p.name) : tr("meals_near_place", s.kitchens.count, p.name)
        } else {
            title = tr("meals_near_count", s.kitchens.count)
        }
        return HStack(alignment: .firstTextBaseline) {
            Text(title).font(display(TypeScale.title)).foregroundStyle(Warm.ink)
            Spacer(minLength: Space.small)
            Button { picking = true } label: {
                Text("meals_change")
                    .font(.system(size: TypeScale.body, weight: .semibold))
                    .underline()
                    .foregroundStyle(Warm.chilli)
                    .frame(minHeight: Space.touch)
            }
        }
        .padding(.horizontal, Space.edge)
        .padding(.top, Space.large)
        .padding(.bottom, Space.small)
    }

    private var officeLink: some View {
        Button {
            if let u = URL(string: Config.webBaseUrl + (isArabicLocale ? "/ar" : "/en") + "/business") { openURL(u) }
        } label: {
            Text("meals_office_lunch")
                .font(.system(size: TypeScale.body, weight: .semibold))
                .underline()
                .foregroundStyle(Warm.ink)
                .frame(minHeight: Space.touch)
        }
        .padding(.horizontal, Space.edge)
        .padding(.vertical, Space.medium)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: Space.large) {
            HStack(alignment: .center) {
                VStack(alignment: .leading, spacing: 2) {
                    Text("meals_title").font(display(TypeScale.title * 1.3)).foregroundStyle(Warm.ink)
                    Text("meals_subtitle").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                }
                Spacer()
                Button(action: onMine) {
                    Text("meals_my")
                        .font(.system(size: TypeScale.secondary, weight: .semibold))
                        .foregroundStyle(Color.white)
                        .padding(.horizontal, 14)
                        .padding(.vertical, 10)
                        .background(Warm.chilli, in: Capsule())
                }
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Space.small) {
                    Chip(label: NSLocalizedString("meals_slot_all", comment: ""), selected: s.slot == nil) {
                        vm.selectSlot(slot: nil)
                    }
                    ForEach(["breakfast", "lunch", "dinner"], id: \.self) { slot in
                        Chip(label: slotLabel(slot), selected: s.slot == slot) { vm.selectSlot(slot: slot) }
                    }
                }
            }
        }
        .padding(.horizontal, Space.edge)
        .padding(.top, Space.large)
        .padding(.bottom, Space.medium)
        .background(LinearGradient(colors: [Warm.blush, Warm.paper], startPoint: .top, endPoint: .bottom))
    }

    private var rotationPromo: some View {
        Button(action: onRotation) {
            VStack(alignment: .leading, spacing: Space.small) {
                WeekBars()
                Text("meals_rotation_title").font(display(TypeScale.heading * 1.2)).foregroundStyle(Color.white)
                Text("meals_rotation_body").font(.system(size: TypeScale.secondary)).foregroundStyle(Color.white.opacity(0.8))
                Text(NSLocalizedString("meals_rotation_cta", comment: "") + " →")
                    .font(.system(size: TypeScale.body, weight: .bold))
                    .foregroundStyle(Warm.saffron)
            }
            .multilineTextAlignment(.leading)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(Space.large)
            .background(
                LinearGradient(colors: [Warm.char, Warm.chilliDeep], startPoint: .topLeading, endPoint: .bottomTrailing),
                in: RoundedRectangle(cornerRadius: 18)
            )
        }
        .buttonStyle(.plain)
        .padding(.horizontal, Space.edge)
        .padding(.vertical, Space.small)
    }

    private var dishRail: some View {
        VStack(alignment: .leading, spacing: 0) {
            SectionTitle(text: NSLocalizedString("meals_tomorrow", comment: ""))
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: Space.medium) {
                    ForEach(Array(s.dishes.enumerated()), id: \.offset) { _, d in
                        Button { onKitchen(d.branchId) } label: { DishTile(d: d) }
                            .buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, Space.edge)
                .padding(.bottom, Space.small)
            }
        }
    }

    @ViewBuilder
    private func sectionView(_ section: HomeSection) -> some View {
        let title = section.settings.title.map { pick($0, section.settings.titleAr) }
            ?? section.collection.map { pick($0.title, $0.titleAr) }
        switch section.type {
        case "kitchen_rail":
            // Staff rails skip kitchens the list above already shows.
            let listed = Set(s.kitchens.map { $0.branchId })
            let kitchens = (section.kitchens ?? []).filter { !listed.contains($0.branchId) }
            if !kitchens.isEmpty {
                if let title { SectionTitle(text: title) }
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: Space.medium) {
                        ForEach(kitchens, id: \.branchId) { k in
                            Button { onKitchen(k.branchId) } label: {
                                VStack(alignment: .leading, spacing: 4) {
                                    KitchenCover(name: pick(k.name, k.nameAr), imageUrl: k.coverImageUrl, cuisines: k.cuisines.map(tagLabel))
                                        .frame(width: 220, height: 124)
                                    Text(pick(k.name, k.nameAr))
                                        .font(.system(size: TypeScale.body, weight: .semibold))
                                        .foregroundStyle(MP.text)
                                        .lineLimit(1)
                                    Text(tr("meals_from_meal", money(k.fromMealPriceMinor, k.currency)))
                                        .font(.system(size: TypeScale.secondary))
                                        .foregroundStyle(Warm.chilli)
                                }
                                .frame(width: 220, alignment: .leading)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.horizontal, Space.edge)
                }
            }
        case "banner_carousel":
            let banners = section.banners ?? []
            if !banners.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: Space.medium) {
                        ForEach(banners, id: \.id) { b in
                            VStack(alignment: .leading, spacing: 4) {
                                Text(pick(b.title, b.titleAr)).font(display(TypeScale.heading)).foregroundStyle(Color.white)
                                if let body = b.body {
                                    Text(pick(body, b.bodyAr))
                                        .font(.system(size: TypeScale.secondary))
                                        .foregroundStyle(Color.white.opacity(0.75))
                                }
                            }
                            .frame(width: 300, alignment: .leading)
                            .padding(Space.large)
                            .background(Warm.chilli, in: RoundedRectangle(cornerRadius: 16))
                        }
                    }
                    .padding(.horizontal, Space.edge)
                    .padding(.vertical, Space.small)
                }
            }
        default:
            EmptyView()
        }
    }
}

/// A food photo filling whatever frame it is given, over a drawn tile that
/// stays visible until (and unless) the photo loads.
private struct FoodPhoto: View {
    let seed: String
    let imageUrl: String?
    /// Tried when [imageUrl] is missing or fails, e.g. one of the kitchen's dish photos.
    var fallbackUrl: String? = nil

    var body: some View {
        let tint = Warm.covers[abs(seed.hashValue) % Warm.covers.count]
        Rectangle()
            .fill(LinearGradient(colors: [tint, tint.opacity(0.6)], startPoint: .topLeading, endPoint: .bottomTrailing))
            .overlay {
                Text(String(seed.prefix(1)).uppercased())
                    .font(display(34))
                    .foregroundStyle(Color.white.opacity(0.9))
            }
            .overlay {
                PhotoChain(urls: [imageUrl, fallbackUrl].compactMap { $0 }.filter { !$0.isEmpty })
            }
            .clipped()
    }
}

/// Tries each photo in turn: a broken link falls through to the next, then to
/// the drawn tile underneath.
private struct PhotoChain: View {
    let urls: [String]

    var body: some View {
        if let first = urls.first, let url = URL(string: first) {
            AsyncImage(url: url) { phase in
                switch phase {
                case .success(let image): image.resizable().scaledToFill()
                case .failure: PhotoChain(urls: Array(urls.dropFirst()))
                default: Color.clear
                }
            }
        }
    }
}

private struct DishTile: View {
    let d: DayDish

    var body: some View {
        let name = pick(d.name, d.nameAr)
        VStack(alignment: .leading, spacing: 0) {
            FoodPhoto(seed: name, imageUrl: d.imageUrl)
                .frame(width: 200, height: 170)
                .overlay(alignment: .topLeading) {
                    if d.hasAlternative {
                        Text("meals_two_choices")
                            .font(.system(size: TypeScale.caption, weight: .bold))
                            .foregroundStyle(Color.white)
                            .padding(.horizontal, 10)
                            .padding(.vertical, 5)
                            .background(Warm.chilli, in: Capsule())
                            .padding(Space.small)
                    }
                }
            VStack(alignment: .leading, spacing: 4) {
                Text(name)
                    .font(.system(size: TypeScale.body, weight: .bold))
                    .foregroundStyle(Warm.ink)
                    .lineLimit(1)
                Text(pick(d.kitchenName, d.kitchenNameAr) + " · " + slotLabel(d.slot))
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Warm.muted)
                    .lineLimit(1)
                Text(tr("meals_from_meal", money(d.fromMealPriceMinor, d.currency)))
                    .font(.system(size: TypeScale.secondary, weight: .bold))
                    .foregroundStyle(Warm.chilli)
            }
            .padding(Space.medium)
        }
        .frame(width: 200, alignment: .leading)
        .background(Color.white)
        .clipShape(RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).stroke(Warm.line, lineWidth: 1))
    }
}

private struct KitchenCard: View {
    let k: Kitchen

    var body: some View {
        let name = pick(k.name, k.nameAr)
        let dishPhoto = k.weekDishes.first(where: { $0.imageUrl != nil })?.imageUrl
        VStack(alignment: .leading, spacing: 0) {
            FoodPhoto(seed: name, imageUrl: k.coverImageUrl, fallbackUrl: dishPhoto)
                .frame(height: 190)
                .overlay(alignment: .topLeading) {
                    HStack(spacing: 6) {
                        if let rating = k.rating, k.ratingCount > 0 {
                            Text("★ " + String(format: "%.1f", rating.doubleValue))
                                .font(.system(size: TypeScale.caption, weight: .bold))
                                .foregroundStyle(Warm.ink)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 5)
                                .background(Warm.saffron, in: Capsule())
                        }
                        if k.foodSafetyVerified {
                            Text("✓ " + NSLocalizedString("meals_verified", comment: ""))
                                .font(.system(size: TypeScale.caption, weight: .semibold))
                                .foregroundStyle(Warm.leaf)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 5)
                                .background(Warm.leafSoft, in: Capsule())
                        }
                    }
                    .padding(Space.medium)
                }
                .overlay(alignment: .bottomLeading) {
                    Text(tr("meals_from_meal", money(k.fromMealPriceMinor, k.currency)))
                        .font(.system(size: TypeScale.secondary, weight: .bold))
                        .foregroundStyle(Warm.ink)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 7)
                        .background(Color.white, in: Capsule())
                        .padding(Space.medium)
                }
            VStack(alignment: .leading, spacing: 6) {
                Text(name)
                    .font(.system(size: TypeScale.heading, weight: .bold))
                    .foregroundStyle(Warm.ink)
                    .lineLimit(1)
                Text(tagLine(k.cuisines + k.dietaryTags))
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Warm.muted)
                    .lineLimit(1)
                if !k.weekDishes.isEmpty {
                    HStack(spacing: 6) {
                        ForEach(Array(k.weekDishes.prefix(4).enumerated()), id: \.offset) { _, d in
                            FoodPhoto(seed: pick(d.name, d.nameAr), imageUrl: d.imageUrl)
                                .frame(width: 40, height: 40)
                                .clipShape(Circle())
                        }
                        Text(k.weekDishes.prefix(2).map { pick($0.name, $0.nameAr) }.joined(separator: ", "))
                            .font(.system(size: TypeScale.caption))
                            .foregroundStyle(Warm.muted)
                            .lineLimit(1)
                    }
                    .padding(.top, 2)
                }
            }
            .padding(Space.medium)
        }
        .background(Color.white)
        .clipShape(RoundedRectangle(cornerRadius: 18))
        .overlay(RoundedRectangle(cornerRadius: 18).stroke(Warm.line, lineWidth: 1))
        .padding(.horizontal, Space.edge)
        .padding(.vertical, Space.small)
    }
}

/// The location sheet: current location, saved addresses, or an area.
private struct PlacePicker: View {
    let vm: MealsViewModel
    let current: ChosenPlace?
    let onPick: (ChosenPlace?) -> Void

    @State private var query = ""
    @State private var addresses: [Address] = []

    var body: some View {
        NavigationView {
            List {
                Section {
                    row(NSLocalizedString("meals_use_location", comment: ""), nil, current == nil) { onPick(nil) }
                }
                if !addresses.isEmpty {
                    Section(header: Text("meals_your_addresses")) {
                        ForEach(addresses, id: \.id) { a in
                            row(a.label, a.line, current?.kind == "address" && current?.id == a.id) {
                                let area = (a.area ?? "").isEmpty ? a.label : (a.area ?? a.label)
                                onPick(ChosenPlace(kind: "address", id: a.id, label: area, labelAr: nil,
                                                   lat: a.point.lat, lng: a.point.lng))
                            }
                        }
                    }
                }
                Section(header: Text("meals_choose_area")) {
                    let areas = MealAreas.shared.search(query: query)
                    if areas.isEmpty {
                        Text("meals_no_area").foregroundStyle(Warm.muted)
                    }
                    ForEach(areas, id: \.id) { a in
                        row(a.name(arabic: isArabicLocale), a.city(arabic: isArabicLocale),
                            current?.kind == "area" && current?.id == a.id) {
                            onPick(ChosenPlace(kind: "area", id: a.id, label: a.en, labelAr: a.ar, lat: a.lat, lng: a.lng))
                        }
                    }
                }
            }
            .searchable(text: $query, prompt: Text("meals_search_areas"))
            .navigationTitle(Text("meals_where"))
            .navigationBarTitleDisplayMode(.inline)
        }
        .task {
            vm.loadPlaceAddresses()
            for await state in vm.store { addresses = state.addresses }
        }
    }

    private func row(_ title: String, _ detail: String?, _ selected: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(Warm.ink)
                    if let detail, !detail.isEmpty {
                        Text(detail).font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.muted)
                    }
                }
                Spacer()
                if selected { Image(systemName: "checkmark").foregroundStyle(Warm.chilli) }
            }
        }
    }
}

// MARK: - Kitchen

private struct KitchenPage: View {
    let vm: MealsViewModel
    let onBack: () -> Void
    let onSubscribe: (MealPlan) -> Void

    @State private var current: KitchenUiState?
    private var s: KitchenUiState { current ?? vm.kitchen.value }

    var body: some View {
        Group {
            if let failure = s.error {
                VStack(spacing: 0) {
                    TopBar(title: "", onBack: onBack)
                    FailureBlock(failure: failure) { vm.openKitchen(branchId: s.branchId) }
                    Spacer()
                }
            } else if let k = s.kitchen {
                KitchenBody(k: k, reviews: s.reviews, onBack: onBack, onSubscribe: onSubscribe)
            } else {
                VStack(spacing: 0) {
                    TopBar(title: "", onBack: onBack)
                    LoadingBlock()
                    Spacer()
                }
            }
        }
        .task {
            for await state in vm.kitchen { current = state }
        }
    }
}

private struct KitchenBody: View {
    let k: KitchenDetail
    let reviews: [KitchenReview]
    let onBack: () -> Void
    let onSubscribe: (MealPlan) -> Void

    @State private var day: Int32?
    /// Plan-area rows currently on screen; the sticky bar hides while any is.
    @State private var planRows: Set<String> = []

    var body: some View {
        let byDay = Dictionary(grouping: k.menu.slots, by: { $0.weekday })
        let days = byDay.keys.sorted()
        let selected = day.flatMap { byDay[$0] != nil ? $0 : nil } ?? days.first
        let name = pick(k.name, k.nameAr)
        let dishPhoto = k.menu.slots.compactMap { $0.`default`?.imageUrl ?? $0.alternative?.imageUrl }.first
        let enabled = k.deliversHere?.boolValue != false
        let from = k.plans.map { $0.mealPriceMinor }.min()
        let currency = k.plans.first?.currency ?? "AED"
        ScrollViewReader { proxy in
            ZStack(alignment: .bottom) {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        FoodPhoto(seed: name, imageUrl: k.coverImageUrl, fallbackUrl: dishPhoto)
                            .frame(height: 260)
                            .overlay(alignment: .topLeading) {
                                Button(action: onBack) {
                                    Image(systemName: "chevron.backward")
                                        .font(.system(size: 17, weight: .bold))
                                        .foregroundStyle(Warm.ink)
                                        .frame(width: Space.touch, height: Space.touch)
                                        .background(Color.white, in: Circle())
                                }
                                .accessibilityLabel(Text("meals_back"))
                                .padding(Space.medium)
                            }
                        VStack(alignment: .leading, spacing: 8) {
                            Text(name).font(display(TypeScale.title * 1.25)).foregroundStyle(Warm.ink)
                            if !k.cuisines.isEmpty {
                                Text(tagLine(k.cuisines))
                                    .font(.system(size: TypeScale.secondary))
                                    .foregroundStyle(Warm.muted)
                            }
                            if let from {
                                Text(tr("meals_from_meal", money(from, currency)))
                                    .font(.system(size: TypeScale.secondary, weight: .bold))
                                    .foregroundStyle(Color.white)
                                    .padding(.horizontal, 12)
                                    .padding(.vertical, 7)
                                    .background(Warm.chilli, in: Capsule())
                            }
                            HStack(spacing: Space.small) {
                                RatingText(rating: k.rating, count: k.ratingCount)
                                if k.foodSafetyVerified { VerifiedBadge() }
                            }
                            if let d = k.description_ {
                                Text(pick(d, k.descriptionAr)).font(.system(size: TypeScale.body)).foregroundStyle(Warm.muted)
                            }
                            if !enabled {
                                Text("meals_not_here").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.bad)
                            }
                        }
                        .padding(Space.edge)

                        if !days.isEmpty {
                            SectionTitle(text: NSLocalizedString("meals_weekly_menu", comment: ""))
                            ScrollView(.horizontal, showsIndicators: false) {
                                HStack(spacing: Space.small) {
                                    ForEach(days, id: \.self) { d in
                                        Chip(label: weekday(d), selected: d == selected) { day = d }
                                    }
                                }
                                .padding(.horizontal, Space.edge)
                                .padding(.bottom, Space.small)
                            }
                            if byDay.values.contains(where: { $0.contains { $0.alternative != nil } }) {
                                Text("meals_choice_help")
                                    .font(.system(size: TypeScale.secondary))
                                    .foregroundStyle(Warm.muted)
                                    .padding(.horizontal, Space.edge)
                                    .padding(.bottom, Space.small)
                            }
                            ForEach(selected.flatMap { byDay[$0] } ?? [], id: \.slot) { slot in
                                DishCard(slot: slot)
                            }
                            SectionTitle(text: NSLocalizedString("meals_week_glance", comment: ""))
                            CardBox {
                                ForEach(days, id: \.self) { d in
                                    HStack(alignment: .top, spacing: Space.medium) {
                                        Text(weekday(d))
                                            .font(.system(size: TypeScale.secondary, weight: .bold))
                                            .foregroundStyle(Warm.chilli)
                                            .frame(width: 64, alignment: .leading)
                                        Text((byDay[d] ?? []).compactMap { $0.`default`.map { pick($0.name, $0.nameAr) } }
                                            .joined(separator: ", "))
                                            .font(.system(size: TypeScale.secondary))
                                            .foregroundStyle(Warm.ink)
                                    }
                                }
                            }
                        }

                        if !k.plans.isEmpty {
                            SectionTitle(text: NSLocalizedString("meals_plans", comment: ""))
                                .id("plans")
                                .onAppear { planRows.insert("title") }
                                .onDisappear { planRows.remove("title") }
                            ForEach(k.plans, id: \.id) { p in
                                PlanCard(p: p, popular: p.id == k.popularPlanId, enabled: enabled) { onSubscribe(p) }
                                    .onAppear { planRows.insert(p.id) }
                                    .onDisappear { planRows.remove(p.id) }
                            }
                        }
                        Group {
                            if !reviews.isEmpty {
                                SectionTitle(text: NSLocalizedString("meals_reviews", comment: ""))
                                ForEach(reviews.prefix(5), id: \.id) { r in
                                    CardBox {
                                        let stars = max(0, min(5, Int(r.rating)))
                                        Text(String(repeating: "★", count: stars) + String(repeating: "☆", count: 5 - stars))
                                            .font(.system(size: TypeScale.secondary))
                                            .foregroundStyle(Warm.saffron)
                                        if !r.comment.isEmpty {
                                            Text(r.comment).font(.system(size: TypeScale.body)).foregroundStyle(Warm.ink)
                                        }
                                        Text(NSLocalizedString("meals_subscriber", comment: "") + " · " + shortDate(r.createdAt))
                                            .font(.system(size: TypeScale.caption))
                                            .foregroundStyle(Warm.muted)
                                    }
                                }
                            }
                            Color.clear.frame(height: navBarHeight + Space.xl + 72)
                        }
                        .onAppear { planRows.insert("after") }
                        .onDisappear { planRows.remove("after") }
                    }
                }
                if !k.plans.isEmpty && planRows.isEmpty {
                    HStack(spacing: Space.medium) {
                        if let from {
                            Text(tr("meals_from_meal", money(from, currency)))
                                .font(.system(size: TypeScale.body, weight: .bold))
                                .foregroundStyle(Warm.ink)
                                .lineLimit(1)
                                .minimumScaleFactor(0.8)
                        }
                        Spacer(minLength: 0)
                        Button {
                            withAnimation { proxy.scrollTo("plans", anchor: .top) }
                        } label: {
                            Text("meals_see_plans")
                                .font(.system(size: TypeScale.body, weight: .bold))
                                .foregroundStyle(Color.white)
                                .padding(.horizontal, 22)
                                .frame(minHeight: 46)
                                .background(Warm.chilli, in: RoundedRectangle(cornerRadius: 12))
                        }
                    }
                    .padding(.horizontal, Space.edge)
                    .padding(.vertical, Space.medium)
                    .background(Color.white)
                    .overlay(alignment: .top) { Rectangle().fill(Warm.line).frame(height: 1) }
                    .padding(.bottom, navBarHeight)
                }
            }
        }
    }
}

private struct DishCard: View {
    let slot: MenuSlot

    var body: some View {
        let main = slot.`default` ?? slot.alternative
        VStack(alignment: .leading, spacing: 0) {
            FoodPhoto(seed: main.map { pick($0.name, $0.nameAr) } ?? slotLabel(slot.slot), imageUrl: main?.imageUrl)
                .frame(height: 190)
                .overlay(alignment: .topLeading) {
                    Text(slotLabel(slot.slot))
                        .font(.system(size: TypeScale.caption, weight: .bold))
                        .foregroundStyle(Warm.ink)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 5)
                        .background(Warm.saffronSoft, in: Capsule())
                        .padding(Space.medium)
                }
            VStack(alignment: .leading, spacing: 8) {
                if let main {
                    Text(pick(main.name, main.nameAr))
                        .font(.system(size: TypeScale.heading, weight: .bold))
                        .foregroundStyle(Warm.ink)
                }
                if let a = slot.alternative, slot.`default` != nil {
                    HStack(spacing: Space.small) {
                        FoodPhoto(seed: pick(a.name, a.nameAr), imageUrl: a.imageUrl)
                            .frame(width: 40, height: 40)
                            .clipShape(Circle())
                        Text(tr("meals_or", pick(a.name, a.nameAr)))
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Warm.muted)
                    }
                }
            }
            .padding(Space.medium)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.white)
        .clipShape(RoundedRectangle(cornerRadius: 18))
        .overlay(RoundedRectangle(cornerRadius: 18).stroke(Warm.line, lineWidth: 1))
        .padding(.horizontal, Space.edge)
        .padding(.vertical, 6)
    }
}

private struct PlanCard: View {
    let p: MealPlan
    var popular: Bool = false
    let enabled: Bool
    let onSubscribe: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if popular {
                Text("meals_popular")
                    .font(.system(size: TypeScale.caption, weight: .bold))
                    .foregroundStyle(Color.white)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(Warm.chilli, in: Capsule())
            }
            Text(pick(p.name, p.nameAr))
                .font(.system(size: TypeScale.heading, weight: .bold))
                .foregroundStyle(Warm.ink)
            Text(tr("meals_meal_price", money(p.mealPriceMinor, p.currency)))
                .font(display(TypeScale.title))
                .foregroundStyle(Warm.ink)
            Text(tr("meals_cycle_from", money(p.priceMinor, p.currency)))
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Warm.muted)
            Text(tr("meals_plan_summary", Int(p.mealsPerCycle), Int(p.daysPerWeek))
                 + (p.mealSlots.isEmpty ? "" : " · " + p.mealSlots.map(slotLabel).joined(separator: ", ")))
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Warm.muted)
            PrimaryButton(label: NSLocalizedString("meals_subscribe", comment: ""), enabled: enabled, action: onSubscribe)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(Space.large)
        .background(Color.white)
        .clipShape(RoundedRectangle(cornerRadius: 18))
        .overlay(RoundedRectangle(cornerRadius: 18).stroke(popular ? Warm.chilli : Warm.line, lineWidth: popular ? 2 : 1))
        .padding(.horizontal, Space.edge)
        .padding(.vertical, 6)
    }
}

// MARK: - Checkout

private struct CheckoutPage: View {
    let vm: MealsViewModel
    let lat: Double
    let lng: Double
    let onBack: () -> Void
    let onPaid: () -> Void
    let openPayment: (String) -> Void

    @State private var current: CheckoutUiState?
    private var s: CheckoutUiState { current ?? vm.checkout.value }
    @State private var startDate = ""
    @State private var promo = ""

    private var promoCode: String? {
        guard let failure = s.error, case .rejected(let r) = onEnum(of: failure) else { return nil }
        return r.fieldCodes["promo_code"]
    }

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: NSLocalizedString("meals_checkout", comment: ""), onBack: onBack)
            if let plan = s.plan {
                form(plan)
                PrimaryButton(
                    label: tr("meals_pay", s.preview.map { money($0.totalMinor, $0.currency) } ?? ""),
                    enabled: s.preview != nil && !s.busy
                ) { vm.subscribe() }
                .padding(Space.edge)
                Color.clear.frame(height: navBarHeight)
            } else {
                Spacer()
            }
        }
        .task {
            startDate = vm.checkout.value.startDate
            promo = vm.checkout.value.promoCode
            for await state in vm.checkout {
                current = state
                if let url = state.paymentUrl { openPayment(url) }
                if state.subscriptionId != nil { onPaid() }
            }
        }
    }

    private func form(_ plan: MealPlan) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                CardBox {
                    Text(s.kitchen.map { pick($0.name, $0.nameAr) } ?? "")
                        .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                    Text(pick(plan.name, plan.nameAr))
                        .font(.system(size: TypeScale.heading, weight: .semibold)).foregroundStyle(MP.text)
                    Text(tr("meals_meal_price", money(plan.mealPriceMinor, plan.currency)))
                        .font(.system(size: TypeScale.body)).foregroundStyle(Warm.chilli)
                }
                FieldLabel(text: NSLocalizedString("meals_address", comment: ""))
                ForEach(s.addresses, id: \.id) { a in
                    Choice(title: a.label, detail: a.line, selected: a.id == s.addressId) { vm.selectAddress(id: a.id) }
                }
                AddressForm(startOpen: s.addresses.isEmpty, busy: s.busy) { label, building, area, city in
                    vm.addAddress(label: label, building: building, area: area, city: city, at: LatLng(lat: lat, lng: lng))
                }
                FieldLabel(text: tr("meals_days", Int(plan.daysPerWeek)))
                chips(plan.deliveryWeekdays.map { $0.int32Value }.sorted(), label: { weekday($0) },
                      selected: { s.weekdays.contains(KotlinInt(int: $0)) }) { vm.toggleWeekday(day: $0) }
                FieldLabel(text: tr("meals_meals", Int(plan.mealsPerDay)))
                chips(plan.mealSlots, label: slotLabel, selected: { s.slots.contains($0) }) { vm.toggleSlot(slot: $0) }
                let windows = s.kitchen?.deliveryWindows ?? []
                if !windows.isEmpty {
                    FieldLabel(text: NSLocalizedString("meals_window", comment: ""))
                    chips(windows.map { $0.id }, label: { id in windows.first { $0.id == id }?.label ?? id },
                          selected: { $0 == s.windowId }) { vm.selectWindow(id: $0) }
                }
                FieldLabel(text: NSLocalizedString("meals_start", comment: ""))
                InputField(placeholder: "YYYY-MM-DD", text: $startDate)
                    .padding(.horizontal, Space.edge)
                    .onChange(of: startDate) { _, new in vm.setStartDate(date: new) }
                FieldLabel(text: NSLocalizedString("meals_promo", comment: ""))
                HStack(spacing: Space.small) {
                    InputField(placeholder: "", text: $promo)
                        .onChange(of: promo) { _, new in vm.setPromo(code: new) }
                    SecondaryButton(label: NSLocalizedString("meals_apply", comment: "")) { vm.applyPromo() }
                }
                .padding(.horizontal, Space.edge)
                if let code = promoCode {
                    Text(promoReason(code))
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(MP.bad)
                        .padding(.horizontal, Space.edge)
                        .padding(.vertical, 6)
                }
                if let p = s.preview {
                    CardBox {
                        Text(shortDate(p.startsOn) + " – " + shortDate(p.endsOn))
                            .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                        PriceLine(label: NSLocalizedString("meals_subtotal", comment: ""), value: money(p.subtotalMinor, p.currency))
                        if p.discountMinor > 0 {
                            PriceLine(label: NSLocalizedString("meals_discount", comment: ""), value: "−" + money(p.discountMinor, p.currency))
                        }
                        PriceLine(label: NSLocalizedString("meals_delivery", comment: ""), value: money(p.deliveryMinor, p.currency))
                        PriceLine(label: NSLocalizedString("meals_total", comment: ""), value: money(p.totalMinor, p.currency), strong: true)
                    }
                    .padding(.top, Space.medium)
                }
                if promoCode == nil, let failure = s.error { FailureStrip(failure: failure) }
                Color.clear.frame(height: Space.xl)
            }
        }
    }

    private func chips<T: Hashable>(_ values: [T], label: @escaping (T) -> String,
                                    selected: @escaping (T) -> Bool, onTap: @escaping (T) -> Void) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Space.small) {
                ForEach(values, id: \.self) { v in
                    Chip(label: label(v), selected: selected(v)) { onTap(v) }
                }
            }
            .padding(.horizontal, Space.edge)
        }
    }
}

private struct AddressForm: View {
    let startOpen: Bool
    let busy: Bool
    let onSave: (String, String, String, String) -> Void

    @State private var open = false
    @State private var label = ""
    @State private var building = ""
    @State private var area = ""
    @State private var city = "Dubai"

    var body: some View {
        if !open && !startOpen {
            Button { open = true } label: {
                Text("+ " + NSLocalizedString("meals_add_address", comment: ""))
                    .font(.system(size: TypeScale.body))
                    .foregroundStyle(Warm.chilli)
                    .padding(.horizontal, Space.edge)
                    .padding(.vertical, Space.medium)
            }
        } else {
            CardBox {
                Text("meals_add_address")
                    .font(.system(size: TypeScale.body, weight: .semibold))
                    .foregroundStyle(MP.text)
                InputField(placeholder: NSLocalizedString("meals_label", comment: ""), text: $label)
                InputField(placeholder: NSLocalizedString("meals_building", comment: ""), text: $building)
                InputField(placeholder: NSLocalizedString("meals_area", comment: ""), text: $area)
                InputField(placeholder: NSLocalizedString("meals_city", comment: ""), text: $city)
                PrimaryButton(
                    label: NSLocalizedString("meals_save_address", comment: ""),
                    enabled: !busy && !label.isEmpty && !city.isEmpty
                ) {
                    onSave(label, building, area, city)
                    open = false
                }
            }
        }
    }
}

// MARK: - Rotation

private struct RotationPage: View {
    let vm: MealsViewModel
    let onBack: () -> Void
    let onPaid: () -> Void
    let openPayment: (String) -> Void

    @State private var current: RotationUiState?
    private var s: RotationUiState { current ?? vm.rotation.value }

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: NSLocalizedString("meals_rotation", comment: ""), onBack: onBack)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    FieldLabel(text: NSLocalizedString("meals_address", comment: ""))
                    ForEach(s.addresses, id: \.id) { a in
                        Choice(title: a.label, detail: a.line, selected: a.id == s.addressId) {
                            vm.selectRotationAddress(id: a.id)
                        }
                    }
                    FieldLabel(text: tr("meals_pick_four", s.picked.count))
                    if s.busy && s.candidates.isEmpty { LoadingBlock() }
                    ForEach(s.candidates, id: \.branchId) { k in
                        let price = s.plans[k.branchId]?.rotationWeekPriceMinor?.int64Value
                            ?? k.rotationWeekPriceMinor?.int64Value ?? 0
                        Choice(
                            title: pick(k.name, k.nameAr),
                            detail: money(price, k.currency),
                            selected: s.picked.contains(k.branchId)
                        ) { vm.togglePick(branchId: k.branchId) }
                    }
                    if let q = s.quote {
                        CardBox {
                            WeekBars()
                            ForEach(q.weeks.sorted { $0.week < $1.week }, id: \.week) { w in
                                PriceLine(label: tr("meals_week", Int(w.week)) + " · " + pick(w.kitchenName, w.kitchenNameAr),
                                          value: money(w.priceMinor, q.currency))
                            }
                            if q.discountMinor > 0 {
                                PriceLine(label: NSLocalizedString("meals_discount", comment: ""), value: "−" + money(q.discountMinor, q.currency))
                            }
                            PriceLine(label: NSLocalizedString("meals_total", comment: ""), value: money(q.amountMinor, q.currency), strong: true)
                        }
                        .padding(.top, Space.medium)
                    }
                    if let failure = s.error { FailureStrip(failure: failure) }
                }
            }
            PrimaryButton(
                label: tr("meals_pay", s.quote.map { money($0.amountMinor, $0.currency) } ?? ""),
                enabled: s.quote != nil && !s.busy
            ) { vm.payRotation() }
            .padding(Space.edge)
            Color.clear.frame(height: navBarHeight)
        }
        .task {
            for await state in vm.rotation {
                current = state
                if let url = state.paymentUrl { openPayment(url) }
                if state.rotationId != nil { onPaid() }
            }
        }
    }
}

// MARK: - Paid, My meals, subscription

private struct PaidPage: View {
    let redirected: Bool
    let onMine: () -> Void

    var body: some View {
        VStack(spacing: Space.large) {
            Text("✓").font(.system(size: TypeScale.title * 2.5)).foregroundStyle(Warm.leaf)
            Text(LocalizedStringKey(redirected ? "meals_paying" : "meals_paid"))
                .font(.system(size: TypeScale.heading))
                .foregroundStyle(MP.text)
                .multilineTextAlignment(.center)
            PrimaryButton(label: NSLocalizedString("meals_view_subs", comment: ""), action: onMine)
        }
        .padding(Space.xl)
    }
}

private struct MinePage: View {
    let vm: MealsViewModel
    let onBack: () -> Void
    let onOpen: (String) -> Void

    @State private var current: MyMealsUiState?
    private var s: MyMealsUiState { current ?? vm.mine.value }

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: NSLocalizedString("meals_my", comment: ""), onBack: onBack)
            if let failure = s.error, !s.loaded {
                FailureBlock(failure: failure) { vm.loadMine(force: true) }
                Spacer()
            } else if !s.loaded {
                LoadingBlock()
                Spacer()
            } else if s.subscriptions.isEmpty && s.rotations.isEmpty {
                EmptyBlock(message: NSLocalizedString("meals_no_subs", comment: ""))
                Spacer()
            } else {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        if !s.rotations.isEmpty {
                            SectionTitle(text: NSLocalizedString("meals_rotations", comment: ""))
                            ForEach(s.rotations, id: \.id) { r in RotationCard(r: r, vm: vm, onOpen: onOpen) }
                        }
                        if !s.subscriptions.isEmpty {
                            SectionTitle(text: NSLocalizedString("meals_subscriptions", comment: ""))
                            ForEach(s.subscriptions, id: \.id) { sub in
                                Button { onOpen(sub.id) } label: { SubscriptionRow(s: sub) }.buttonStyle(.plain)
                            }
                        }
                        Color.clear.frame(height: navBarHeight + Space.xl)
                    }
                }
            }
        }
        .task {
            vm.loadMine(force: false)
            for await state in vm.mine { current = state }
        }
    }
}

private struct RotationCard: View {
    let r: Rotation
    let vm: MealsViewModel
    let onOpen: (String) -> Void
    @State private var confirming = false

    var body: some View {
        CardBox {
            HStack {
                Text(r.rotationNo).font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                Spacer()
                StatusPill(status: r.status)
            }
            Text(shortDate(r.startDate) + " – " + shortDate(r.endDate))
                .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
            ForEach(r.weeks.sorted { $0.week < $1.week }, id: \.week) { w in
                Button { if let id = w.subscriptionId { onOpen(id) } } label: {
                    HStack(spacing: Space.small) {
                        Circle().fill(Warm.weeks[(Int(w.week) - 1 + 4) % 4]).frame(width: 10, height: 10)
                        Text(tr("meals_week", Int(w.week)) + " · " + pick(w.kitchenName, w.kitchenNameAr))
                            .font(.system(size: TypeScale.body)).foregroundStyle(MP.text)
                        Spacer()
                        Image(systemName: "chevron.forward").foregroundStyle(MP.textFaint)
                    }
                    .padding(.vertical, 6)
                }
                .disabled(w.subscriptionId == nil)
            }
            if r.status == "active" || r.status == "pending" {
                RenewToggle(on: r.autoRenew) { vm.setRotationRenewal(id: r.id, auto: $0) }
                Button { confirming = true } label: {
                    Text("meals_cancel_rotation").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.bad)
                }
                .padding(.vertical, Space.small)
            }
        }
        .modifier(ConfirmCancel(shown: $confirming) { vm.cancelRotation(id: r.id) })
    }
}

private struct SubscriptionRow: View {
    let s: MySubscription

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(pick(s.kitchenName, s.kitchenNameAr))
                    .font(.system(size: TypeScale.heading, weight: .semibold)).foregroundStyle(MP.text)
                Text(pick(s.planName, s.planNameAr))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                if let next = s.upcoming.first {
                    Text(shortDate(next.date) + " · " + slotLabel(next.mealSlot))
                        .font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.chilli)
                }
            }
            Spacer()
            StatusPill(status: s.status)
        }
        .padding(Space.large)
        .background(Warm.card, in: RoundedRectangle(cornerRadius: 16))
        .padding(.horizontal, Space.edge)
        .padding(.vertical, 6)
    }
}

private struct ConfirmCancel: ViewModifier {
    @Binding var shown: Bool
    let onConfirm: () -> Void

    func body(content: Content) -> some View {
        content.alert(Text("meals_cancel_confirm"), isPresented: $shown) {
            Button(role: .destructive, action: onConfirm) { Text("meals_cancel_yes") }
            Button(role: .cancel) {} label: { Text("meals_keep") }
        }
    }
}

private struct SubscriptionPage: View {
    let vm: MealsViewModel
    let onBack: () -> Void
    let openPayment: (String) -> Void

    @State private var current: SubscriptionUiState?
    private var s: SubscriptionUiState { current ?? vm.sub.value }
    @State private var confirming = false

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: s.subscription.map { pick($0.kitchenName, $0.kitchenNameAr) } ?? "", onBack: onBack)
            if let sub = s.subscription {
                content(sub)
            } else if let failure = s.error {
                FailureBlock(failure: failure)
                Spacer()
            } else {
                LoadingBlock()
                Spacer()
            }
        }
        .modifier(ConfirmCancel(shown: $confirming) { vm.cancel() })
        .task {
            for await state in vm.sub {
                current = state
                if let url = state.paymentUrl { openPayment(url) }
            }
        }
    }

    private func content(_ sub: MySubscription) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                CardBox {
                    HStack {
                        Text(pick(sub.planName, sub.planNameAr))
                            .font(.system(size: TypeScale.heading, weight: .semibold)).foregroundStyle(MP.text)
                        Spacer()
                        StatusPill(status: sub.status)
                    }
                    let end = sub.cycleEndsOn ?? sub.endDate
                    Text(shortDate(sub.startDate) + (end.map { " – " + shortDate($0) } ?? ""))
                        .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                    if sub.rotationId == nil && (sub.status == "active" || sub.status == "paused") {
                        RenewToggle(on: sub.renewalMode == "auto") { vm.setAutoRenew(auto: $0) }
                    }
                    if sub.pendingOrderId != nil {
                        PrimaryButton(label: NSLocalizedString("meals_pay_due", comment: ""), enabled: !s.busy) { vm.payPending() }
                    }
                    if sub.canRenew {
                        PrimaryButton(label: NSLocalizedString("meals_renew", comment: ""), enabled: !s.busy) { vm.renew() }
                    }
                }
                if let failure = s.error { FailureStrip(failure: failure) }
                if !sub.upcoming.isEmpty {
                    SectionTitle(text: NSLocalizedString("meals_upcoming", comment: ""))
                    ForEach(sub.upcoming, id: \.self) { m in MealCard(m: m, busy: s.busy, vm: vm) }
                }
                if sub.rotationId == nil && sub.status == "active" && sub.pauseAllowanceDays > 0 {
                    PauseCard(sub: sub, busy: s.busy, vm: vm)
                }
                if sub.rotationId == nil && ["active", "paused", "pending"].contains(sub.status) {
                    Button { confirming = true } label: {
                        Text("meals_cancel").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.bad)
                    }
                    .disabled(s.busy)
                    .padding(Space.edge)
                }
                Color.clear.frame(height: navBarHeight + Space.xl)
            }
        }
    }
}

private struct PauseCard: View {
    let sub: MySubscription
    let busy: Bool
    let vm: MealsViewModel

    @State private var from = ""
    @State private var to = ""

    var body: some View {
        let left = max(0, Int(sub.pauseAllowanceDays) - Int(sub.pauseDaysUsed))
        let first = sub.upcoming.first { !$0.locked }?.date
        if let first {
            CardBox {
                Text("meals_pause_title")
                    .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                Text(tr("meals_pause_left", left, Int(sub.pauseAllowanceDays)))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                HStack(spacing: Space.small) {
                    InputField(placeholder: NSLocalizedString("meals_pause_from", comment: ""), text: $from)
                    InputField(placeholder: NSLocalizedString("meals_pause_to", comment: ""), text: $to)
                }
                PrimaryButton(label: NSLocalizedString("meals_pause", comment: ""), enabled: !busy && left > 0) {
                    vm.pause(from: from.trimmingCharacters(in: .whitespaces), to: to.trimmingCharacters(in: .whitespaces))
                }
            }
            .task(id: first) {
                from = first
                to = first
            }
        }
    }
}

private struct MealCard: View {
    let m: UpcomingMeal
    let busy: Bool
    let vm: MealsViewModel

    var body: some View {
        let chosen = m.chosenDishId ?? m.options.first { $0.isDefault }?.id
        CardBox {
            HStack {
                Text(shortDate(m.date) + " · " + slotLabel(m.mealSlot))
                    .font(.system(size: TypeScale.secondary, weight: .semibold)).foregroundStyle(Warm.chilli)
                Spacer()
                if m.locked {
                    Text("meals_locked").font(.system(size: TypeScale.caption)).foregroundStyle(MP.textFaint)
                } else {
                    Button { vm.skip(date: m.date, slot: m.mealSlot) } label: {
                        Text("meals_skip").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                            .padding(Space.small)
                    }
                    .disabled(busy)
                }
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Space.small) {
                    ForEach(m.options, id: \.id) { o in
                        Chip(label: pick(o.name, o.nameAr), selected: o.id == chosen, enabled: !m.locked && !busy) {
                            vm.choose(date: m.date, slot: m.mealSlot, dishId: o.id)
                        }
                    }
                }
            }
        }
    }
}
