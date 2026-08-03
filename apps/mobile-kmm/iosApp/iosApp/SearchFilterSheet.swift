import SwiftUI
import Shared

/// The facets the classifieds index has always accepted.
///
/// Price, year, mileage, spec, body type and seller kind — plus sort, kept
/// visually apart because it reorders rather than hides, and conflating the two
/// is how someone ends up unable to find the filter excluding their car.
///
/// Fuel type and transmission are accepted by the server and deliberately not
/// offered: in this market a used-car buyer filters on spec and mileage first
/// and almost never on gearbox, and every extra row is one more thing between
/// them and the results.
///
/// Values are hard-coded, unlike the feed's vertical chips which are served.
/// The difference is real: a vertical appears when a provider registers, while
/// a new regional spec is a CHECK constraint — already an app release's worth of
/// coordination.
struct SearchFilterSheet: View {
    let viewModel: MeViewModel
    let draft: SearchFilters
    let hasLocation: Bool

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    section("filters_price")
                    HStack(spacing: Space.small) {
                        // Minor units on the wire, major units on screen. The
                        // multiply by 100 happens once, here, rather than being
                        // a rule every caller has to remember.
                        NumberBox(value: draft.priceMinMinor.map { Int64(truncating: $0) / 100 },
                                  placeholder: "filters_min") { v in
                            viewModel.editFilters { $0.withPriceMin(minor: v.map { KotlinLong(value: $0 * 100) }) }
                        }
                        NumberBox(value: draft.priceMaxMinor.map { Int64(truncating: $0) / 100 },
                                  placeholder: "filters_max") { v in
                            viewModel.editFilters { $0.withPriceMax(minor: v.map { KotlinLong(value: $0 * 100) }) }
                        }
                    }

                    section("filters_year")
                    HStack(spacing: Space.small) {
                        NumberBox(value: draft.yearMin.map { Int64(truncating: $0) },
                                  placeholder: "filters_from") { v in
                            viewModel.editFilters { $0.withYearMin(year: v.map { KotlinInt(value: Int32($0)) }) }
                        }
                        NumberBox(value: draft.yearMax.map { Int64(truncating: $0) },
                                  placeholder: "filters_to") { v in
                            viewModel.editFilters { $0.withYearMax(year: v.map { KotlinInt(value: Int32($0)) }) }
                        }
                    }

                    section("filters_mileage")
                    NumberBox(value: draft.mileageMax.map { Int64(truncating: $0) },
                              placeholder: "filters_mileage_hint") { v in
                        viewModel.editFilters { $0.withMileageMax(km: v.map { KotlinInt(value: Int32($0)) }) }
                    }

                    section("filters_spec")
                    chipRow(Self.specs, selected: draft.regionalSpecs) { value in
                        viewModel.editFilters { $0.toggleSpec(value: value) }
                    }

                    section("filters_body")
                    chipRow(Self.bodyTypes, selected: draft.bodyTypes) { value in
                        viewModel.editFilters { $0.toggleBodyType(value: value) }
                    }

                    section("filters_seller")
                    chipRow(Self.sellers, selected: Set([draft.sellerKind].compactMap { $0 })) { value in
                        viewModel.editFilters { $0.toggleSellerKind(value: value) }
                    }

                    section("filters_sort")
                    chipRow(Self.sorts(hasLocation: hasLocation),
                            selected: Set([draft.sort ?? SearchSort.newest.wire])) { value in
                        viewModel.editFilters { $0.withSort(value: value) }
                    }

                    Button {
                        viewModel.applyFilters()
                        dismiss()
                    } label: {
                        Text("filters_apply")
                            .font(.system(size: TypeScale.body).weight(.semibold))
                            .foregroundStyle(.white)
                            .frame(maxWidth: .infinity, minHeight: Space.touch)
                            .padding(.vertical, 10)
                            .background(RoundedRectangle(cornerRadius: 10).fill(Palette.accent))
                    }
                    .padding(.top, Space.xl)
                    .padding(.bottom, Space.large)
                }
                .padding(.horizontal, Space.edge)
            }
            .background(Palette.ink)
            .navigationTitle(Text("filters_title"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                // Only when there is something to clear. A permanently visible
                // Clear beside an untouched sheet is a control that does
                // nothing, which is how people learn to ignore controls.
                if !draft.isEmpty {
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("filters_clear") { viewModel.editFilters { $0.cleared() } }
                            .tint(Palette.accent)
                    }
                }
            }
        }
        .presentationDetents([.large])
    }

    // ── options ──────────────────────────────────────────────────────────────

    private struct Opt: Hashable { let value: String; let label: String }

    /// Mirrors the CHECK constraints on `vehicle_listings`. A value outside them
    /// would be dropped by the server's csv() parse and return everything, which
    /// reads as the filter not working.
    private static let specs = [
        Opt(value: "gcc", label: "GCC"), Opt(value: "us", label: "US"),
        Opt(value: "euro", label: "Euro"), Opt(value: "japan", label: "Japan"),
        Opt(value: "other", label: "Other"),
    ]

    private static let bodyTypes = [
        Opt(value: "suv", label: "SUV"), Opt(value: "sedan", label: "Sedan"),
        Opt(value: "hatchback", label: "Hatchback"), Opt(value: "coupe", label: "Coupe"),
        Opt(value: "pickup", label: "Pickup"), Opt(value: "van", label: "Van"),
    ]

    private static let sellers = [
        Opt(value: "private", label: "Private"), Opt(value: "dealer", label: "Dealer"),
    ]

    private static func sorts(hasLocation: Bool) -> [Opt] {
        var out = [
            Opt(value: SearchSort.newest.wire, label: "Newest"),
            Opt(value: SearchSort.priceasc.wire, label: "Price ↑"),
            Opt(value: SearchSort.pricedesc.wire, label: "Price ↓"),
            Opt(value: SearchSort.mileageasc.wire, label: "Lowest km"),
            Opt(value: SearchSort.yeardesc.wire, label: "Newest year"),
        ]
        // Only with a location. The server falls back to newest without one, and
        // a sort that looks selected while producing a different order is worse
        // than one that is absent.
        if hasLocation { out.append(Opt(value: SearchSort.distance.wire, label: "Nearest")) }
        return out
    }

    private func section(_ key: LocalizedStringKey) -> some View {
        Text(key)
            .font(.system(size: TypeScale.micro).weight(.bold))
            .foregroundStyle(Palette.textFaint)
            .textCase(.uppercase)
            .padding(.top, Space.large)
            .padding(.bottom, Space.small)
    }

    private func chipRow(
        _ options: [Opt], selected: Set<String>, onToggle: @escaping (String) -> Void
    ) -> some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Space.small) {
                ForEach(options, id: \.value) { opt in
                    let on = selected.contains(opt.value)
                    Button { onToggle(opt.value) } label: {
                        Text(opt.label)
                            .font(.system(size: TypeScale.secondary)
                                .weight(on ? .semibold : .regular))
                            .foregroundStyle(on ? Palette.accent : Palette.textMuted)
                            .padding(.horizontal, 14).padding(.vertical, 10)
                            .background(Capsule().fill(on ? Palette.accent.opacity(0.12) : .clear))
                            .overlay(Capsule().stroke(on ? Palette.accent : Palette.line, lineWidth: 1))
                    }
                }
            }
        }
    }
}

/// A numeric field that reports nil for empty.
///
/// Nil rather than zero, because they are different questions: "no minimum
/// price" and "a minimum price of nothing" produce the same number and only one
/// of them should reach the wire.
///
/// Its own view rather than a helper function so it can hold the text it is
/// editing. Deriving the string from the bound number on every keystroke fights
/// the keyboard — a leading zero or a half-typed value round-trips through
/// `Int64` and comes back changed under the cursor.
private struct NumberBox: View {
    let value: Int64?
    let placeholder: LocalizedStringKey
    let onValue: (Int64?) -> Void

    @State private var text: String = ""

    var body: some View {
        TextField("", text: $text, prompt:
            Text(placeholder).foregroundStyle(Palette.textGhost))
            .textFieldStyle(.plain)
            .keyboardType(.numberPad)
            .foregroundStyle(Palette.text)
            .font(.system(size: TypeScale.secondary))
            .padding(.horizontal, Space.medium).padding(.vertical, 13)
            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Palette.line, lineWidth: 1))
            .onAppear { text = value.map(String.init) ?? "" }
            .onChange(of: text) { _, raw in
                // Digits only, capped at nine. Nine digits of price is a typo,
                // and without the cap it overflows once multiplied by 100.
                let digits = String(raw.filter(\.isNumber).prefix(9))
                if digits != raw { text = digits }
                onValue(digits.isEmpty ? nil : Int64(digits))
            }
    }
}
