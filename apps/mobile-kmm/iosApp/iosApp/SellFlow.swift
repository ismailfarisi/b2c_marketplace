import SwiftUI
import Shared

/// The sell flow, camera first and form last.
///
/// The ordering is the whole shape of it and it is the shared layer's decision,
/// not this file's: a seller who has already filmed a car has sunk thirty
/// seconds into it and finishes the form; a seller shown a form first closes the
/// app. The vertical is chosen *after* filming for the same reason — it is a
/// question about the video that already exists rather than a gate in front of
/// the camera.
///
/// Read this file for the word "mileage" or "price" and it is not here. Every
/// input is driven by a `SellField` that arrived over the wire, exactly as on
/// Android. The day a mulkiya reader lands and the car form drops from four
/// fields to three, neither screen changes.
struct SellFlow: View {
    let viewModel: SellViewModel
    let onLeave: () -> Void
    let onSeeFeed: () -> Void

    @StateObject private var recorder = Recorder()
    @State private var current: SellUiState?
    private var state: SellUiState { current ?? viewModel.state.value }

    var body: some View {
        ZStack {
            Palette.ink.ignoresSafeArea()

            switch state.step {
            case .record:
                RecordStep(recorder: recorder, tip: state.form?.tip ?? "") { data, ms in
                    viewModel.onClipRecorded(bytes: toKotlinByteArray(data), durationMs: ms)
                } onCancel: {
                    viewModel.reset()
                    onLeave()
                }

            case .vertical:
                VerticalStep(
                    state: state,
                    onPick: viewModel.onVerticalPicked,
                    onBack: { viewModel.onBack() },
                    onRetry: { viewModel.start() }
                )

            case .details:
                DetailsStep(state: state, viewModel: viewModel)

            case .clock:
                ClockStep(state: state, viewModel: viewModel)

            case .live:
                LiveStep(state: state, viewModel: viewModel, onSeeFeed: {
                    // Reset before leaving, so the tab is a camera again next
                    // time. Coming back to a finished live screen makes Sell
                    // look like a receipt rather than a recorder — and holds
                    // the clip's bytes for the life of the process.
                    viewModel.reset()
                    onSeeFeed()
                }, onDone: {
                    viewModel.reset()
                    onLeave()
                })

            default:
                EmptyView()
            }
        }
        .task {
            viewModel.start()
            for await s in viewModel.state { current = s }
        }
        .onDisappear {
            // The camera goes when the tab does. A session left running holds
            // the device and shows in the status bar, and iOS will hand it to
            // any other app that asks — which surfaces as a viewfinder that
            // silently freezes next time.
            recorder.teardown()
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 1 · The viewfinder
// ─────────────────────────────────────────────────────────────────────────────

private struct RecordStep: View {
    @Environment(\.scenePhase) private var scenePhase
    @ObservedObject var recorder: Recorder
    let tip: String
    let onClip: (Data, Int64) -> Void
    let onCancel: () -> Void

    var body: some View {
        ZStack(alignment: .bottom) {
            if recorder.authorised {
                ViewfinderView(session: recorder.session).ignoresSafeArea()
            } else {
                // The permission ask says *why*, and the reason is the product's
                // rather than the platform's: this app lists things people have
                // filmed. A bare system prompt with no context is the version
                // people decline.
                VStack(spacing: Space.medium) {
                    Text("sell_camera_needed")
                        .font(.system(size: 19).weight(.semibold))
                        .foregroundStyle(Palette.text)
                    Text(recorder.blocked ? "sell_camera_blocked" : "sell_camera_why")
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.textMuted)
                        .multilineTextAlignment(.center)
                    // Once the system will no longer prompt, asking again is a
                    // no-op. The button has to point at the one place the
                    // decision can still be changed.
                    if recorder.blocked {
                        Button("sell_camera_settings") {
                            if let url = URL(string: UIApplication.openSettingsURLString) {
                                UIApplication.shared.open(url)
                            }
                        }
                        .tint(Palette.accent)
                        .padding(.top, Space.small)
                    } else {
                        Button("sell_camera_allow") { Task { await recorder.authorise() } }
                            .tint(Palette.accent)
                            .padding(.top, Space.small)
                    }
                }
                .padding(.horizontal, Space.xxl)
            }

            VStack {
                HStack {
                    Button(action: onCancel) {
                        Image(systemName: "xmark")
                            .font(.system(size: 18, weight: .semibold))
                            .foregroundStyle(.white)
                            .frame(width: Space.touch, height: Space.touch)
                    }
                    .accessibilityLabel(Text("action_close"))
                    Spacer()
                }
                Spacer()

                // The vertical's own advice, over the viewfinder, while it is
                // still useful. "Walk the whole unit, slowly" is worth more
                // before filming than in a help article afterwards — and it is
                // served, so a new vertical brings its own without a release.
                if !tip.isEmpty {
                    Text(tip)
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(.white)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, Space.xl)
                        .padding(.vertical, Space.small)
                        .background(Capsule().fill(.black.opacity(0.45)))
                        .padding(.bottom, Space.xl)
                }

                Button {
                    if recorder.recording {
                        recorder.stop()
                    } else {
                        recorder.start(onFinish: onClip)
                    }
                } label: {
                    // The shutter, as a ring around a shape that changes. One
                    // control rather than separate start and stop buttons: the
                    // seller is holding a phone at arm's length pointed at a
                    // car, and hunting for a second target is how a clip ends
                    // up thirty seconds of tarmac.
                    ZStack {
                        Circle().stroke(.white, lineWidth: 4).frame(width: 76, height: 76)
                        RoundedRectangle(cornerRadius: recorder.recording ? 6 : 30)
                            .fill(Palette.accent)
                            .frame(width: recorder.recording ? 32 : 60,
                                   height: recorder.recording ? 32 : 60)
                            .animation(.easeInOut(duration: 0.15), value: recorder.recording)
                    }
                }
                .disabled(!recorder.authorised)
                .padding(.bottom, Space.xxl)
            }

            if let failure = recorder.failure {
                Text(failure)
                    .font(.system(size: 12))
                    .foregroundStyle(Palette.accent)
                    .padding(.bottom, 140)
            }
        }
        .task { if !recorder.authorised { await recorder.authorise() } }
        // The return from Settings. scenePhase rather than onAppear: the view
        // is never dismissed, so onAppear does not fire on the way back.
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await recorder.refreshAuthorisation() } }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 2 · What are you listing?
// ─────────────────────────────────────────────────────────────────────────────

/// The tile grid, served.
///
/// There is no list of verticals in this file. The tiles come from
/// `GET /sell/verticals`, which the subject registry builds by asking each
/// composer's vertical for its own facet — so a new vertical gets a tile the day
/// its composer registers, with no app release.
private struct VerticalStep: View {
    let state: SellUiState
    let onPick: (SellVertical) -> Void
    let onBack: () -> Void
    let onRetry: () -> Void

    var body: some View {
        StepScaffold(title: "sell_what_listing", onBack: onBack, footer: nil) {
            if state.verticals.isEmpty {
                // Three different facts, rendered as the third until now.
                // Someone who has just filmed a listing and is told the
                // product cannot sell it does not tap again — they leave.
                if let failure = state.error {
                    VStack(spacing: Space.small) {
                        Text(failure.words)
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Palette.text.opacity(0.7))
                            .multilineTextAlignment(.center)
                        if failure.canRetry {
                            Button(action: onRetry) {
                                Text("action_retry")
                                    .font(.system(size: 14))
                                    .foregroundStyle(Palette.accent)
                                    .frame(minHeight: Space.touch)
                            }
                        }
                    }
                } else if !state.verticalsLoaded {
                    ProgressView().tint(Palette.textMuted)
                } else {
                    Text("sell_nothing_sellable")
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.textMuted)
                }
            } else {
                // A grid that loaded and a tap that failed. Picking a vertical
                // fetches its form; when that failed the view model set an
                // error and sent the seller back here, where nothing drew it —
                // so the tile simply did not respond, and they tap it again.
                if let failure = state.error {
                    Text(failure.words)
                        .font(.system(size: TypeScale.caption))
                        .foregroundStyle(Palette.bad)
                        .padding(.bottom, Space.medium)
                }
                LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())],
                          spacing: Space.small) {
                    ForEach(state.verticals, id: \.key) { vertical in
                        let active = state.selected?.key == vertical.key
                        Button { onPick(vertical) } label: {
                            VStack(alignment: .leading, spacing: Space.xl) {
                                Text(glyph(vertical.icon)).font(.system(size: 20))
                                Text(localised(vertical.label, vertical.labelAr))
                                    .font(.system(size: 14).weight(active ? .semibold : .regular))
                                    .foregroundStyle(Palette.text)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(14)
                            .background(RoundedRectangle(cornerRadius: 12)
                                .fill(active ? Palette.accent.opacity(0.12) : .clear))
                            .overlay(RoundedRectangle(cornerRadius: 12)
                                .stroke(active ? Palette.accent : Palette.line, lineWidth: 1))
                        }
                    }
                }
            }
        }
    }

    /// The one place a client maps a server value onto something, and it is
    /// safe: the mapping is on the *glyph*, so a new vertical picks a name that
    /// already exists here and needs no release. An unrecognised name falls
    /// back rather than drawing nothing.
    private func glyph(_ icon: String) -> String {
        switch icon {
        case "car": return "🚗"
        case "home": return "🏠"
        case "meal": return "🍽"
        case "moon": return "🌙"
        default: return "🏷"
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 3 · The fields
// ─────────────────────────────────────────────────────────────────────────────

private struct DetailsStep: View {
    let state: SellUiState
    let viewModel: SellViewModel

    var body: some View {
        StepScaffold(
            title: "sell_three_fields",
            subtitle: state.selected.map { localised($0.label, $0.labelAr) },
            onBack: viewModel.onBack,
            footer: AnyView(
                PrimaryButton(title: "sell_next_clock", enabled: !state.busy) {
                    viewModel.onDetailsSubmitted()
                }
            )
        ) {
            ForEach(state.form?.fields ?? [], id: \.key) { field in
                FieldInput(
                    field: field,
                    value: state.values[field.key] ?? "",
                    display: state.lookupLabels[field.key],
                    issue: state.issues.first { $0.key == field.key },
                    // The typeahead rows belong to whichever field is being
                    // looked up, so they are passed down rather than read
                    // from a local — the first version had a stub returning
                    // an empty list, which rendered nothing and looked live.
                    lookupResults: state.lookupFor == field.key ? state.lookupResults : [],
                    viewModel: viewModel
                )
            }

            // The trust slot's identifier, drawn with the same input as the
            // fields above it because it *is* one — it posts back through the
            // same values map. Property's permit number lives here.
            if let reference = state.form?.trust?.reference {
                FieldInput(
                    field: reference,
                    value: state.values[reference.key] ?? "",
                    display: nil,
                    issue: state.issues.first { $0.key == reference.key },
                    // A reference is never a typeahead — the seam refuses a
                    // lookup kind for it, because a catalogue of every permit
                    // number ever issued is not a thing this platform has.
                    lookupResults: [],
                    viewModel: viewModel
                )
            }
        }
    }
}

private struct FieldInput: View {
    let field: SellField
    let value: String
    let display: String?
    let issue: FieldIssue?
    let lookupResults: [SellOption]
    let viewModel: SellViewModel

    @State private var text: String = ""

    var body: some View {
        VStack(alignment: .leading, spacing: Space.tight) {
            HStack {
                Text(localised(field.label, field.labelAr).uppercased())
                    .font(.system(size: TypeScale.micro).weight(.bold))
                    .foregroundStyle(Palette.textFaint)
                if !field.unit.isEmpty {
                    Text(localised(field.unit, field.unitAr))
                        .font(.system(size: TypeScale.micro))
                        .foregroundStyle(Palette.textGhost)
                }
            }

            if field.kind == "select" {
                // A menu rather than a wheel picker. Four options in a sheet is
                // three taps; a menu is two, and the seller can see all of them
                // before committing.
                Menu {
                    ForEach(field.options, id: \.value) { option in
                        Button(localised(option.label, option.labelAr)) {
                            viewModel.onValueChanged(key: field.key, value: option.value)
                        }
                    }
                } label: {
                    boxed(
                        selectedLabel ?? placeholderText,
                        muted: selectedLabel == nil
                    )
                }
            } else {
                TextField("", text: $text, prompt:
                    Text(field.placeholder).foregroundStyle(Palette.textGhost))
                    .textFieldStyle(.plain)
                    .keyboardType(keyboard)
                    .foregroundStyle(Palette.text)
                    .font(.system(size: 16))
                    .padding(14)
                    .overlay(RoundedRectangle(cornerRadius: 8)
                        .stroke(issue == nil ? Palette.line : Palette.bad, lineWidth: 1))
                    .onAppear { text = display ?? value }
                    .onChange(of: text) { _, new in
                        if field.kind == "lookup" {
                            viewModel.onLookupQuery(field: field.key, query: new)
                        } else {
                            viewModel.onValueChanged(key: field.key, value: new)
                        }
                    }
            }

            // The typeahead's rows, when this is the field being looked up. The
            // value posted is an opaque token the vertical minted — the client
            // never learns its format.
            if field.kind == "lookup" && !lookupResults.isEmpty {
                ForEach(lookupResults, id: \.value) { option in
                    Button {
                        viewModel.onLookupPicked(field: field.key, option: option)
                        text = localised(option.label, option.labelAr)
                    } label: {
                        Text(localised(option.label, option.labelAr))
                            .font(.system(size: 14))
                            .foregroundStyle(Palette.text)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.vertical, 10)
                            .padding(.horizontal, 14)
                    }
                    .background(Palette.surface)
                }
            }

            if let issue {
                Text(problemText(issue))
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.bad)
            } else if !field.hint.isEmpty {
                Text(localised(field.hint, field.hintAr))
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.textFaint)
            }
        }
        .padding(.bottom, Space.medium)
    }

    private var selectedLabel: String? {
        field.options.first { $0.value == value }.map { localised($0.label, $0.labelAr) }
    }

    private var placeholderText: String {
        field.placeholder.isEmpty ? localised(field.label, field.labelAr) : field.placeholder
    }

    private var keyboard: UIKeyboardType {
        switch field.kind {
        case "integer", "money": return .numberPad
        default: return .default
        }
    }

    private func boxed(_ label: String, muted: Bool) -> some View {
        Text(label)
            .font(.system(size: 16))
            .foregroundStyle(muted ? Palette.textGhost : Palette.text)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(14)
            .overlay(RoundedRectangle(cornerRadius: 8)
                .stroke(issue == nil ? Palette.line : Palette.bad, lineWidth: 1))
    }

    /// Words for a field problem.
    ///
    /// The view model produces codes, not sentences — the same discipline as
    /// `Failure` — so an Arabic build translates these without a shared file
    /// changing. The bound travels as a number so a translator can put the unit
    /// on the correct side of it.
    private func problemText(_ issue: FieldIssue) -> String {
        switch issue.problem {
        case .required: return String(localized: "sell_field_required")
        case .notanumber: return String(localized: "sell_field_number")
        case .notanoption: return String(localized: "sell_field_option")
        case .belowmin:
            return String(format: String(localized: "sell_field_min"),
                          issue.bound.map { "\($0)" } ?? "", localised(field.unit, field.unitAr))
        case .abovemax:
            return String(format: String(localized: "sell_field_max"),
                          issue.bound.map { "\($0)" } ?? "", localised(field.unit, field.unitAr))
        default: return String(localized: "sell_field_required")
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// 4 · How long should it run?
// ─────────────────────────────────────────────────────────────────────────────

private struct ClockStep: View {
    let state: SellUiState
    let viewModel: SellViewModel

    var body: some View {
        StepScaffold(
            title: "sell_how_long",
            onBack: viewModel.onBack,
            footer: AnyView(
                PrimaryButton(title: "sell_publish", enabled: state.canPublish) {
                    viewModel.onPublish()
                }
            )
        ) {
            ForEach(state.form?.clocks ?? [], id: \.key) { clock in
                let active = state.activeClock?.key == clock.key
                Button { viewModel.onClockPicked(key: clock.key) } label: {
                    VStack(alignment: .leading, spacing: Space.tight) {
                        Text(localised(clock.label, clock.labelAr))
                            .font(.system(size: TypeScale.body).weight(.medium))
                            .foregroundStyle(Palette.text)
                        if !clock.detail.isEmpty {
                            Text(localised(clock.detail, clock.detailAr))
                                .font(.system(size: TypeScale.caption))
                                .foregroundStyle(Palette.textFaint)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(14)
                    .overlay(RoundedRectangle(cornerRadius: 10)
                        .stroke(active ? Palette.accent : Palette.line, lineWidth: 1))
                }
                .disabled(!state.clockIsAChoice)
            }

            // A single clock is a statement, not a choice. A control with one
            // option cannot change anything, and drawing it as a chooser
            // implies the others exist.
            if !state.clockIsAChoice {
                Text("sell_only_term")
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.textFaint)
            }

            Text("sell_emirate_area")
                .font(.system(size: TypeScale.micro).weight(.bold))
                .foregroundStyle(Palette.textFaint)
                .padding(.top, Space.large)

            ForEach(PLACES, id: \.label) { place in
                let active = state.place == place.label
                Button {
                    viewModel.onPlacePicked(region: "Dubai", place: place.label,
                                            lat: place.lat, lng: place.lng)
                } label: {
                    Text(place.displayName)
                        .font(.system(size: TypeScale.body))
                        .foregroundStyle(active ? Palette.accent : Palette.text)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.vertical, 12)
                }
            }

            if state.place.isEmpty {
                Text("sell_place_required")
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.textFaint)
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// shared chrome
// ─────────────────────────────────────────────────────────────────────────────

private struct StepScaffold<Content: View>: View {
    let title: LocalizedStringKey
    var subtitle: String?
    let onBack: () -> Void
    let footer: AnyView?
    @ViewBuilder let content: () -> Content

    init(
        title: LocalizedStringKey,
        subtitle: String? = nil,
        onBack: @escaping () -> Void,
        footer: AnyView?,
        @ViewBuilder content: @escaping () -> Content
    ) {
        self.title = title
        self.subtitle = subtitle
        self.onBack = onBack
        self.footer = footer
        self.content = content
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Header(title: "", onBack: onBack)

            Text(title)
                .font(.system(size: 21).weight(.semibold))
                .foregroundStyle(Palette.text)
                .padding(.horizontal, Space.edge)
            if let subtitle {
                Text(subtitle)
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Palette.textMuted)
                    .padding(.horizontal, Space.edge)
                    .padding(.top, Space.tight)
            }

            ScrollView {
                VStack(alignment: .leading, spacing: 0) { content() }
                    .padding(.horizontal, Space.edge)
                    .padding(.top, Space.xl)
            }

            if let footer {
                footer.padding(.horizontal, Space.edge).padding(.bottom, Space.medium)
            }
        }
    }
}

private struct PrimaryButton: View {
    let title: LocalizedStringKey
    let enabled: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(.system(size: TypeScale.body).weight(.semibold))
                .foregroundStyle(.white)
                .frame(maxWidth: .infinity, minHeight: Space.touch)
                .padding(.vertical, 10)
                .background(RoundedRectangle(cornerRadius: 10)
                    .fill(enabled ? Palette.accent : Palette.accent.opacity(0.3)))
        }
        .disabled(!enabled)
    }
}

/// Bridges `Data` to the `KotlinByteArray` the shared layer takes.
///
/// # Why this is one memcpy and not a loop
///
/// The obvious version iterates `data.enumerated()` calling `array.set` per
/// byte. For a sixty-second clip that is twenty million calls *across the
/// Objective-C bridge*, each one a dynamic dispatch — seconds of main-thread
/// work, on the tap that is supposed to feel like finishing.
///
/// `usePinned` hands back a stable pointer to Kotlin/Native's backing store for
/// the duration of the block, so the whole thing is a single copy. The copy
/// itself is unavoidable across the boundary, and is the reason
/// `onClipRecorded` takes bytes rather than a path — see the note on Recorder.
private func toKotlinByteArray(_ data: Data) -> KotlinByteArray {
    let array = KotlinByteArray(size: Int32(data.count))
    data.withUnsafeBytes { (raw: UnsafeRawBufferPointer) in
        guard let source = raw.baseAddress else { return }
        BytesKt.copyInto(target: array, source: source, count: Int32(data.count))
    }
    return array
}
