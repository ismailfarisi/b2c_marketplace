import SwiftUI
import Shared

/// The inbox.
///
/// The other half of the CTA. "Message seller" is on every reel in the feed and
/// this is where it lands.
struct InboxScreen: View {
    let viewModel: MeViewModel
    let onOpen: (Conversation) -> Void
    let onBack: () -> Void

    @State private var current: InboxUiState?
    private var state: InboxUiState { current ?? viewModel.inbox.value }

    var body: some View {
        VStack(spacing: 0) {
            Header(title: String(localized: "me_messages"), onBack: onBack)

            if state.loading && state.items.isEmpty {
                Spacer(); LoadingBlock(); Spacer()
            } else if let failure = state.error, state.items.isEmpty {
                Spacer(); FailureBlock(failure: failure) { viewModel.loadInbox() }; Spacer()
            } else if state.items.isEmpty {
                Spacer()
                EmptyBlock(message: String(localized: "inbox_empty"),
                           hint: String(localized: "inbox_empty_hint"))
                Spacer()
            } else {
                List(state.items, id: \.id) { c in
                    let title = c.listingTitle.isEmpty
                        ? String(localized: "inbox_untitled") : c.listingTitle
                    HStack(spacing: Space.medium) {
                        // The conversation's initial rather than a photo: there
                        // is no seller-picture endpoint, and a grey silhouette
                        // repeated down the list would be decoration pretending
                        // to be information.
                        Text(title.prefix(1).uppercased())
                            .font(.system(size: TypeScale.heading).weight(.semibold))
                            .foregroundStyle(Palette.textMuted)
                            .frame(width: 44, height: 44)
                            .background(Circle().fill(Palette.surface))

                        VStack(alignment: .leading, spacing: Space.hair) {
                            Text(title)
                                .font(.system(size: TypeScale.body)
                                    .weight(c.unread > 0 ? .semibold : .regular))
                                .foregroundStyle(Palette.text)
                            // A closed or blocked thread is stated. A buyer
                            // typing into a thread the seller closed and getting
                            // silence is worse than being told.
                            if c.state != "open" && !c.state.isEmpty {
                                Text(c.state)
                                    .font(.system(size: TypeScale.caption))
                                    .foregroundStyle(Palette.textFaint)
                            }
                        }
                        Spacer()
                        if c.unread > 0 {
                            Text(c.unread > 9 ? "9+" : "\(c.unread)")
                                .font(.system(size: 10).weight(.bold))
                                .foregroundStyle(.white)
                                .frame(width: 20, height: 20)
                                .background(Circle().fill(Palette.accent))
                        }
                    }
                    .frame(minHeight: Space.touch)
                    .listRowBackground(Palette.ink)
                    .contentShape(Rectangle())
                    .onTapGesture { onOpen(c) }
                }
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
            }
        }
        .background(Palette.ink)
        .task {
            viewModel.loadInbox()
            for await s in viewModel.inbox { current = s }
        }
    }
}

/// One conversation.
///
/// Three things here are not cosmetic.
///
/// **Bubbles have sides.** `mine` is computed by the server, because the client
/// has no idea which uuid is its own. Without it every bubble renders down one
/// side, which is a transcript with the speaker labels torn off.
///
/// **There is no optimistic bubble.** The message that appears is the one the
/// server returned. A message can come back `held` — under review and *not*
/// delivered — or `redacted`, with a phone number stripped. A sender who thinks
/// a held message arrived concludes they are being ignored and goes looking for
/// a way off the platform, which is exactly what the redaction prevents.
///
/// **The draft survives a failure.** Losing what someone typed because the
/// network dropped is the most annoying failure a messaging screen has.
struct ThreadScreen: View {
    let viewModel: MeViewModel
    let onBack: () -> Void

    @State private var current: ThreadUiState?
    private var state: ThreadUiState { current ?? viewModel.thread.value }
    @State private var draft: String = ""

    var body: some View {
        VStack(spacing: 0) {
            Header(title: state.title.isEmpty ? String(localized: "me_messages") : state.title,
                   onBack: onBack)

            if state.loading {
                Spacer(); LoadingBlock(); Spacer()
            } else if let failure = state.error, state.messages.isEmpty {
                // A thread that failed to load is not an empty thread. Showing
                // "Say hello" over a request that 500'd invites the viewer to
                // type their question into a conversation that already has it.
                Spacer(); FailureBlock(failure: failure); Spacer()
            } else if state.messages.isEmpty {
                Spacer()
                EmptyBlock(message: String(localized: "thread_empty"),
                           hint: String(localized: "thread_empty_hint"))
                Spacer()
            } else {
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(spacing: Space.tight) {
                            ForEach(state.messages, id: \.id) { Bubble(message: $0).id($0.id) }
                        }
                        .padding(.vertical, Space.small)
                    }
                    .onChange(of: state.messages.count) { _, _ in
                        if let last = state.messages.last {
                            withAnimation { proxy.scrollTo(last.id, anchor: .bottom) }
                        }
                    }
                }
            }

            if let failure = state.error, !state.messages.isEmpty {
                FailureStrip(failure: failure) { viewModel.sendDraft() }
            }

            HStack(alignment: .bottom, spacing: Space.small) {
                TextField("", text: $draft, prompt:
                    Text("thread_hint").foregroundStyle(Palette.textGhost), axis: .vertical)
                    .textFieldStyle(.plain)
                    .lineLimit(1...5)
                    .foregroundStyle(Palette.text)
                    .font(.system(size: 14))
                    .onChange(of: draft) { _, new in viewModel.onDraftChanged(text: new) }
                    .padding(.horizontal, 14).padding(.vertical, 11)
                    // Filled rather than outlined. A hairline box around a
                    // growing multi-line field draws a shape that changes size
                    // as you type; a fill just gets taller.
                    .background(Capsule().fill(Palette.surface))

                let canSend = !draft.trimmingCharacters(in: .whitespaces).isEmpty && !state.sending
                Button { viewModel.sendDraft() } label: {
                    // A glyph, not the word "Send". The composer is the one row
                    // that has to survive a keyboard, an eight-line draft and a
                    // language that reads the other way.
                    Image(systemName: "paperplane.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(canSend ? .white : Palette.textFaint)
                        .frame(width: Space.touch, height: Space.touch)
                        .background(Circle().fill(canSend ? Palette.accent : Palette.surface))
                }
                .disabled(!canSend)
                .accessibilityLabel(Text("thread_send"))
            }
            .padding(.horizontal, Space.medium)
            .padding(.vertical, 10)
        }
        .background(Palette.ink)
        .task {
            for await s in viewModel.thread {
                current = s
                // Only corrected when the view model cleared it — a successful
                // send. Driving the field from state on every tick would fight
                // the keyboard mid-word.
                if s.draft.isEmpty && !draft.isEmpty && !s.sending { draft = "" }
            }
        }
    }
}

private struct Bubble: View {
    let message: Message

    var body: some View {
        VStack(alignment: message.mine ? .trailing : .leading, spacing: 3) {
            Text(message.body)
                .font(.system(size: 14))
                .foregroundStyle(Palette.text.opacity(message.state == "held" ? 0.5 : 0.92))
                .padding(.horizontal, 13).padding(.vertical, 9)
                .frame(maxWidth: 300, alignment: .leading)
                .background(
                    // The viewer's own messages carry the accent, muted. A
                    // saturated fill is the convention, but this accent is also
                    // the price colour and the CTA colour — at full strength a
                    // chat would be the loudest screen in an app about video.
                    UnevenRoundedRectangle(
                        topLeadingRadius: 20,
                        // The corner nearest the speaker is squared off: the
                        // cheapest way to say which side a message is on, and it
                        // survives bright sunlight where a subtle fill does not.
                        bottomLeadingRadius: message.mine ? 20 : Space.tight,
                        bottomTrailingRadius: message.mine ? Space.tight : 20,
                        topTrailingRadius: 20
                    )
                    .fill(message.mine ? Palette.accent.opacity(0.22) : Palette.surface)
                )

            // The state, in the sender's own words, under the bubble.
            if let note = Self.stateNote(message) {
                Text(note)
                    .font(.system(size: 10))
                    .foregroundStyle(Palette.accent.opacity(0.85))
            }
        }
        .frame(maxWidth: .infinity, alignment: message.mine ? .trailing : .leading)
        .padding(.horizontal, 14)
    }

    private static func stateNote(_ m: Message) -> String? {
        switch m.state {
        case "held": return String(localized: "msg_held")
        case "redacted": return String(localized: "msg_redacted")
        case "blocked": return String(localized: "msg_blocked")
        default: return nil
        }
    }
}

struct Header: View {
    let title: String
    let onBack: () -> Void

    var body: some View {
        HStack(spacing: Space.medium) {
            Button(action: onBack) {
                // A system chevron rather than a "‹" character, which does not
                // mirror in a right-to-left layout — SwiftUI flips the symbol
                // and cannot flip a glyph in a string.
                Image(systemName: "chevron.backward")
                    .font(.system(size: 20))
                    .foregroundStyle(Palette.text)
                    .frame(minWidth: Space.touch, minHeight: Space.touch)
            }
            .accessibilityLabel(Text("action_back"))

            Text(title)
                .font(.system(size: TypeScale.heading).weight(.semibold))
                .foregroundStyle(Palette.text)
            Spacer()
        }
        .padding(.horizontal, 14)
        .padding(.vertical, Space.medium)
    }
}
