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

/// A count-led sentence from Localizable.stringsdict (Arabic has six forms).
private func trPlural(_ key: String, _ count: Int) -> String {
    String.localizedStringWithFormat(NSLocalizedString(key, comment: ""), count)
}

private func trPlural(_ key: String, _ count: Int, _ name: String) -> String {
    String.localizedStringWithFormat(NSLocalizedString(key, comment: ""), count, name)
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
/// "This week", "Next week", "In 3 weeks" for a multi-week menu's tab.
private func weekTitle(_ index: Int) -> String {
    switch index {
    case 0: return NSLocalizedString("meals_this_week", comment: "")
    case 1: return NSLocalizedString("meals_next_week", comment: "")
    default: return tr("meals_in_weeks", index)
    }
}

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
    case "breakfast", "lunch", "dinner", "suhoor", "iftar": return NSLocalizedString("meals_slot_" + slot, comment: "")
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
    default: return NSLocalizedString("meals_status_other", comment: "")
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
    case home, kitchen, checkout, rotation, rotationRenew, paid, mine, subscription, rotationDetail, collection
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
    /// A deep link or push tap to open, then report handled.
    var link: MealsLink? = nil
    var onLinkHandled: () -> Void = {}
    /// Native Help: a ticket id, or nil for the Help list.
    var onOpenHelp: (String?) -> Void = { _ in }

    @State private var stack: [MealsPage] = [.home]
    @AppStorage("meals.place") private var placeRaw = ""
    private var place: ChosenPlace? { ChosenPlace.decode(placeRaw) }
    @State private var redirected = false
    @Environment(\.openURL) private var openURL

    private func go(_ p: MealsPage) { stack.append(p) }

    private func follow(_ l: MealsLink) {
        switch l.page {
        case .kitchen:
            if let id = l.id { viewModel.openKitchen(branchId: id); stack = [.home, .kitchen] }
        case .subscription:
            if let id = l.id { requireAccount { viewModel.openSubscription(id: id); stack = [.home, .subscription] } }
        case .mine:
            requireAccount { viewModel.loadMine(force: false); stack = [.home, .mine] }
        case .rotation:
            requireAccount { viewModel.startRotation(); stack = [.home, .rotation] }
        case .rotationDetail:
            if let id = l.id { requireAccount { viewModel.openRotation(id: id); stack = [.home, .mine, .rotationDetail] } }
        case .rotationRenew:
            if let id = l.id { requireAccount { viewModel.openRenewal(id: id); stack = [.home, .mine, .rotationRenew] } }
        case .support:
            // Help is native, on the app's session (a banner to /support must
            // not open the web, which has no session and asks for a new code).
            // The host gates it, so the sign-in screen can say "get help"
            // rather than "subscribe".
            onOpenHelp(l.id)
            stack = [.home]
        case .collection:
            if let id = l.id { viewModel.openCollection(slug: id); stack = [.home, .collection] }
        default:
            stack = [.home]
        }
    }
    private func back() { if stack.count > 1 { stack.removeLast() } }

    /// A banner's link: a meals page opens in the tab, anything else in the browser.
    private func openBannerLink(_ url: String) {
        if url.hasPrefix("/"), let l = MealsLinks.shared.parse(url: url) { follow(l); return }
        let full = url.hasPrefix("/") ? Config.webBaseUrl + (isArabicLocale ? "/ar" : "/en") + url : url
        if let u = URL(string: full) { openURL(u) }
    }

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
                    place: place,
                    onPlace: { placeRaw = $0?.encoded ?? "" },
                    onCollection: { slug in viewModel.openCollection(slug: slug); go(.collection) },
                    onLink: openBannerLink
                )
            case .collection:
                CollectionPage(vm: viewModel, onBack: back) { id in viewModel.openKitchen(branchId: id); go(.kitchen) }
            case .kitchen:
                KitchenPage(vm: viewModel, onBack: back,
                            onTaste: { mark in requireAccount { viewModel.setKitchenTaste(sentiment: mark) } }) { plan in
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
            case .rotationRenew:
                RotationRenewPage(vm: viewModel, onBack: back, onPaid: { go(.paid) }, openPayment: openPayment)
            case .paid:
                PaidPage(redirected: redirected, pending: viewModel.checkout.value.paymentPending) {
                    viewModel.loadMine(force: true)
                    stack = [.home, .mine]
                }
            case .mine:
                MinePage(vm: viewModel, onBack: back, openPayment: openPayment,
                         onOpenRotation: { id in viewModel.openRotation(id: id); go(.rotationDetail) },
                         onRenew: { id in viewModel.openRenewal(id: id); go(.rotationRenew) },
                         onKitchen: { id in viewModel.openKitchen(branchId: id); go(.kitchen) },
                         onLink: { url in if let l = MealsLinks.shared.parse(url: url) { follow(l) } }) { id in
                    viewModel.openSubscription(id: id)
                    go(.subscription)
                }
            case .subscription:
                SubscriptionPage(
                    vm: viewModel, onBack: back, openPayment: openPayment,
                    onOpen: { id in viewModel.openSubscription(id: id); go(.subscription) },
                    onRotation: { requireAccount { viewModel.startRotation(); go(.rotation) } }
                )
            case .rotationDetail:
                RotationDetailPage(vm: viewModel, onBack: back, openPayment: openPayment) { id in
                    viewModel.openSubscription(id: id)
                    go(.subscription)
                }
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
        .task(id: link.map { "\($0.page.name):\($0.id ?? "")" }) {
            if let l = link { follow(l); onLinkHandled() }
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

private struct VarietyBadge: View {
    let weeks: Int

    var body: some View {
        Text("↻ " + tr("meals_no_repeats", weeks))
            .font(.system(size: TypeScale.caption, weight: .bold))
            .foregroundStyle(Warm.ink)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(Warm.saffron.opacity(0.25), in: Capsule())
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
    var enabled: Bool = true
    let onChange: (Bool) -> Void

    var body: some View {
        Toggle(isOn: Binding(get: { on }, set: onChange)) {
            Text("meals_auto_renew")
                .font(.system(size: TypeScale.body))
                .foregroundStyle(MP.text)
        }
        .tint(Warm.chilli)
        .disabled(!enabled)
    }
}

// MARK: - Home

private struct MealsHomePage: View {
    let vm: MealsViewModel
    let onKitchen: (String) -> Void
    let onRotation: () -> Void
    let place: ChosenPlace?
    let onPlace: (ChosenPlace?) -> Void
    var onCollection: (String) -> Void = { _ in }
    var onLink: (String) -> Void = { _ in }

    @State private var current: StorefrontUiState?
    @State private var picking = false
    @State private var query = ""
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
                    if let strip = s.sections.first(where: { $0.type == "cuisine_strip" }) { cuisineStrip(strip) }
                    nearHeader
                    refreshNote
                    if s.kitchens.isEmpty {
                        if s.filtering {
                            EmptyBlock(message: NSLocalizedString("meals_filters_empty", comment: ""))
                            Chip(label: NSLocalizedString("meals_clear_filters", comment: ""), selected: false) {
                                query = ""
                                vm.clearFilters()
                            }
                            .frame(maxWidth: .infinity)
                        } else {
                            EmptyBlock(message: NSLocalizedString("meals_none", comment: ""))
                        }
                    }
                    ForEach(Array(s.kitchens.prefix(3)), id: \.branchId) { k in kitchenButton(k) }
                    if !s.filtering && s.rotationAvailable { rotationPromo }
                    ForEach(Array(s.kitchens.dropFirst(3)), id: \.branchId) { k in kitchenButton(k) }
                    if !s.filtering {
                        ForEach(s.sections.filter { $0.type == "banner_carousel" }, id: \.id) { section in sectionView(section) }
                        if !s.dishes.isEmpty { dishRail }
                        ForEach(s.sections.filter { $0.type == "kitchen_rail" }, id: \.id) { section in sectionView(section) }
                        officeLink
                    }
                }
                Color.clear.frame(height: navBarHeight + Space.xl)
            }
        }
        .task {
            query = vm.store.value.query
            for await state in vm.store { current = state }
        }
        .onChange(of: query) { _, q in vm.setQuery(query: q) }
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
        if s.filtering && s.kitchens.isEmpty {
            title = NSLocalizedString("meals_no_matches", comment: "")
        } else if let p = place {
            title = s.kitchens.isEmpty ? tr("meals_none_near", p.name) : trPlural("meals_near_place", s.kitchens.count, p.name)
        } else {
            title = trPlural("meals_near_count", s.kitchens.count)
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
            }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Space.small) {
                    Chip(label: NSLocalizedString("meals_slot_all", comment: ""), selected: s.slot == nil) {
                        vm.selectSlot(slot: nil)
                    }
                    ForEach(["breakfast", "lunch", "dinner", "suhoor", "iftar"], id: \.self) { slot in
                        Chip(label: slotLabel(slot), selected: s.slot == slot) { vm.selectSlot(slot: slot) }
                    }
                }
            }
            discoveryFilters
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
                HStack(alignment: .firstTextBaseline) {
                    if let title { SectionTitle(text: title) }
                    if let slug = section.collection?.slug {
                        Button { onCollection(slug) } label: {
                            Text("meals_see_all")
                                .font(.system(size: TypeScale.body, weight: .semibold))
                                .foregroundStyle(Warm.chilli)
                                .frame(minHeight: Space.touch)
                        }
                        .padding(.horizontal, Space.edge)
                    }
                }
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
                        ForEach(banners, id: \.id) { b in bannerCard(b) }
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

// MARK: - Home discovery

extension MealsHomePage {
    /// Kitchen/dish search and the rotation-kitchens toggle, under the slot chips.
    fileprivate var discoveryFilters: some View {
        VStack(alignment: .leading, spacing: Space.small) {
            TextField(NSLocalizedString("meals_search_kitchens", comment: ""), text: $query)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .font(.system(size: TypeScale.body))
                .padding(.horizontal, Space.medium)
                .padding(.vertical, 12)
                .background(MP.surface, in: RoundedRectangle(cornerRadius: 10))
                .overlay(RoundedRectangle(cornerRadius: 10).stroke(MP.line, lineWidth: 1))
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Space.small) {
                    Chip(label: NSLocalizedString("meals_rotation_filter", comment: ""), selected: s.rotationOnly) {
                        vm.setRotationOnly(on: !s.rotationOnly)
                    }
                    if let c = s.cuisine {
                        Chip(label: tagLabel(c) + " ×", selected: true) { vm.selectCuisine(cuisine: nil) }
                    }
                }
            }
        }
    }

    /// A refetch over a list still on screen: a spinner while it runs, the
    /// failure with Retry when it fails, so an old list never passes for the
    /// answer to a new place or filter.
    @ViewBuilder
    fileprivate var refreshNote: some View {
        if let failure = s.error {
            FailureStrip(failure: failure, onRetry: { vm.loadStore() })
                .padding(.horizontal, Space.edge)
        } else if s.loading {
            HStack(spacing: Space.small) {
                ProgressView().tint(Warm.chilli)
                Text("meals_updating")
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(MP.textMuted)
            }
            .padding(.horizontal, Space.edge)
            .padding(.vertical, Space.small)
        }
    }

    /// cuisine_strip: tap a cuisine to filter the list by it.
    @ViewBuilder
    fileprivate func cuisineStrip(_ section: HomeSection) -> some View {
        // A cuisine every kitchen serves narrows nothing, so it isn't offered.
        let total = Int(s.unfilteredCount)
        let tiles = (section.cuisines ?? []).filter {
            $0.cuisine == s.cuisine || ($0.kitchens > 0 && (total == 0 || Int($0.kitchens) < total))
        }
        if tiles.count > 1 {
            SectionTitle(text: section.settings.title.map { pick($0, section.settings.titleAr) }
                ?? NSLocalizedString("meals_cuisines", comment: ""))
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Space.small) {
                    ForEach(tiles, id: \.cuisine) { c in
                        let on = c.cuisine == s.cuisine
                        Chip(label: tagLabel(c.cuisine), selected: on) { vm.selectCuisine(cuisine: on ? nil : c.cuisine) }
                    }
                }
                .padding(.horizontal, Space.edge)
            }
        }
    }

    /// A merchandising banner: photo, copy and CTA; the card follows its link.
    fileprivate func bannerCard(_ b: Banner) -> some View {
        let link = b.linkUrl.flatMap { $0.isEmpty ? nil : $0 }
        let cta = b.cta.map { pick($0, b.ctaAr) }
        return Button { if let link { onLink(link) } } label: {
            ZStack(alignment: .bottomLeading) {
                Warm.chilli
                if let img = b.imageUrl, !img.isEmpty {
                    FoodPhoto(seed: b.title, imageUrl: img)
                    LinearGradient(colors: [Color.clear, Color.black.opacity(0.65)], startPoint: .top, endPoint: .bottom)
                }
                VStack(alignment: .leading, spacing: 4) {
                    Text(pick(b.title, b.titleAr)).font(display(TypeScale.heading)).foregroundStyle(Color.white)
                    if let body = b.body {
                        Text(pick(body, b.bodyAr))
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Color.white.opacity(0.8))
                    }
                    if let cta, link != nil {
                        Text(cta + " →")
                            .font(.system(size: TypeScale.body, weight: .semibold))
                            .foregroundStyle(Warm.saffron)
                    }
                }
                .multilineTextAlignment(.leading)
                .padding(Space.large)
            }
            .frame(width: 300, height: 160)
            .clipShape(RoundedRectangle(cornerRadius: 16))
        }
        .buttonStyle(.plain)
        .disabled(link == nil)
    }
}

/// A collection's "See all": its kitchens that deliver to the current point.
private struct CollectionPage: View {
    let vm: MealsViewModel
    let onBack: () -> Void
    let onKitchen: (String) -> Void

    @State private var current: CollectionUiState?
    private var s: CollectionUiState { current ?? vm.collection.value }

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: s.collection.map { pick($0.title, $0.titleAr) } ?? "", onBack: onBack)
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if s.gone {
                        EmptyBlock(message: NSLocalizedString("meals_collection_gone", comment: ""))
                    } else if let failure = s.error {
                        FailureBlock(failure: failure) { vm.retryCollection() }
                    } else if let c = s.collection {
                        if let sub = c.subtitle {
                            Text(pick(sub, c.subtitleAr))
                                .font(.system(size: TypeScale.secondary))
                                .foregroundStyle(MP.textMuted)
                                .padding(.horizontal, Space.edge)
                        }
                        if c.kitchens.isEmpty {
                            EmptyBlock(message: NSLocalizedString("meals_none", comment: ""))
                        }
                        ForEach(c.kitchens, id: \.branchId) { k in
                            Button { onKitchen(k.branchId) } label: { KitchenCard(k: k) }
                                .buttonStyle(.plain)
                        }
                    } else {
                        LoadingBlock()
                    }
                    Color.clear.frame(height: navBarHeight + Space.xl)
                }
            }
        }
        .task {
            for await state in vm.collection { current = state }
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
                        if k.menuWeeks > 1 { VarietyBadge(weeks: Int(k.menuWeeks)) }
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
    let onTaste: (String?) -> Void
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
                KitchenBody(k: k, reviews: s.reviews, offers: s.offers, reviewsError: s.reviewsError,
                            taste: s.taste, tasteBusy: s.tasteBusy, tasteError: s.tasteError, onTaste: onTaste,
                            onRetryReviews: { vm.retryReviews() }, onBack: onBack, onSubscribe: onSubscribe)
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
    let offers: [PromoOffer]
    let reviewsError: Failure?
    let taste: String?
    let tasteBusy: Bool
    let tasteError: Failure?
    let onTaste: (String?) -> Void
    let onRetryReviews: () -> Void
    let onBack: () -> Void
    let onSubscribe: (MealPlan) -> Void

    @State private var day: Int32?
    @State private var weekIndex: Int = 0
    /// Plan-area rows currently on screen; the sticky bar hides while any is.
    @State private var planRows: Set<String> = []

    var body: some View {
        let menuWeeks = KitchenMenu.shared.menuWeeks(k: k)
        let weekSlots = KitchenMenu.shared.weekSlots(k: k, index: Int32(weekIndex))
        let byDay = Dictionary(grouping: weekSlots, by: { $0.weekday })
        let days = byDay.keys.sorted()
        let opening = KitchenMenu.shared.openingDay(slots: weekSlots, weekIndex: Int32(weekIndex)).map { $0.int32Value } ?? days.first
        let selected = day.flatMap { byDay[$0] != nil ? $0 : nil } ?? opening
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
                            TasteRow(taste: taste, busy: tasteBusy, error: tasteError, onTaste: onTaste)
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
                                if k.menuWeeks > 1 { VarietyBadge(weeks: Int(k.menuWeeks)) }
                            }
                            if !offers.isEmpty { OfferChips(offers: offers) }
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
                            if !menuWeeks.isEmpty {
                                ScrollView(.horizontal, showsIndicators: false) {
                                    HStack(spacing: Space.small) {
                                        ForEach(Array(menuWeeks.enumerated()), id: \.offset) { i, w in
                                            DayChip(label: weekTitle(i) + " · " + KitchenMenu.shared.weekRange(startsOn: w.startsOn, arabic: isArabicLocale),
                                                    selected: i == weekIndex, hasChoice: false) {
                                                weekIndex = i
                                                day = nil
                                            }
                                        }
                                    }
                                    .padding(.horizontal, Space.edge)
                                    .padding(.bottom, Space.small)
                                }
                            }
                            ScrollView(.horizontal, showsIndicators: false) {
                                HStack(spacing: Space.small) {
                                    ForEach(days, id: \.self) { d in
                                        DayChip(label: weekday(d), selected: d == selected, hasChoice: KitchenMenu.shared.hasChoiceIn(slots: weekSlots, day: d)) { day = d }
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
                            if let reviewsError {
                                SectionTitle(text: NSLocalizedString("meals_reviews", comment: ""))
                                Text("meals_reviews_failed")
                                    .font(.system(size: TypeScale.secondary))
                                    .foregroundStyle(Warm.ink)
                                    .padding(.horizontal, Space.edge)
                                FailureStrip(failure: reviewsError, onRetry: onRetryReviews)
                            }
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
                                        if let reply = r.reply, !reply.isEmpty {
                                            VStack(alignment: .leading, spacing: 2) {
                                                Text("meals_kitchen_reply")
                                                    .font(.system(size: TypeScale.caption, weight: .bold))
                                                    .foregroundStyle(Warm.ink)
                                                Text(reply).font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.ink)
                                            }
                                            .padding(10)
                                            .frame(maxWidth: .infinity, alignment: .leading)
                                            .background(Warm.paper, in: RoundedRectangle(cornerRadius: 10))
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
                    DishFacts(d: main)
                }
                if let a = slot.alternative, slot.`default` != nil {
                    HStack(spacing: Space.small) {
                        FoodPhoto(seed: pick(a.name, a.nameAr), imageUrl: a.imageUrl)
                            .frame(width: 40, height: 40)
                            .clipShape(Circle())
                        VStack(alignment: .leading, spacing: 2) {
                            Text(tr("meals_or", pick(a.name, a.nameAr)))
                                .font(.system(size: TypeScale.secondary))
                                .foregroundStyle(Warm.muted)
                            DishFacts(d: a)
                        }
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

/// "640 kcal · Allergens: Gluten, Sesame" under a dish name; nothing when neither was entered.
private struct DishFacts: View {
    let d: MenuChoice

    var body: some View {
        let allergens = KitchenMenu.shared.allergenLine(choice: d, arabic: isArabicLocale)
        let parts = [
            d.calories.map { tr("meals_kcal", Int($0.int32Value)) },
            allergens.isEmpty ? nil : tr("meals_allergens", allergens),
        ].compactMap { $0 }
        if !parts.isEmpty {
            Text(parts.joined(separator: " · "))
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Warm.muted)
        }
    }
}

/// A weekday tab; `hasChoice` adds the dot the choice help text refers to (web: ms-day-dot).
private struct DayChip: View {
    let label: String
    let selected: Bool
    let hasChoice: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Text(label)
                    .font(.system(size: TypeScale.secondary, weight: selected ? .semibold : .regular))
                    .foregroundStyle(selected ? Color.white : MP.text)
                if hasChoice {
                    Circle()
                        .fill(selected ? Color.white : Warm.chilli)
                        .frame(width: 7, height: 7)
                        .accessibilityHidden(true)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .background(selected ? Warm.chilli : MP.surface, in: Capsule())
            .overlay(Capsule().stroke(selected ? Color.clear : MP.line, lineWidth: 1))
        }
        .accessibilityLabel(hasChoice ? label + ", " + NSLocalizedString("meals_two_choices", comment: "") : label)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// The kitchen's public promos and trials, as on the web page; applied at checkout.
private struct OfferChips: View {
    let offers: [PromoOffer]

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(offers, id: \.id) { o in
                VStack(alignment: .leading, spacing: 2) {
                    let label = offerLabel(o)
                    if !label.isEmpty {
                        Text(label)
                            .font(.system(size: TypeScale.secondary, weight: .bold))
                            .foregroundStyle(Warm.ink)
                    }
                    Text(pick(o.name, o.nameAr)
                         + (o.newCustomersOnly ? " · " + NSLocalizedString("meals_offer_new_only", comment: "") : ""))
                        .font(.system(size: TypeScale.caption))
                        .foregroundStyle(Warm.muted)
                }
                .padding(.horizontal, 12)
                .padding(.vertical, 8)
                .background(Warm.saffronSoft, in: RoundedRectangle(cornerRadius: 12))
                .overlay(RoundedRectangle(cornerRadius: 12).stroke(Warm.saffron, lineWidth: 1))
            }
        }
    }

    private func offerLabel(_ o: PromoOffer) -> String {
        var parts: [String] = []
        if let days = o.trialDays?.intValue, days > 0 { parts.append(tr("meals_offer_trial", days)) }
        switch o.discountType {
        case "percent":
            let pct = KitchenMenu.shared.percent(bp: o.percentBp)
            if let cap = o.maxDiscountMinor?.int64Value, cap > 0 {
                parts.append(tr("meals_offer_percent_cap", pct, money(cap, o.currency)))
            } else {
                parts.append(tr("meals_offer_percent", pct))
            }
        case "fixed":
            parts.append(tr("meals_offer_fixed", money(o.amountMinor, o.currency)))
        default:
            break
        }
        return parts.joined(separator: " · ")
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
    @State private var promo = ""

    private var promoCode: String? {
        guard let failure = s.error, case .rejected(let r) = onEnum(of: failure) else { return nil }
        return r.fieldCodes["promo_code"]
    }

    private var startCode: String? {
        guard let failure = s.error, case .rejected(let r) = onEnum(of: failure) else { return nil }
        return r.fieldCodes["start_date"]
    }

    private var addressCode: String? {
        guard let failure = s.error, case .rejected(let r) = onEnum(of: failure) else { return nil }
        return r.fieldCodes["address_id"]
    }

    /// No total, no amount: "Pay " with a dangling space read as a bug.
    private var payLabel: String {
        guard let p = s.preview else { return NSLocalizedString("meals_pay_unavailable", comment: "") }
        return tr("meals_pay", money(p.totalMinor, p.currency))
    }

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: NSLocalizedString("meals_checkout", comment: ""), onBack: onBack)
            if let plan = s.plan {
                form(plan)
                PrimaryButton(
                    label: payLabel,
                    enabled: s.preview != nil && !s.busy
                ) { vm.subscribe() }
                .padding(Space.edge)
                Color.clear.frame(height: navBarHeight)
            } else {
                Spacer()
            }
        }
        .task {
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
                // Under the addresses rather than in the generic strip at the
                // bottom: the fix is to pick another one, right here.
                if let code = addressCode {
                    Text(NSLocalizedString(code == "not_found" ? "meals_address_gone" : "meals_address_outside", comment: ""))
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(MP.bad)
                        .padding(.horizontal, Space.edge)
                        .padding(.vertical, 6)
                }
                AddressForm(startOpen: s.addresses.isEmpty, busy: s.busy,
                            added: Int(s.addressAdded), failure: s.error) { label, building, area, city, at in
                    vm.addAddress(label: label, building: building, area: area, city: city, at: at)
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
                StartDatePicker(date: s.startDate, earliest: s.earliestStart) { vm.setStartDate(date: $0) }
                if let reason = startReason(startCode) {
                    Text(reason)
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(MP.bad)
                        .padding(.horizontal, Space.edge)
                        .padding(.vertical, 6)
                }
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
                if s.paymentFailed {
                    Text(NSLocalizedString("meals_pay_failed", comment: ""))
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(MP.bad)
                        .padding(.horizontal, Space.edge)
                        .padding(.vertical, 6)
                }
                if promoCode == nil, startCode == nil, addressCode == nil, let failure = s.error {
                    FailureStrip(failure: failure, onRetry: { vm.retryCheckout() })
                }
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

/// Stays open until the address is saved (the view model bumps `added`), so a
/// failed save keeps what was typed and shows field errors under each box. The
/// point is a fresh fix the customer asked for, never the feed's area or a
/// fallback point, and Save waits for it.
private struct AddressForm: View {
    let startOpen: Bool
    let busy: Bool
    let added: Int
    let failure: Failure?
    let onSave: (String, String, String, String, LatLng) -> Void

    @State private var open = false
    @State private var label = ""
    @State private var building = ""
    @State private var area = ""
    @State private var city = ""
    @State private var pinLat: Double?
    @State private var pinLng: Double?
    @State private var locating = false
    @State private var pinFailed = false
    @StateObject private var fix = FreshFix()

    private var fields: [String: String] {
        guard let failure, case .rejected(let r) = onEnum(of: failure) else { return [:] }
        return r.fields
    }

    @ViewBuilder
    private func fieldError(_ key: String) -> some View {
        if let e = fields[key] {
            Text(e).font(.system(size: TypeScale.secondary)).foregroundStyle(MP.bad)
        }
    }

    var body: some View {
        Group { content }
            .onChange(of: added) { _, _ in
                open = false
                label = ""; building = ""; area = ""; city = ""
                pinLat = nil; pinLng = nil
            }
    }

    @ViewBuilder
    private var content: some View {
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
                fieldError("label")
                InputField(placeholder: NSLocalizedString("meals_building", comment: ""), text: $building)
                InputField(placeholder: NSLocalizedString("meals_area", comment: ""), text: $area)
                InputField(placeholder: NSLocalizedString("meals_city", comment: ""), text: $city)
                fieldError("city")
                Text(pinText)
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(MP.textMuted)
                if pinFailed {
                    Text("meals_pin_failed").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.bad)
                }
                fieldError("point")
                SecondaryButton(label: NSLocalizedString("meals_pin_here", comment: "")) {
                    guard !locating else { return }
                    locating = true
                    pinFailed = false
                    fix.request { point in
                        locating = false
                        pinFailed = point == nil
                        if let point { pinLat = point.0; pinLng = point.1 }
                    }
                }
                PrimaryButton(
                    label: NSLocalizedString("meals_save_address", comment: ""),
                    enabled: !busy && !label.trimmingCharacters(in: .whitespaces).isEmpty
                        && !city.trimmingCharacters(in: .whitespaces).isEmpty && pinLat != nil && pinLng != nil
                ) {
                    guard let lat = pinLat, let lng = pinLng else { return }
                    onSave(label, building, area, city, LatLng(lat: lat, lng: lng))
                }
            }
        }
    }

    private var pinText: String {
        if locating { return NSLocalizedString("meals_pinning", comment: "") }
        if let lat = pinLat, let lng = pinLng {
            return tr("meals_pinned", String(format: "%.4f, %.4f", lat, lng))
        }
        return NSLocalizedString("meals_pin_needed", comment: "")
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
                    if let a = s.adventure {
                        AdventureChips(current: a, enabled: true) { vm.setBuilderAdventure(adventure: $0) }
                    }
                    FieldLabel(text: tr("meals_pick_four", s.picked.count))
                    if s.busy && s.candidates.isEmpty { LoadingBlock() }
                    if s.candidatesLoaded && !s.tooFewKitchens && s.addressId != nil {
                        SecondaryButton(label: NSLocalizedString(s.suggesting ? "meals_balancing" : "meals_balanced", comment: "")) {
                            vm.suggestRotation()
                        }
                        .padding(.horizontal, Space.edge)
                        .padding(.vertical, Space.small)
                        if s.suggestNone {
                            Text(NSLocalizedString("meals_balanced_none", comment: ""))
                                .font(.system(size: TypeScale.secondary))
                                .foregroundStyle(MP.textMuted)
                                .padding(.horizontal, Space.edge)
                                .padding(.vertical, Space.small)
                        }
                        if let failure = s.suggestError {
                            FailureStrip(failure: failure, onRetry: { vm.suggestRotation() })
                        }
                    }
                    ForEach(s.candidates, id: \.branchId) { k in
                        let price = s.plans[k.branchId]?.rotationWeekPriceMinor?.int64Value
                            ?? k.rotationWeekPriceMinor?.int64Value ?? 0
                        Choice(
                            title: pick(k.name, k.nameAr),
                            detail: money(price, k.currency),
                            selected: s.picked.contains(k.branchId)
                        ) { vm.togglePick(branchId: k.branchId) }
                    }
                    if s.tooFewKitchens {
                        Text(tr("meals_rotation_few", s.candidates.count))
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(MP.textMuted)
                            .padding(.horizontal, Space.edge)
                            .padding(.vertical, Space.small)
                        SecondaryButton(label: NSLocalizedString("meals_change_location", comment: ""), action: onBack)
                            .padding(.horizontal, Space.edge)
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
                        if let v = q.variety {
                            VarietyBox(v: v)
                        }
                    }
                    // Before the kitchens load, retry loads them; after, the pay button is the retry.
                    if let failure = s.error {
                        if s.candidatesLoaded {
                            FailureStrip(failure: failure)
                        } else {
                            FailureStrip(failure: failure, onRetry: { vm.startRotation() })
                        }
                    }
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
                if state.paid { onPaid() }
            }
        }
    }
}

/// The quote's advisory variety check: a three-step meter and plain-language warnings.
private struct VarietyBox: View {
    let v: RotationVariety

    private var tone: Color { v.level == "great" ? Warm.leaf : v.level == "good" ? Warm.saffron : Warm.chilli }

    private func text(_ w: RotationVarietyWarning) -> String {
        let dish = pick(w.dish, w.dishAr)
        switch w.code {
        case "same_cuisine":
            let names = w.cuisines.map { tagLabel($0) }.joined(separator: isArabicLocale ? " و" : " and ")
            return tr("meals_variety_same_cuisine", Int(w.kitchens), names)
        case "repeated_dish":
            return tr("meals_variety_repeated_dish", dish, weekday(w.weekday?.int32Value ?? 0), Int(w.kitchens))
        default:
            return tr("meals_variety_across_weeks", dish, Int(w.kitchens))
        }
    }

    var body: some View {
        CardBox {
            HStack {
                Text(NSLocalizedString("meals_variety_" + v.level, comment: ""))
                    .font(.system(size: TypeScale.body, weight: .semibold))
                    .foregroundStyle(Warm.ink)
                Spacer()
                ForEach(1...3, id: \.self) { i in
                    Capsule()
                        .fill(i <= Int(v.steps) ? tone : Warm.line)
                        .frame(width: 22, height: 6)
                }
            }
            Text(NSLocalizedString("meals_variety_hint_" + v.level, comment: ""))
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Warm.muted)
            ForEach(Array(v.warnings.enumerated()), id: \.offset) { _, w in
                Text("• " + text(w))
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Warm.ink)
            }
            if v.level != "great" {
                Text(NSLocalizedString("meals_variety_advisory", comment: ""))
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Warm.muted)
            }
        }
        .padding(.top, Space.medium)
    }
}

// MARK: - Paid, My meals, subscription

private struct PaidPage: View {
    let redirected: Bool
    var pending = false
    let onMine: () -> Void

    var body: some View {
        VStack(spacing: Space.large) {
            Text(pending && !redirected ? "…" : "✓").font(.system(size: TypeScale.title * 2.5)).foregroundStyle(Warm.leaf)
            Text(LocalizedStringKey(redirected ? "meals_paying" : pending ? "meals_pay_pending" : "meals_paid"))
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
    let openPayment: (String) -> Void
    let onOpenRotation: (String) -> Void
    let onRenew: (String) -> Void
    let onKitchen: (String) -> Void
    let onLink: (String) -> Void
    let onOpen: (String) -> Void

    @State private var current: MyMealsUiState?
    private var s: MyMealsUiState { current ?? vm.mine.value }
    @Environment(\.openURL) private var openURL

    private func companyCard(_ name: String) -> some View {
        VStack(alignment: .leading, spacing: Space.small) {
            Text(String(format: NSLocalizedString("meals_company_covers", comment: ""), name))
                .font(.system(size: TypeScale.body))
                .foregroundStyle(Warm.ink)
            Button {
                if let u = URL(string: Config.webBaseUrl + (isArabicLocale ? "/ar" : "/en") + "/business") { openURL(u) }
            } label: {
                Text(NSLocalizedString("meals_company_book_web", comment: ""))
                    .font(.system(size: TypeScale.body, weight: .semibold))
                    .foregroundStyle(Warm.chilli)
                    .frame(minHeight: Space.touch)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(Space.medium)
        .overlay(RoundedRectangle(cornerRadius: 12).stroke(Warm.chilli, lineWidth: 1))
        .padding(.horizontal, Space.edge)
        .padding(.vertical, Space.medium)
    }

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: NSLocalizedString("meals_my", comment: ""), onBack: onBack)
            if let failure = s.error, !s.loaded {
                FailureBlock(failure: failure) { vm.loadMine(force: true) }
                Spacer()
            } else if !s.loaded {
                LoadingBlock()
                Spacer()
            } else if s.subscriptions.isEmpty && s.rotations.isEmpty && s.companies.isEmpty {
                EmptyBlock(message: NSLocalizedString("meals_no_subs", comment: ""))
                Spacer()
            } else {
                if let failure = s.actionError { FailureStrip(failure: failure) }
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        if let failure = s.error {
                            FailureStrip(failure: failure, onRetry: { vm.loadMine(force: true) }, onDismiss: { vm.dismissMineError() })
                        }
                        if let failure = s.extrasError {
                            FailureStrip(failure: failure, onRetry: { vm.loadMine(force: true) })
                        }
                        if !s.nextWeek.isEmpty {
                            NextWeekCard(items: s.nextWeek, onKitchen: onKitchen)
                        }
                        ForEach(s.companies, id: \.companyId) { c in companyCard(c.name) }
                        if !s.rotations.isEmpty {
                            SectionTitle(text: NSLocalizedString("meals_rotations", comment: ""))
                            ForEach(s.rotations, id: \.id) { r in
                                RotationCard(r: r, vm: vm, busy: s.busy.contains(r.id), onOpen: onOpen, onOpenRotation: onOpenRotation, onRenew: onRenew, recap: s.recaps[r.id], onLink: onLink)
                            }
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
            for await state in vm.mine {
                current = state
                if let url = state.paymentUrl { openPayment(url) }
            }
        }
    }
}

private struct RotationCard: View {
    let r: Rotation
    let vm: MealsViewModel
    let busy: Bool
    let onOpen: (String) -> Void
    let onOpenRotation: (String) -> Void
    let onRenew: (String) -> Void
    var recap: RotationRecap? = nil
    var onLink: (String) -> Void = { _ in }
    @State private var confirming = false

    var body: some View {
        CardBox {
            // The heading opens the rotation itself: all four weeks, and changing one.
            Button { onOpenRotation(r.id) } label: {
                HStack {
                    Text(r.rotationNo).font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                    Spacer()
                    StatusPill(status: r.status)
                    Image(systemName: "chevron.forward").foregroundStyle(MP.textFaint)
                }
            }
            .buttonStyle(.plain)
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
            if let recap { RecapBlock(recap: recap, onLink: onLink) }
            if r.awaitingPayment {
                PrimaryButton(label: NSLocalizedString("meals_pay_due", comment: ""), enabled: !busy) {
                    vm.payPendingRotation(id: r.id)
                }
                .padding(.top, Space.small)
            }
            if r.live {
                RenewToggle(on: r.autoRenew) { vm.setRotationRenewal(id: r.id, auto: $0) }
                    .disabled(busy)
                RefreshToggle(on: r.refreshOnRenew) { vm.setRotationRefresh(id: r.id, on: $0) }
                    .disabled(busy)
                Button { confirming = true } label: {
                    Text("meals_cancel_rotation").font(.system(size: TypeScale.secondary)).foregroundStyle(busy ? MP.textFaint : MP.bad)
                }
                .disabled(busy)
                .padding(.vertical, Space.small)
            }
            if r.renewable {
                PrimaryButton(label: NSLocalizedString("meals_renew_fresh", comment: ""), enabled: !busy) { onRenew(r.id) }
                    .padding(.top, Space.small)
                Button { vm.renewRotation(id: r.id) } label: {
                    Text("meals_renew_rotation").font(.system(size: TypeScale.secondary)).foregroundStyle(busy ? MP.textFaint : MP.textMuted)
                }
                .disabled(busy)
                .padding(.vertical, Space.small)
            }
        }
        .sheet(isPresented: $confirming) {
            CancelReasonsSheet(
                vm: vm, rotationId: r.id, busy: busy, canPause: false, canGuestWeek: false,
                onRotationBuilder: { confirming = false },
                onChangeWeek: { confirming = false; onOpenRotation(r.id) },
                onCancel: { code, note in confirming = false; vm.cancelRotation(id: r.id, reasonCode: code, note: note) },
                onClose: { confirming = false }
            )
        }
    }
}

private struct RefreshToggle: View {
    let on: Bool
    let onChange: (Bool) -> Void

    var body: some View {
        Toggle(isOn: Binding(get: { on }, set: onChange)) {
            Text("meals_refresh_on_renew")
                .font(.system(size: TypeScale.body))
                .foregroundStyle(MP.text)
        }
        .tint(Warm.chilli)
    }
}

// MARK: - Renewal with fresh kitchens

private struct AdventureChips: View {
    let current: String
    let enabled: Bool
    let onPick: (String) -> Void

    var body: some View {
        FieldLabel(text: NSLocalizedString("meals_adventure", comment: ""))
        HStack(spacing: Space.small) {
            ForEach(["familiar", "balanced", "surprise"], id: \.self) { a in
                Chip(label: NSLocalizedString("meals_adventure_" + a, comment: ""), selected: current == a, enabled: enabled) { onPick(a) }
            }
        }
        .padding(.horizontal, Space.edge)
        Text(LocalizedStringKey("meals_adventure_" + current + "_note"))
            .font(.system(size: TypeScale.secondary))
            .foregroundStyle(MP.textMuted)
            .padding(.horizontal, Space.edge)
            .padding(.vertical, Space.small)
    }
}

private func reasonLabel(_ k: ProposalKitchen) -> String {
    let cuisine = k.reasonCuisine.map { tagLabel($0) }
    switch k.reason {
    case "loved": return NSLocalizedString("meals_reason_loved", comment: "")
    case "top_rated": return NSLocalizedString("meals_reason_top_rated", comment: "")
    case "kept": return NSLocalizedString("meals_reason_kept", comment: "")
    case "new_cuisine":
        if let c = cuisine { return tr("meals_reason_new_cuisine", c) }
        return NSLocalizedString("meals_reason_new_kitchen", comment: "")
    case "familiar_cuisine":
        if let c = cuisine { return tr("meals_reason_familiar_cuisine", c) }
        return NSLocalizedString("meals_reason_new_kitchen", comment: "")
    default:
        return NSLocalizedString(k.inCurrent ? "meals_reason_kept" : "meals_reason_new_kitchen", comment: "")
    }
}

private func eligibleDetail(_ k: ProposalKitchen) -> String {
    var detail = money(k.weekPriceMinor, k.currency) + " " + NSLocalizedString("meals_per_week", comment: "") + starText(k.rating)
    if k.inCurrent { detail += " · " + NSLocalizedString("meals_in_rotation", comment: "") }
    if k.favourite { detail += " · ♥" }
    return detail
}

private func starText(_ rating: KotlinDouble?) -> String {
    guard let r = rating?.doubleValue else { return "" }
    return " · ★ " + String(format: "%.1f", r)
}

/// The proposed next four kitchens, each swappable, with the total and the pay step.
private struct RotationRenewPage: View {
    let vm: MealsViewModel
    let onBack: () -> Void
    let onPaid: () -> Void
    let openPayment: (String) -> Void

    @State private var current: RenewUiState?
    @State private var swapping: Int? = nil
    private var s: RenewUiState { current ?? vm.renew.value }

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: NSLocalizedString("meals_renew_title", comment: ""), onBack: onBack)
            if let failure = s.error, s.proposal == nil {
                FailureBlock(failure: failure) { vm.retryRenewal() }
                Spacer()
            } else if let p = s.proposal {
                let chosen = s.chosen
                let repeats = Set(chosen.map { $0.branchId }).count < chosen.count
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        Text(NSLocalizedString("meals_renew_lede", comment: "") + " " + tr("meals_renew_starts", shortDate(p.startDate)))
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(MP.textMuted)
                            .padding(.horizontal, Space.edge)
                            .padding(.vertical, Space.small)
                        AdventureChips(current: p.adventure, enabled: !s.busy) { vm.setRenewalAdventure(adventure: $0) }
                        ForEach(Array(chosen.enumerated()), id: \.offset) { i, k in
                            CardBox {
                                HStack(alignment: .top, spacing: Space.small) {
                                    Circle().fill(Warm.weeks[i % 4]).frame(width: 10, height: 10).padding(.top, 5)
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(tr("meals_week", i + 1) + " · " + pick(k.kitchenName, k.kitchenNameAr))
                                            .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                                        Text(reasonLabel(k) + starText(k.rating))
                                            .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                                    }
                                    Spacer()
                                    VStack(alignment: .trailing, spacing: 2) {
                                        Text(money(k.weekPriceMinor, k.currency))
                                            .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                                        if k.mealPriceMinor > 0 {
                                            Text(tr("meals_per_meal", money(k.mealPriceMinor, k.currency)))
                                                .font(.system(size: TypeScale.caption)).foregroundStyle(MP.textFaint)
                                        }
                                    }
                                }
                                SecondaryButton(label: NSLocalizedString(swapping == i ? "meals_swap_done" : "meals_swap", comment: "")) {
                                    swapping = swapping == i ? nil : i
                                }
                                .padding(.top, Space.small)
                            }
                        }
                        if repeats {
                            Text("meals_renew_repeats")
                                .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                                .padding(.horizontal, Space.edge)
                        }
                        if let slot = swapping {
                            FieldLabel(text: tr("meals_pick_for_week", slot + 1))
                            ForEach(p.eligible, id: \.planId) { k in
                                let picked = slot < s.picks.count ? s.picks[slot] : nil
                                let taken = s.picks.contains(k.planId) && picked != k.planId
                                Choice(title: pick(k.kitchenName, k.kitchenNameAr), detail: eligibleDetail(k), selected: picked == k.planId) {
                                    if !taken && !s.busy {
                                        vm.swapKitchen(slot: Int32(slot), planId: k.planId)
                                        swapping = nil
                                    }
                                }
                                .opacity(taken ? 0.5 : 1)
                            }
                        }
                        CardBox {
                            PriceLine(label: NSLocalizedString("meals_total", comment: ""), value: money(s.totalMinor, s.currency), strong: true)
                            Text(tr("meals_last_rotation", money(p.previousAmountMinor, p.currency)))
                                .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                        }
                        .padding(.top, Space.medium)
                        if let failure = s.actionError {
                            FailureStrip(failure: failure, onRetry: nil, onDismiss: { vm.dismissRenewalError() })
                        }
                    }
                }
                PrimaryButton(
                    label: tr("meals_confirm_pay", money(s.totalMinor, s.currency)),
                    enabled: !s.busy && chosen.count == 4
                ) { vm.confirmRenewal() }
                .padding(Space.edge)
                Color.clear.frame(height: navBarHeight)
            } else {
                LoadingBlock()
                Spacer()
            }
        }
        .task {
            for await state in vm.renew {
                current = state
                if let url = state.paymentUrl { openPayment(url); vm.renewalPaymentOpened() }
                if state.paid { onPaid() }
            }
        }
    }
}

/// Heart / "not again" on a kitchen; the mark is remembered for renewal proposals.
private struct TasteRow: View {
    let taste: String?
    let busy: Bool
    let error: Failure?
    let onTaste: (String?) -> Void

    var body: some View {
        HStack(spacing: Space.small) {
            if taste == "favourite" {
                Text("♥ " + NSLocalizedString("meals_hearted", comment: ""))
                    .font(.system(size: TypeScale.secondary, weight: .semibold)).foregroundStyle(Warm.leaf)
                Spacer()
                Chip(label: NSLocalizedString("meals_undo", comment: ""), selected: false, enabled: !busy) { onTaste(nil) }
            } else if taste == "avoid" {
                Text("meals_kitchen_avoided")
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                Spacer()
                Chip(label: NSLocalizedString("meals_undo", comment: ""), selected: false, enabled: !busy) { onTaste(nil) }
            } else {
                Chip(label: "♡ " + NSLocalizedString("meals_heart", comment: ""), selected: false, enabled: !busy) { onTaste("favourite") }
                Chip(label: NSLocalizedString("meals_not_again", comment: ""), selected: false, enabled: !busy) { onTaste("avoid") }
            }
        }
        if let failure = error {
            Text(failure.words).font(.system(size: TypeScale.caption)).foregroundStyle(MP.bad)
        }
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

// MARK: - One rotation: its four weeks, changing a week's kitchen, cancelling

private struct RotationDetailPage: View {
    let vm: MealsViewModel
    let onBack: () -> Void
    let openPayment: (String) -> Void
    let onOpen: (String) -> Void

    @State private var current: RotationDetailUiState?
    private var s: RotationDetailUiState { current ?? vm.rotationDetail.value }
    @State private var confirming = false

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: NSLocalizedString("meals_rotation_detail_title", comment: ""), onBack: onBack)
            if let r = s.rotation {
                content(r)
            } else if let failure = s.error {
                FailureBlock(failure: failure) { vm.retryOpenRotation() }
                Spacer()
            } else {
                LoadingBlock()
                Spacer()
            }
        }
        .sheet(isPresented: $confirming) {
            if let r = s.rotation {
                CancelReasonsSheet(
                    vm: vm, rotationId: r.id, busy: s.busy, canPause: false, canGuestWeek: false,
                    onRotationBuilder: { confirming = false },
                    // The weeks and their "Change kitchen" buttons are on this very screen.
                    onChangeWeek: { confirming = false },
                    onCancel: { code, note in confirming = false; vm.cancelOpenRotation(reasonCode: code, note: note) },
                    onClose: { confirming = false }
                )
            }
        }
        .task {
            for await state in vm.rotationDetail {
                current = state
                if let url = state.paymentUrl {
                    vm.clearRotationPaymentUrl()
                    openPayment(url)
                }
                if state.cancelled { onBack() }
            }
        }
    }

    private func content(_ r: Rotation) -> some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                CardBox {
                    HStack {
                        Text(r.rotationNo).font(.system(size: TypeScale.heading, weight: .semibold)).foregroundStyle(MP.text)
                        Spacer()
                        StatusPill(status: r.status)
                    }
                    Text(shortDate(r.startDate) + " – " + shortDate(r.endDate))
                        .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                    Text(String(format: NSLocalizedString("meals_rotation_total", comment: ""), money(r.amountMinor, r.currency)))
                        .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                }
                if let failure = s.error { FailureStrip(failure: failure, onRetry: { vm.retryOpenRotation() }) }
                if let failure = s.actionError { FailureStrip(failure: failure, onDismiss: { vm.dismissRotationDetailError() }) }
                ForEach(r.weeks.sorted { $0.week < $1.week }, id: \.week) { w in
                    let picking = s.picking?.int32Value == w.week
                    RotationWeekCard(w: w, currency: r.currency, picking: picking, busy: s.busy, vm: vm, onOpen: onOpen)
                    if picking { SwapPickerCard(w: w, currency: r.currency, s: s, vm: vm) }
                }
                if r.live {
                    Button { confirming = true } label: {
                        Text("meals_cancel_rotation").font(.system(size: TypeScale.secondary)).foregroundStyle(s.busy ? MP.textFaint : MP.bad)
                    }
                    .disabled(s.busy)
                    .padding(Space.edge)
                }
                Color.clear.frame(height: navBarHeight + Space.xl)
            }
        }
    }
}

private struct RotationWeekCard: View {
    let w: RotationWeek
    let currency: String
    let picking: Bool
    let busy: Bool
    let vm: MealsViewModel
    let onOpen: (String) -> Void

    var body: some View {
        CardBox {
            HStack(spacing: Space.small) {
                Circle().fill(Warm.weeks[(Int(w.week) - 1 + 4) % 4]).frame(width: 10, height: 10)
                Text(tr("meals_week", Int(w.week)) + " · " + pick(w.kitchenName, w.kitchenNameAr))
                    .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                Spacer()
                if let status = w.status { StatusPill(status: status) }
            }
            Text(shortDate(w.startsOn) + " – " + shortDate(w.endsOn))
                .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
            Text(money(w.priceMinor, currency)).font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
            if w.swapped {
                Text("meals_kitchen_changed").font(.system(size: TypeScale.caption)).foregroundStyle(MP.textMuted)
            }
            if w.swappable {
                SecondaryButton(label: NSLocalizedString(picking ? "meals_keep" : "meals_change_kitchen", comment: "")) {
                    if busy { return }
                    if picking { vm.closeWeekPicker() } else { vm.pickWeekKitchen(week: w.week) }
                }
                .padding(.top, Space.small)
                if let until = w.swapUntil {
                    Text(String(format: NSLocalizedString("meals_swap_until", comment: ""), shortDate(until)))
                        .font(.system(size: TypeScale.caption)).foregroundStyle(MP.textFaint)
                }
            }
            if let id = w.subscriptionId {
                Button { onOpen(id) } label: {
                    Text("meals_open_week").font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.chilli)
                }
                .padding(.vertical, Space.small)
            }
        }
    }
}

/// The kitchens one week could change to, each with what it costs or credits, and a confirm.
private struct SwapPickerCard: View {
    let w: RotationWeek
    let currency: String
    let s: RotationDetailUiState
    let vm: MealsViewModel

    @State private var chosen: String?
    @State private var confirming = false

    private var pickedOption: RotationSwapOption? { s.options?.items.first { $0.planId == chosen } }

    private func difference(_ minor: Int64, _ currency: String) -> String {
        if minor == 0 { return NSLocalizedString("meals_same_price", comment: "") }
        if minor > 0 { return String(format: NSLocalizedString("meals_pay_more", comment: ""), money(minor, currency)) }
        return String(format: NSLocalizedString("meals_credit_back", comment: ""), money(-minor, currency))
    }

    var body: some View {
        CardBox {
            Text(tr("meals_pick_kitchen_title", Int(w.week)))
                .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
            Text("meals_pick_kitchen_note").font(.system(size: TypeScale.caption)).foregroundStyle(MP.textMuted)
            Text(String(format: NSLocalizedString("meals_current_week_price", comment: ""), money(w.priceMinor, currency)))
                .font(.system(size: TypeScale.caption)).foregroundStyle(MP.textMuted)
            if let failure = s.optionsError {
                FailureStrip(failure: failure, onRetry: { vm.retrySwapOptions() })
            } else if let options = s.options, !s.optionsLoading {
                if options.items.isEmpty {
                    Text("meals_no_swap_options").font(.system(size: TypeScale.body)).foregroundStyle(MP.textMuted)
                } else {
                    ForEach(options.items, id: \.planId) { o in
                        let on = chosen == o.planId
                        Button { chosen = o.planId } label: {
                            HStack(alignment: .top, spacing: Space.small) {
                                Text(on ? "●" : "○").font(.system(size: TypeScale.body)).foregroundStyle(on ? Warm.chilli : MP.textFaint)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(pick(o.kitchenName, o.kitchenNameAr))
                                        .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                                    Text(ratingLine(o)).font(.system(size: TypeScale.caption)).foregroundStyle(MP.textMuted)
                                }
                                Spacer()
                                VStack(alignment: .trailing, spacing: 2) {
                                    Text(money(o.priceMinor, o.currency)).font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                                    Text(difference(o.differenceMinor, o.currency))
                                        .font(.system(size: TypeScale.caption)).foregroundStyle(o.differenceMinor > 0 ? MP.bad : MP.textMuted)
                                }
                            }
                            .padding(.vertical, 6)
                        }
                        .buttonStyle(.plain)
                        .disabled(s.busy)
                    }
                }
            } else {
                LoadingBlock()
            }
            let picked = pickedOption
            PrimaryButton(
                label: NSLocalizedString("meals_confirm_change", comment: "")
                    + (picked.map { " · " + difference($0.differenceMinor, $0.currency) } ?? ""),
                enabled: picked != nil && !s.busy
            ) { confirming = true }
            .padding(.top, Space.small)
        }
        .alert(
            Text(String(format: NSLocalizedString("meals_swap_confirm", comment: ""),
                        pickedOption.map { pick($0.kitchenName, $0.kitchenNameAr) } ?? "")),
            isPresented: $confirming
        ) {
            Button(action: { if let p = pickedOption { vm.swapWeekKitchen(planId: p.planId) } }) { Text("meals_swap_yes") }
            Button(role: .cancel) {} label: { Text("meals_keep") }
        }
    }

    private func ratingLine(_ o: RotationSwapOption) -> String {
        let rated = o.rating.map { String(format: "%.1f ★ · %d", $0.doubleValue, Int(o.ratingCount)) }
            ?? NSLocalizedString("meals_new_kitchen", comment: "")
        return o.cuisines.isEmpty ? rated : rated + " · " + o.cuisines.joined(separator: ", ")
    }
}

private struct SubscriptionPage: View {
    let vm: MealsViewModel
    let onBack: () -> Void
    let openPayment: (String) -> Void
    let onOpen: (String) -> Void
    let onRotation: () -> Void

    @State private var current: SubscriptionUiState?
    private var s: SubscriptionUiState { current ?? vm.sub.value }
    @State private var confirming = false

    var body: some View {
        VStack(spacing: 0) {
            TopBar(title: s.subscription.map { pick($0.kitchenName, $0.kitchenNameAr) } ?? "", onBack: onBack)
            if let sub = s.subscription {
                content(sub)
            } else if let failure = s.error {
                FailureBlock(failure: failure) { vm.retryOpen() }
                Spacer()
            } else {
                LoadingBlock()
                Spacer()
            }
        }
        .sheet(isPresented: $confirming) {
            if let sub = s.subscription {
                CancelReasonsSheet(
                    vm: vm, rotationId: nil, busy: s.busy,
                    canPause: sub.status == "active" && sub.pauseAllowanceDays > 0,
                    canGuestWeek: sub.guestWeekOffer?.eligible == true && sub.guestWeek == nil,
                    onRotationBuilder: { confirming = false; onRotation() },
                    onChangeWeek: { confirming = false },
                    onCancel: { code, note in confirming = false; vm.cancel(reasonCode: code, note: note) },
                    onClose: { confirming = false }
                )
            }
        }
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
                        RenewToggle(on: sub.renewalMode == "auto", enabled: !s.busy) { vm.setAutoRenew(auto: $0) }
                    }
                    if let company = sub.companyName {
                        Text(String(format: NSLocalizedString("meals_company_paid_by", comment: ""), company))
                            .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                    }
                    if sub.pendingOrderId != nil, let company = sub.companyName {
                        Text(String(format: NSLocalizedString("meals_company_unpaid", comment: ""), company))
                            .font(.system(size: TypeScale.body)).foregroundStyle(MP.text)
                    } else if sub.pendingOrderId != nil {
                        PrimaryButton(label: NSLocalizedString("meals_pay_due", comment: ""), enabled: !s.busy) { vm.payPending() }
                    }
                    if sub.canRenew {
                        PrimaryButton(label: NSLocalizedString("meals_renew", comment: ""), enabled: !s.busy) { vm.renew() }
                    }
                }
                if let failure = s.error {
                    FailureStrip(failure: failure, onRetry: { vm.retry() }, onDismiss: { vm.dismissError() })
                }
                let meals = (sub.upcoming + sub.skipped).sorted { ($0.date, $0.cutoffAt) < ($1.date, $1.cutoffAt) }
                if !meals.isEmpty {
                    SectionTitle(text: NSLocalizedString("meals_upcoming", comment: ""))
                    ForEach(meals, id: \.self) { m in
                        if m.skipped {
                            SkippedMealCard(m: m, busy: s.busy, vm: vm)
                        } else {
                            MealCard(m: m, busy: s.busy, vm: vm, avoided: s.avoidedDishes, tasteBusy: s.tasteBusy)
                        }
                    }
                }
                RecentDeliveries(items: sub.recent)
                if sub.rotationId == nil && (sub.status == "paused" || !sub.pauses.isEmpty) {
                    ResumeCard(sub: sub, busy: s.busy, resumesOn: s.resumesOn, vm: vm)
                }
                if sub.status != "pending_payment" && sub.status != "pending" {
                    RateKitchenCard(state: s.review, subscriptionId: sub.id, vm: vm)
                }
                if sub.rotationId == nil {
                    GuestWeekCard(sub: sub, state: s, vm: vm, onOpen: onOpen, onRotation: onRotation)
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

private let isoDay: DateFormatter = {
    let f = DateFormatter()
    f.locale = Locale(identifier: "en_US_POSIX")
    f.calendar = Calendar(identifier: .gregorian)
    f.timeZone = TimeZone(identifier: "UTC")
    f.dateFormat = "yyyy-MM-dd"
    return f
}()

/// Pause dates from date pickers that start at the first day still before its
/// cutoff, so a typo or a locked day never reaches the API.
private struct PauseCard: View {
    let sub: MySubscription
    let busy: Bool
    let vm: MealsViewModel

    @State private var from = Date()
    @State private var to = Date()

    var body: some View {
        let left = max(0, Int(sub.pauseAllowanceDays) - Int(sub.pauseDaysUsed))
        let first = sub.upcoming.first { !$0.locked }?.date
        if let first, let earliest = isoDay.date(from: first) {
            let valid = to >= from
            CardBox {
                Text("meals_pause_title")
                    .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                Text(tr("meals_pause_left", left, Int(sub.pauseAllowanceDays)))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                DatePicker(selection: $from, in: earliest..., displayedComponents: .date) {
                    Text("meals_pause_from").font(.system(size: TypeScale.body)).foregroundStyle(MP.text)
                }
                .environment(\.timeZone, TimeZone(identifier: "UTC")!)
                DatePicker(selection: $to, in: from..., displayedComponents: .date) {
                    Text("meals_pause_to").font(.system(size: TypeScale.body)).foregroundStyle(MP.text)
                }
                .environment(\.timeZone, TimeZone(identifier: "UTC")!)
                if !valid {
                    Text("meals_pause_bad_range").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.bad)
                }
                PrimaryButton(label: NSLocalizedString("meals_pause", comment: ""), enabled: !busy && left > 0 && valid) {
                    vm.pause(from: isoDay.string(from: from), to: isoDay.string(from: to))
                }
            }
            .tint(Warm.chilli)
            .task(id: first) {
                from = earliest
                to = earliest
            }
            .onChange(of: from) { _, day in if to < day { to = day } }
        }
    }
}

private func deliveryLabel(_ status: String) -> String {
    switch status {
    case "scheduled", "dispatched", "en_route", "arrived", "completed", "failed":
        return NSLocalizedString("meals_delivery_" + status, comment: "")
    default:
        return NSLocalizedString("meals_status_other", comment: "")
    }
}

private struct DeliveryPill: View {
    let status: String

    var body: some View {
        let tint: Color = switch status {
        case "completed": Warm.leaf
        case "failed": MP.bad
        case "dispatched", "en_route", "arrived": Warm.chilli
        default: MP.textMuted
        }
        Text(deliveryLabel(status))
            .font(.system(size: TypeScale.caption, weight: .semibold))
            .foregroundStyle(tint)
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .overlay(Capsule().stroke(tint.opacity(0.5), lineWidth: 1))
    }
}

/// "Mon 14:00" in the reader's language, or nil when the timestamp is unreadable.
private func cutoffLabel(_ iso: String) -> String? {
    guard let date = ISO8601DateFormatter().date(from: iso) else { return nil }
    let out = DateFormatter()
    out.locale = mealsLocale
    out.dateFormat = "EEE HH:mm"
    return out.string(from: date)
}

private struct RecentDeliveries: View {
    let items: [MyDelivery]

    var body: some View {
        if !items.isEmpty {
            SectionTitle(text: NSLocalizedString("meals_recent", comment: ""))
            CardBox {
                ForEach(items, id: \.self) { d in
                    HStack {
                        Text(shortDate(d.date) + " · " + slotLabel(d.mealSlot))
                            .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                        Spacer()
                        DeliveryPill(status: d.status)
                    }
                }
            }
        }
    }
}

/// The current and next pauses, and a way to end them early.
private struct ResumeCard: View {
    let sub: MySubscription
    let busy: Bool
    let resumesOn: String?
    let vm: MealsViewModel

    var body: some View {
        CardBox {
            Text("meals_paused_dates")
                .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
            ForEach(sub.pauses, id: \.self) { p in
                Text(shortDate(p.fromDate) + " – " + shortDate(p.toDate))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
            }
            if let resumesOn {
                Text(tr("meals_resumes_on", shortDate(resumesOn)))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.leaf)
            }
            PrimaryButton(label: NSLocalizedString("meals_resume", comment: ""), enabled: !busy) { vm.resume() }
            Text("meals_resume_note").font(.system(size: TypeScale.caption)).foregroundStyle(MP.textMuted)
        }
    }
}

/// A skipped meal stays listed, with a way to put it back before the cutoff.
private struct SkippedMealCard: View {
    let m: UpcomingMeal
    let busy: Bool
    let vm: MealsViewModel

    var body: some View {
        CardBox {
            HStack {
                Text(shortDate(m.date) + " · " + slotLabel(m.mealSlot))
                    .font(.system(size: TypeScale.secondary, weight: .semibold)).foregroundStyle(MP.textFaint)
                Spacer()
                Text("meals_skipped").font(.system(size: TypeScale.caption, weight: .semibold)).foregroundStyle(MP.textMuted)
            }
            if !m.locked {
                Button { vm.unskip(date: m.date, slot: m.mealSlot) } label: {
                    Text("meals_unskip").font(.system(size: TypeScale.secondary, weight: .semibold)).foregroundStyle(Warm.chilli)
                }
                .disabled(busy)
            }
        }
    }
}

/// One review per subscription, once the first delivery has arrived.
private struct RateKitchenCard: View {
    let state: ReviewUiState
    let subscriptionId: String
    let vm: MealsViewModel

    @State private var rating = 0
    @State private var comment = ""

    var body: some View {
        SectionTitle(text: NSLocalizedString("meals_rate_title", comment: ""))
        if let data = state.data {
            if !data.canReview {
                CardBox {
                    Text("meals_rate_after").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                }
            } else {
                form(data)
            }
        } else if let failure = state.error {
            FailureStrip(failure: failure, onRetry: { vm.loadReview(id: subscriptionId) })
        } else {
            LoadingBlock()
        }
    }

    private func form(_ data: MyReview) -> some View {
        CardBox {
            if data.review?.status == "hidden" {
                Text("meals_rate_hidden").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.bad)
            }
            HStack(spacing: Space.small) {
                ForEach(1...5, id: \.self) { n in
                    Button { rating = n } label: {
                        Text("★")
                            .font(.system(size: TypeScale.title))
                            .foregroundStyle(n <= rating ? Warm.chilli : MP.textFaint)
                            .frame(minWidth: Space.touch, minHeight: Space.touch)
                    }
                    .disabled(state.saving)
                    .accessibilityLabel(tr("meals_rate_star", n))
                    .accessibilityAddTraits(rating == n ? .isSelected : [])
                }
            }
            InputField(placeholder: NSLocalizedString("meals_rate_comment", comment: ""), text: $comment)
            if let failure = state.error {
                FailureStrip(failure: failure, onRetry: { vm.saveReview(rating: Int32(rating), comment: comment) })
            }
            if state.saved {
                Text("meals_rate_saved").font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.leaf)
            }
            PrimaryButton(
                label: NSLocalizedString(data.review != nil ? "meals_rate_update" : "meals_rate_save", comment: ""),
                enabled: rating > 0 && !state.saving
            ) { vm.saveReview(rating: Int32(rating), comment: comment) }
        }
        .task(id: data.review?.id) {
            rating = Int(data.review?.rating ?? 0)
            comment = data.review?.comment ?? ""
        }
    }
}

private struct MealCard: View {
    let m: UpcomingMeal
    let busy: Bool
    let vm: MealsViewModel
    var avoided: Set<String> = []
    var tasteBusy: Set<String> = []
    @State private var confirmingSkip = false

    var body: some View {
        let chosen = m.chosenDishId ?? m.options.first { $0.isDefault }?.id
        CardBox {
            HStack {
                Text(shortDate(m.date) + " · " + slotLabel(m.mealSlot))
                    .font(.system(size: TypeScale.secondary, weight: .semibold)).foregroundStyle(Warm.chilli)
                Spacer()
                if let status = m.deliveryStatus { DeliveryPill(status: status) }
                if m.locked {
                    Text("meals_locked").font(.system(size: TypeScale.caption)).foregroundStyle(MP.textFaint)
                } else {
                    Button { confirmingSkip = true } label: {
                        Text("meals_skip").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                            .padding(Space.small)
                    }
                    .disabled(busy)
                }
            }
            if !m.locked, let cutoff = cutoffLabel(m.cutoffAt) {
                Text(tr(m.options.count > 1 ? "meals_change_until" : "meals_skip_until", cutoff))
                    .font(.system(size: TypeScale.caption)).foregroundStyle(MP.textFaint)
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
            // A delivered dish can be marked "not again": remembered, and the default moves off it.
            if m.deliveryStatus == "completed", let dish = chosen {
                let saving = tasteBusy.contains(dish)
                if avoided.contains(dish) {
                    HStack(spacing: Space.small) {
                        Text("meals_dish_avoided").font(.system(size: TypeScale.caption)).foregroundStyle(MP.textMuted)
                        Spacer()
                        Button { vm.setDishAvoided(dishId: dish, avoid: false) } label: {
                            Text("meals_undo").font(.system(size: TypeScale.secondary)).foregroundStyle(saving ? MP.textFaint : Warm.chilli)
                        }
                        .disabled(saving)
                    }
                } else {
                    Button { vm.setDishAvoided(dishId: dish, avoid: true) } label: {
                        Text("meals_not_again").font(.system(size: TypeScale.secondary)).foregroundStyle(saving ? MP.textFaint : MP.textMuted)
                            .padding(.vertical, Space.small)
                    }
                    .disabled(saving)
                }
            }
        }
        .alert(Text("meals_skip_confirm"), isPresented: $confirmingSkip) {
            Button { vm.skip(date: m.date, slot: m.mealSlot) } label: { Text("meals_skip") }
            Button(role: .cancel) {} label: { Text("meals_keep") }
        } message: {
            Text("meals_skip_note")
        }
    }
}

// MARK: - Next-week preview and cycle recap

/// "Lebanese home cooking", from the kitchen's first cuisine tag.
private func cuisineLine(_ cuisines: [String]) -> String {
    guard let first = cuisines.first else { return NSLocalizedString("meals_next_week_home_style", comment: "") }
    return tr("meals_next_week_home_cooking", MealTags.shared.label(tag: first, arabic: isArabicLocale))
}

/// The "Next week" card at the top of My meals: who cooks and what, per day.
private struct NextWeekCard: View {
    let items: [NextWeekPreview]
    let onKitchen: (String) -> Void

    var body: some View {
        if let p = items.first {
            let kitchen = pick(p.kitchenName, p.kitchenNameAr)
            CardBox {
                Button { onKitchen(p.branchId) } label: {
                    HStack(spacing: Space.medium) {
                        KitchenCover(name: kitchen, imageUrl: p.coverImageUrl, cuisines: p.cuisines)
                            .frame(width: 72, height: 72)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(tr("meals_next_week_kicker", shortDate(p.startsOn) + " – " + shortDate(p.endsOn)))
                                .font(.system(size: TypeScale.secondary, weight: .semibold)).foregroundStyle(Warm.chilli)
                            Text(tr("meals_next_week_title", kitchen, cuisineLine(p.cuisines)))
                                .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(Warm.ink)
                                .multilineTextAlignment(.leading)
                            Text(NSLocalizedString("meals_next_week_open", comment: ""))
                                .font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.chilli)
                        }
                        Spacer()
                    }
                }
                .buttonStyle(.plain)
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(alignment: .top, spacing: Space.small) {
                        ForEach(p.meals, id: \.self) { m in
                            let name = m.shown.map { pick($0.name, $0.nameAr) } ?? NSLocalizedString("meals_next_week_no_dishes", comment: "")
                            VStack(alignment: .leading, spacing: 2) {
                                KitchenCover(name: name, imageUrl: m.shown?.imageUrl, cuisines: [])
                                    .frame(width: 112, height: 76)
                                Text(shortDate(m.date) + " · " + slotLabel(m.mealSlot))
                                    .font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.muted)
                                Text(name).font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.ink).lineLimit(2)
                            }
                            .frame(width: 112, alignment: .leading)
                        }
                    }
                }
                if items.count > 1 {
                    Text(tr("meals_next_week_more", items.count - 1))
                        .font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.muted)
                }
            }
        }
    }
}

/// The end-of-cycle recap inside a rotation card, from week 4 on.
private struct RecapBlock: View {
    let recap: RotationRecap
    let onLink: (String) -> Void

    private func stat(_ n: Int32, _ key: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("\(n)").font(.system(size: TypeScale.heading, weight: .bold)).foregroundStyle(Warm.chilli)
            Text(NSLocalizedString(key, comment: "")).font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.muted)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.small) {
            Text(NSLocalizedString(recap.ended ? "meals_recap_ended" : "meals_recap_title", comment: ""))
                .font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(Warm.ink)
            HStack {
                stat(recap.kitchenCount, "meals_recap_kitchens")
                stat(recap.cuisineCount, "meals_recap_cuisines")
                stat(recap.dishesDelivered, "meals_recap_dishes")
                stat(recap.daysSkipped, "meals_recap_skipped")
            }
            if let f = recap.favourite {
                Text(tr("meals_recap_favourite", pick(f.kitchenName, f.kitchenNameAr), Int(f.rating)))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.leaf)
            }
            PrimaryButton(label: NSLocalizedString("meals_recap_renew", comment: ""), enabled: true) { onLink(recap.renewPath) }
        }
        .padding(.top, Space.small)
    }
}

// MARK: - Cancel reasons + guest week (variety retention)

private let cancelReasonCodes = ["bored_of_food", "too_expensive", "food_quality", "delivery_problems", "moving_away", "other"]

private func reasonLabel(_ code: String) -> String {
    switch code {
    case "bored_of_food": return NSLocalizedString("meals_reason_bored", comment: "")
    case "too_expensive": return NSLocalizedString("meals_reason_expensive", comment: "")
    case "food_quality": return NSLocalizedString("meals_reason_quality", comment: "")
    case "delivery_problems": return NSLocalizedString("meals_reason_delivery", comment: "")
    case "moving_away": return NSLocalizedString("meals_reason_moving", comment: "")
    default: return NSLocalizedString("meals_reason_other", comment: "")
    }
}

/// Why are you leaving? One honest save option for the answer, and the cancel
/// button right there under it — no second "are you sure".
private struct CancelReasonsSheet: View {
    let vm: MealsViewModel
    let rotationId: String?
    let busy: Bool
    let canPause: Bool
    let canGuestWeek: Bool
    let onRotationBuilder: () -> Void
    let onChangeWeek: () -> Void
    let onCancel: (String, String) -> Void
    let onClose: () -> Void

    @State private var code: String?
    @State private var note = ""
    @State private var cheaper: CheaperPlans?
    @State private var cheaperLoading = false
    @State private var cheaperError: Failure?

    private var title: String {
        NSLocalizedString(rotationId == nil ? "meals_cancel" : "meals_cancel_rotation", comment: "")
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Space.medium) {
                Text(title).font(.system(size: TypeScale.heading, weight: .semibold)).foregroundStyle(MP.text)
                Text("meals_cancel_why").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                FlowChips(items: cancelReasonCodes, label: reasonLabel, selected: { code == $0 }, enabled: !busy) { code = $0 }
                if let c = code {
                    Text("meals_cancel_note").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                    TextField("", text: $note, axis: .vertical)
                        .lineLimit(2...4)
                        .padding(10)
                        .background(MP.surface, in: RoundedRectangle(cornerRadius: 10))
                        .overlay(RoundedRectangle(cornerRadius: 10).stroke(MP.line, lineWidth: 1))
                    saveOption(c)
                }
                Button { if let c = code { onCancel(c, note) } } label: {
                    Text(title)
                        .font(.system(size: TypeScale.body, weight: .semibold))
                        .foregroundStyle(Color.white)
                        .frame(maxWidth: .infinity, minHeight: 48)
                        .background(code == nil || busy ? MP.bad.opacity(0.35) : MP.bad, in: RoundedRectangle(cornerRadius: 12))
                }
                .disabled(code == nil || busy)
                SecondaryButton(label: NSLocalizedString("meals_keep", comment: ""), action: onClose)
            }
            .padding(Space.edge)
        }
        .task { loadCheaper() }
    }

    private func loadCheaper() {
        cheaperLoading = true
        cheaperError = nil
        if let rid = rotationId {
            vm.loadRotationCheaperPlans(id: rid)
        } else {
            vm.loadCheaperPlans()
        }
        Task {
            if let rid = rotationId {
                for await st in vm.mine where st.cheaperFor == rid {
                    cheaper = st.cheaper
                    cheaperLoading = st.cheaperLoading
                    cheaperError = st.cheaperError
                }
            } else {
                for await st in vm.sub {
                    cheaper = st.cheaper
                    cheaperLoading = st.cheaperLoading
                    cheaperError = st.cheaperError
                }
            }
        }
    }

    @ViewBuilder
    private var pause: some View {
        if canPause {
            Text("meals_save_pause").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
            SecondaryButton(label: NSLocalizedString("meals_save_pause_btn", comment: ""), action: onClose)
        }
    }

    @ViewBuilder
    private func saveOption(_ c: String) -> some View {
        if c == "bored_of_food" || c == "too_expensive" || canPause {
            VStack(alignment: .leading, spacing: Space.small) {
                Text("meals_before_you_go").font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                if c == "bored_of_food" {
                    if rotationId != nil {
                        Text("meals_save_bored_rotation").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                        SecondaryButton(label: NSLocalizedString("meals_save_change_week", comment: ""), action: onChangeWeek)
                    } else {
                        Text("meals_save_bored_single").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                        if canGuestWeek {
                            SecondaryButton(label: NSLocalizedString("meals_save_guest_week", comment: ""), action: onClose)
                        }
                        SecondaryButton(label: NSLocalizedString("meals_save_rotation", comment: ""), action: onRotationBuilder)
                    }
                } else if c == "too_expensive" {
                    if cheaperLoading {
                        LoadingBlock()
                    } else if let failure = cheaperError {
                        FailureStrip(failure: failure, onRetry: { loadCheaper() })
                    } else if let items = cheaper?.items, !items.isEmpty {
                        Text("meals_save_expensive").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                        ForEach(items, id: \.planId) { p in
                            Text(pick(p.kitchenName, p.kitchenNameAr) + " · " + tr("meals_per_meal", money(p.perMealMinor, p.currency)))
                                .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                        }
                    } else {
                        Text("meals_save_expensive_none").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                    }
                    pause
                } else {
                    pause
                }
            }
            .padding(Space.medium)
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(RoundedRectangle(cornerRadius: 12).stroke(Warm.chilli, lineWidth: 1))
        }
    }
}

private struct FlowChips: View {
    let items: [String]
    let label: (String) -> String
    let selected: (String) -> Bool
    let enabled: Bool
    let action: (String) -> Void

    var body: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 140), alignment: .leading)], alignment: .leading, spacing: 6) {
            ForEach(items, id: \.self) { c in
                Chip(label: label(c), selected: selected(c), enabled: enabled) { action(c) }
            }
        }
    }
}

/// The guest week in My meals: the offer, the week itself with both kitchens
/// named, then "make it a rotation".
private struct GuestWeekCard: View {
    let sub: MySubscription
    let state: SubscriptionUiState
    let vm: MealsViewModel
    let onOpen: (String) -> Void
    let onRotation: () -> Void
    @State private var planId: String?

    var body: some View {
        if let g = sub.guestOf {
            CardBox {
                Text("meals_guest_week").font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                Text(tr("meals_guest_of", pick(g.kitchenName, g.kitchenNameAr)))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                SecondaryButton(label: NSLocalizedString("meals_guest_back", comment: "")) { onOpen(g.subscriptionId) }
            }
        } else if let g = sub.guestWeek {
            CardBox {
                Text("meals_guest_week").font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                Text(tr("meals_guest_lede", shortDate(g.weekStart) + " – " + shortDate(g.weekEnd), pick(g.kitchenName, g.kitchenNameAr)))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                if g.status == "pending_payment" {
                    Text("meals_guest_pending").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                }
                SecondaryButton(label: NSLocalizedString(g.status == "pending_payment" ? "meals_pay_due" : "meals_guest_see", comment: "")) {
                    onOpen(g.guestSubscriptionId)
                }
                if g.completed {
                    Text("meals_make_rotation").font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                        .padding(.top, Space.small)
                    Text("meals_make_rotation_lede").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                    PrimaryButton(label: NSLocalizedString("meals_make_rotation_btn", comment: ""), action: onRotation)
                }
            }
        } else if let offer = sub.guestWeekOffer, let from = offer.weekStart, let to = offer.weekEnd {
            if let started = state.guestStarted {
                let k = offer.options.first { $0.weekMinor == started.walletMinor + started.dueMinor }
                CardBox {
                    Text(tr("meals_guest_done", k.map { pick($0.kitchenName, $0.kitchenNameAr) } ?? ""))
                        .font(.system(size: TypeScale.body)).foregroundStyle(Warm.leaf)
                }
            } else if offer.eligible {
                CardBox {
                    Text("meals_guest_offer").font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                    Text(tr("meals_guest_offer_lede", shortDate(from) + " – " + shortDate(to), money(offer.creditMinor, offer.currency)))
                        .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.text)
                    Text("meals_guest_same_days").font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                    ForEach(offer.options, id: \.planId) { o in
                        GuestOptionRow(o: o, selected: planId == o.planId, enabled: !state.busy) { planId = o.planId }
                    }
                    HStack(spacing: Space.small) {
                        PrimaryButton(label: NSLocalizedString("meals_guest_start", comment: ""), enabled: !state.busy && planId != nil) {
                            if let id = planId { vm.startGuestWeek(planId: id) }
                        }
                        SecondaryButton(label: NSLocalizedString("meals_guest_not_now", comment: "")) {
                            if !state.busy { vm.dismissGuestWeek() }
                        }
                    }
                }
            }
        }
    }
}

private struct GuestOptionRow: View {
    let o: GuestWeekOption
    let selected: Bool
    let enabled: Bool
    let action: () -> Void

    private var split: String {
        if o.dueMinor > 0 { return tr("meals_guest_due", money(o.dueMinor, o.currency)) }
        if o.leftoverCreditMinor > 0 { return tr("meals_guest_leftover", money(o.leftoverCreditMinor, o.currency)) }
        return NSLocalizedString("meals_guest_covered", comment: "")
    }

    var body: some View {
        Button(action: action) {
            VStack(alignment: .leading, spacing: 2) {
                Text(pick(o.kitchenName, o.kitchenNameAr)).font(.system(size: TypeScale.body, weight: .semibold)).foregroundStyle(MP.text)
                if !o.cuisines.isEmpty {
                    Text(o.cuisines.joined(separator: " · ")).font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                }
                Text(tr("meals_per_meal", money(o.perMealMinor, o.currency)) + " · " + tr("meals_guest_week_price", money(o.weekMinor, o.currency)))
                    .font(.system(size: TypeScale.secondary)).foregroundStyle(MP.textMuted)
                Text(split).font(.system(size: TypeScale.secondary)).foregroundStyle(Warm.leaf)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Space.medium)
            .padding(.vertical, 10)
            .background(MP.surface, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).stroke(selected ? Warm.chilli : MP.line, lineWidth: 1))
        }
        .disabled(!enabled)
        .accessibilityAddTraits(selected ? [.isSelected] : [])
    }
}
