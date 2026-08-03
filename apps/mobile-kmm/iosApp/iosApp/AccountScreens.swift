import SwiftUI
import Shared

// ─────────────────────────────────────────────────────────────────────────────
// Search
// ─────────────────────────────────────────────────────────────────────────────

/// Search runs against the classifieds index, not the feed.
///
/// Two surfaces, deliberately. The feed is discovery — geo-scoped, ranked by
/// watch behaviour, no text box. Search is intent: someone who wants a Patrol
/// under 200k types that, and the structured facts to answer it live in
/// `vehicle_listings`.
///
/// Results are cards rather than reels because a person comparing seven cars is
/// not going to watch seven videos — but tapping one opens the reel, because
/// they will happily watch *the* one.
struct SearchScreen: View {
    let viewModel: MeViewModel
    let hasLocation: Bool
    let onOpenReel: (String) -> Void
    let onInquire: (SearchResult) -> Void

    /// Nil until the flow emits, then whatever it last said.
    ///
    /// Optional rather than a fabricated empty value, for two reasons. A
    /// hand-built `SearchUiState(...)` is a literal that has to be updated every
    /// time the shared type gains a field, and Kotlin default arguments do not
    /// cross to Swift above a small parameter count — so the no-arg form does
    /// not exist and the long form silently rots.
    ///
    /// It is also more honest: a screen rendering an invented empty state before
    /// the first emission shows "nothing matched" for a frame to someone whose
    /// search is still running. `StateFlow.value` is available synchronously, so
    /// there is never a frame without a real answer.
    @State private var current: SearchUiState?
    private var state: SearchUiState { current ?? viewModel.search.value }
    @State private var query: String = ""

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: Space.small) {
                TextField("", text: $query, prompt:
                    Text("search_hint").foregroundStyle(Palette.textGhost))
                    .textFieldStyle(.plain)
                    .foregroundStyle(Palette.text)
                    .font(.system(size: TypeScale.body))
                    .submitLabel(.search)
                    .onSubmit { viewModel.submitSearch() }
                    .onChange(of: query) { _, new in viewModel.onQueryChanged(query: new) }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 13)
                    .overlay(RoundedRectangle(cornerRadius: 10).stroke(Palette.line, lineWidth: 1))

                // The count is on the button, always. A filter still applied
                // from three searches ago is the commonest way a search screen
                // lies: someone sets a price cap, searches something else, gets
                // nothing, and concludes the thing does not exist.
                let active = Int(state.filters.activeCount)
                Button { viewModel.openFilters() } label: {
                    Text(active > 0
                         ? String(format: String(localized: "search_filters_n"), active)
                         : String(localized: "search_filters"))
                        .font(.system(size: TypeScale.secondary)
                            .weight(active > 0 ? .semibold : .regular))
                        .foregroundStyle(active > 0 ? Palette.accent : Palette.textMuted)
                        .padding(.horizontal, Space.medium)
                        .frame(minHeight: Space.touch)
                        .overlay(
                            RoundedRectangle(cornerRadius: 10)
                                .stroke(active > 0 ? Palette.accent : Palette.line, lineWidth: 1)
                        )
                }
            }
            .padding(.horizontal, Space.large)
            .padding(.vertical, Space.medium)

            content
        }
        .background(Palette.ink)
        .task {
            for await s in viewModel.search {
                current = s
                // The box is not driven from state on every tick — that would
                // fight the keyboard mid-word. It is only corrected when the
                // two have genuinely diverged, which happens when the view
                // model clears a query itself.
                if s.query != query && !s.query.isEmpty { query = s.query }
            }
        }
        .sheet(isPresented: Binding(
            get: { state.draftFilters != nil },
            set: { if !$0 { viewModel.cancelFilters() } }
        )) {
            if let draft = state.draftFilters {
                SearchFilterSheet(viewModel: viewModel, draft: draft, hasLocation: hasLocation)
            }
        }
    }

    @ViewBuilder
    private var content: some View {
        // Four states, not one. "Type something", "that failed", "nothing
        // matched" and a list are four different facts, and a single "No
        // results" reports a working search as broken to the person who has not
        // searched yet.
        if state.loading && state.items.isEmpty {
            Spacer(); LoadingBlock(); Spacer()
        } else if !state.searched && state.error == nil {
            Spacer()
            EmptyBlock(message: String(localized: "search_prompt"),
                       hint: String(localized: "search_prompt_hint"))
            Spacer()
        } else if let failure = state.error, state.items.isEmpty {
            Spacer()
            FailureBlock(failure: failure) { viewModel.submitSearch() }
            Spacer()
        } else if state.items.isEmpty {
            Spacer()
            EmptyBlock(message: String(format: String(localized: "search_none"), state.query),
                       hint: String(localized: "search_none_hint"))
            Spacer()
        } else {
            List {
                if let failure = state.error {
                    FailureStrip(failure: failure) { viewModel.submitSearch() }
                        .listRowInsets(EdgeInsets())
                        .listRowBackground(Palette.ink)
                }
                Text(String(format: String(localized: "search_count"), state.total))
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.textFaint)
                    .listRowBackground(Palette.ink)

                ForEach(state.items, id: \.id) { result in
                    SearchRow(result: result)
                        .listRowBackground(Palette.ink)
                        .contentShape(Rectangle())
                        .onTapGesture {
                            // The video first when there is one. Going straight
                            // to a conversation for everything skipped the reel
                            // entirely, making the highest-intent surface the
                            // one place you could not watch anything.
                            if let reel = result.reelId {
                                onOpenReel(reel)
                            } else {
                                onInquire(result)
                            }
                        }
                        .onAppear {
                            // Paged a few rows early rather than at the bottom.
                            if result.id == state.items.suffix(4).first?.id {
                                viewModel.loadMore()
                            }
                        }
                }

                if state.loadingMore {
                    LoadingBlock().listRowBackground(Palette.ink)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
        }
    }
}

private struct SearchRow: View {
    let result: SearchResult

    var body: some View {
        HStack(alignment: .top, spacing: Space.medium) {
            Poster(url: result.posterUrl.isEmpty ? nil : result.posterUrl,
                   lqip: result.lqip.isEmpty ? nil : result.lqip)
                .frame(width: 54, height: 72)

            VStack(alignment: .leading, spacing: Space.hair) {
                HStack {
                    Text(result.title.isEmpty ? result.referenceNo : result.title)
                        .font(.system(size: TypeScale.body).weight(.medium))
                        .foregroundStyle(Palette.text)
                    Spacer()
                    if result.promoted {
                        // Labelled, always. A promoted result that looks organic
                        // is what makes buyers stop trusting the order — at
                        // which point the placement the seller paid for is worth
                        // nothing.
                        Text("search_promoted")
                            .font(.system(size: TypeScale.micro).weight(.bold))
                            .foregroundStyle(Palette.accent)
                            .padding(.horizontal, 5).padding(.vertical, 2)
                            .overlay(RoundedRectangle(cornerRadius: 4)
                                .stroke(Palette.accent.opacity(0.6), lineWidth: 1))
                    }
                }
                Text(Self.attributes(result))
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Palette.textMuted)
                Text("\(result.currency) \(grouped(result.priceMinor / 100))")
                    .font(.system(size: TypeScale.body).weight(.semibold))
                    .foregroundStyle(Palette.accent)
            }
        }
        .padding(.vertical, Space.tight)
    }

    private static func attributes(_ r: SearchResult) -> String {
        var parts: [String] = []
        if r.mileageKm > 0 { parts.append("\(grouped(Int64(r.mileageKm))) km") }
        if !r.regionalSpec.isEmpty { parts.append(r.regionalSpec.uppercased()) }
        let place = r.city.isEmpty ? r.emirate : r.city
        if !place.isEmpty { parts.append(place) }
        if let metres = r.distanceM?.int64Value { parts.append(distance(metres)) }
        return parts.joined(separator: " · ")
    }

    /// Rounded to something a person would say.
    ///
    /// Integer division alone renders everything under a kilometre as "0 km
    /// away", which on the column people choose by looks like missing data —
    /// the same bug the Android row had.
    private static func distance(_ metres: Int64) -> String {
        switch metres {
        case ..<1000: return "\(metres) m away"
        case ..<10_000: return "\(metres / 1000).\((metres % 1000) / 100) km away"
        default: return "\(metres / 1000) km away"
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Saved
// ─────────────────────────────────────────────────────────────────────────────

/// What the viewer kept.
///
/// The words on each row are the vertical's, resolved through the same
/// SummarizePage the feed uses — so a saved car and a saved flash offer come
/// through one view that knows what neither of them is.
struct SavedScreen: View {
    let viewModel: MeViewModel
    let onOpenReel: (String) -> Void

    // See SearchScreen: the current value rather than an invented one.
    @State private var current: SavedUiState?
    private var state: SavedUiState { current ?? viewModel.saved.value }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ScreenTitle("nav_saved")

            if state.loading && state.items.isEmpty {
                Spacer(); LoadingBlock(); Spacer()
            } else if let failure = state.error, state.items.isEmpty {
                Spacer()
                FailureBlock(failure: failure) { viewModel.loadSaved(force: true) }
                Spacer()
            } else if state.items.isEmpty {
                Spacer()
                EmptyBlock(message: String(localized: "saved_empty"),
                           hint: String(localized: "saved_empty_hint"))
                Spacer()
            } else {
                List(state.items, id: \.reelId) { item in
                    SavedRow(item: item)
                        .listRowBackground(Palette.ink)
                        .contentShape(Rectangle())
                        .onTapGesture {
                            // The reel they kept, not the feed in general. A
                            // bookmark that opens something else is the least
                            // useful possible answer to a bookmark.
                            guard !item.unavailable else { return }
                            onOpenReel(item.reelId)
                        }
                }
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
            }
        }
        .background(Palette.ink)
        .task {
            viewModel.loadSaved(force: false)
            for await s in viewModel.saved { current = s }
        }
    }
}

private struct SavedRow: View {
    let item: SavedItem

    var body: some View {
        let fade = item.unavailable ? 0.4 : 1.0
        let summary = item.summary

        HStack(alignment: .top, spacing: Space.medium) {
            Poster(url: item.posterUrl.isEmpty ? nil : item.posterUrl,
                   lqip: item.lqip.isEmpty ? nil : item.lqip)
                .frame(width: 54, height: 72)

            VStack(alignment: .leading, spacing: Space.hair) {
                if let badge = summary.map({ localised($0.badge, $0.badgeAr) }), !badge.isEmpty {
                    Text(badge)
                        .font(.system(size: TypeScale.micro).weight(.bold))
                        .foregroundStyle(Palette.text.opacity(0.45 * fade))
                }
                // No summary means the vertical has no provider, or its provider
                // failed. Either way the reel is still a real thing the viewer
                // kept, so the row renders rather than vanishing.
                Text(summary.map { localised($0.title, $0.titleAr) }.flatMap { $0.isEmpty ? nil : $0 }
                     ?? String(localized: "saved_untitled"))
                    .font(.system(size: TypeScale.body).weight(.medium))
                    .foregroundStyle(Palette.text.opacity(fade))
                if let line = summary.map({ localised($0.line, $0.lineAr) }), !line.isEmpty {
                    Text(line)
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.text.opacity(0.5 * fade))
                }
                if let summary, let price = summary.priceMinor?.int64Value,
                   let currency = summary.currency, !currency.isEmpty {
                    Text("\(currency) \(grouped(price / 100))"
                         + localised(summary.priceSuffix, summary.priceSuffixAr))
                        .font(.system(size: TypeScale.secondary).weight(.semibold))
                        .foregroundStyle(Palette.accent.opacity(fade))
                }
                if item.unavailable {
                    Text("saved_gone")
                        .font(.system(size: TypeScale.caption))
                        .foregroundStyle(Palette.textFaint)
                }
            }
        }
        .padding(.vertical, Space.tight)
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Me
// ─────────────────────────────────────────────────────────────────────────────

/// The seller's own listings, in every state including the ugly ones.
///
/// A rejected listing and a reel stuck in review are the two things a seller
/// actually comes here to find out about, and both are invisible everywhere
/// else. Showing only live listings would leave the most anxious person on the
/// platform with a blank screen and no explanation.
struct MeScreen: View {
    let viewModel: MeViewModel
    let onOpenInbox: () -> Void
    let onSignOut: () -> Void

    @State private var current: MyListingsUiState?
    private var state: MyListingsUiState { current ?? viewModel.listings.value }
    @State private var unread: Int = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ScreenTitle("nav_me")

            List {
                Button(action: onOpenInbox) {
                    HStack {
                        Text("me_messages")
                            .font(.system(size: TypeScale.body))
                            .foregroundStyle(Palette.text)
                        if unread > 0 {
                            Text(unread > 99 ? "99+" : "\(unread)")
                                .font(.system(size: TypeScale.micro).weight(.bold))
                                .foregroundStyle(.white)
                                .padding(.horizontal, 6).padding(.vertical, 2)
                                .background(Capsule().fill(Palette.accent))
                        }
                        Spacer()
                        Image(systemName: "chevron.forward")
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Palette.textFaint)
                    }
                    .frame(minHeight: Space.touch)
                }
                .listRowBackground(Palette.ink)

                Section {
                    if state.loading && state.items.isEmpty {
                        LoadingBlock().listRowBackground(Palette.ink)
                    } else if let failure = state.error, state.items.isEmpty {
                        FailureBlock(failure: failure) { viewModel.loadMyListings(force: true) }
                            .listRowBackground(Palette.ink)
                    } else if state.items.isEmpty {
                        EmptyBlock(message: String(localized: "me_no_listings"),
                                   hint: String(localized: "me_no_listings_hint"))
                            .listRowBackground(Palette.ink)
                    } else {
                        ForEach(state.items, id: \.id) { listing in
                            ListingRow(listing: listing).listRowBackground(Palette.ink)
                        }
                    }
                } header: {
                    Text("me_your_listings")
                        .font(.system(size: TypeScale.caption).weight(.medium))
                        .foregroundStyle(Palette.text.opacity(0.45))
                }

                Button(action: onSignOut) {
                    Text("me_sign_out")
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.text.opacity(0.7))
                        .frame(maxWidth: .infinity, minHeight: Space.touch)
                        .overlay(RoundedRectangle(cornerRadius: 10).stroke(Palette.line, lineWidth: 1))
                }
                .listRowBackground(Palette.ink)
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
        }
        .background(Palette.ink)
        .task {
            viewModel.loadMyListings(force: false)
            // The unread count is the only reason to open the inbox, so it has
            // to be known before the viewer decides whether to.
            viewModel.loadInbox()
            for await s in viewModel.listings { current = s }
        }
        .task {
            for await inbox in viewModel.inbox {
                unread = inbox.items.reduce(0) { $0 + Int($1.unread) }
            }
        }
    }
}

private struct ListingRow: View {
    let listing: MyListing

    var body: some View {
        HStack(alignment: .top, spacing: Space.medium) {
            Poster(url: listing.posterUrl.isEmpty ? nil : listing.posterUrl,
                   lqip: listing.lqip.isEmpty ? nil : listing.lqip)
                .frame(width: 44, height: 58)

            VStack(alignment: .leading, spacing: Space.hair) {
                HStack {
                    Text(listing.title.isEmpty ? listing.referenceNo : listing.title)
                        .font(.system(size: TypeScale.body).weight(.medium))
                        .foregroundStyle(Palette.text)
                    Spacer()
                    StatusPill(status: listing.status)
                }
                Text(Self.attributes(listing))
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Palette.textMuted)
                // The reel's state, when it is the thing holding the listing up.
                // A seller whose car is "pending review" while the video is
                // still transcoding is being told the wrong reason otherwise.
                if let note = Self.reelNote(listing) {
                    Text(note)
                        .font(.system(size: TypeScale.caption))
                        .foregroundStyle(Palette.textFaint)
                }
            }
        }
        .padding(.vertical, Space.tight)
    }

    private static func attributes(_ l: MyListing) -> String {
        var parts: [String] = []
        if l.mileageKm > 0 { parts.append("\(grouped(Int64(l.mileageKm))) km") }
        if !l.city.isEmpty { parts.append(l.city) }
        parts.append("\(l.currency) \(grouped(l.priceMinor / 100))")
        return parts.joined(separator: " · ")
    }

    private static func reelNote(_ l: MyListing) -> String? {
        guard l.reelId != nil else { return nil }
        switch l.reelState {
        case "published", "": return nil
        case "review_pending": return String(localized: "reel_note_review")
        case "rejected": return String(localized: "reel_note_rejected")
        case "expired": return String(localized: "reel_note_expired")
        case "failed": return String(localized: "reel_note_failed")
        default: return String(localized: "reel_note_processing")
        }
    }
}

private struct StatusPill: View {
    let status: String

    var body: some View {
        let (label, colour) = words
        Text(label.uppercased())
            .font(.system(size: TypeScale.micro).weight(.bold))
            .foregroundStyle(colour)
            .padding(.horizontal, 6).padding(.vertical, 2)
            .overlay(RoundedRectangle(cornerRadius: 4).stroke(colour.opacity(0.5), lineWidth: 1))
    }

    private var words: (String, Color) {
        switch status {
        case "active": return (String(localized: "status_active"), Palette.good)
        case "pending_review": return (String(localized: "status_review"), Palette.accent)
        case "draft": return (String(localized: "status_draft"), Palette.textFaint)
        case "rejected": return (String(localized: "status_rejected"), Palette.bad)
        case "sold": return (String(localized: "status_sold"), Palette.textFaint)
        case "paused": return (String(localized: "status_paused"), Palette.textFaint)
        case "expired": return (String(localized: "status_expired"), Palette.textFaint)
        // A status this build predates still renders, as itself. Falling
        // through to nothing would make a new state invisible on the one screen
        // that exists to explain states.
        default: return (status, Palette.textFaint)
        }
    }
}

struct ScreenTitle: View {
    let key: LocalizedStringKey
    init(_ key: LocalizedStringKey) { self.key = key }

    var body: some View {
        Text(key)
            .font(.system(size: TypeScale.title).weight(.semibold))
            .foregroundStyle(Palette.text)
            .padding(.leading, Space.edge)
            .padding(.top, Space.medium)
            .padding(.bottom, 10)
    }
}
