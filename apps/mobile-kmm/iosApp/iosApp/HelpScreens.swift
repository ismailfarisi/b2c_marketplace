import SwiftUI
import Shared

/// Help (new request + the customer's requests), one request's thread, and the
/// invoice list — native, on the app's own session, so a signed-in customer
/// never has to sign in again in a browser to ask for help.

private let dubai = TimeZone(identifier: "Asia/Dubai") ?? .current

/// Activity time in Dubai, so a 01:00 reply isn't dated the day before.
private func whenText(_ iso: String, time: Bool) -> String {
    let parser = ISO8601DateFormatter()
    parser.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    let date = parser.date(from: iso) ?? ISO8601DateFormatter().date(from: iso)
    guard let date else { return String(iso.prefix(10)) }
    let f = DateFormatter()
    f.locale = Locale(identifier: isArabicLocale ? "ar" : "en")
    f.timeZone = dubai
    f.dateStyle = .medium
    f.timeStyle = time ? .short : .none
    return f.string(from: date)
}

private func statusLabel(_ status: String) -> String {
    switch status {
    case "open": return String(localized: "help_status_open")
    case "answered": return String(localized: "help_status_answered")
    case "closed": return String(localized: "help_status_closed")
    default: return status
    }
}

private func amount(_ minor: Int64, _ currency: String) -> String {
    let f = NumberFormatter()
    f.numberStyle = .decimal
    f.locale = Locale(identifier: "en_US_POSIX")
    f.minimumFractionDigits = 2
    f.maximumFractionDigits = 2
    var s = f.string(from: NSNumber(value: Double(minor) / 100)) ?? "\(minor)"
    if s.hasSuffix(".00") { s.removeLast(3) }
    return currency + " " + s
}

private func rejectedFields(_ failure: Failure?) -> [String: String]? {
    guard let failure, case .rejected(let r) = onEnum(of: failure) else { return nil }
    return r.fields
}

/// A send failure in words, plus that the text is still there.
private struct SendFailure: View {
    let failure: Failure
    var body: some View {
        let kept = rejectedFields(failure) == nil ? " " + String(localized: "help_kept") : ""
        Text(failure.words + kept)
            .font(.system(size: TypeScale.secondary))
            .foregroundStyle(Palette.bad)
    }
}

private struct HelpField: View {
    let hint: LocalizedStringKey
    @Binding var text: String
    let lines: Int
    let error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: Space.tight) {
            TextField("", text: $text, prompt: Text(hint).foregroundStyle(Palette.textGhost), axis: .vertical)
                .textFieldStyle(.plain)
                .lineLimit(lines...(lines + 3))
                .foregroundStyle(Palette.text)
                .font(.system(size: 14))
                .padding(.horizontal, 14).padding(.vertical, 11)
                .overlay(RoundedRectangle(cornerRadius: 12)
                    .stroke(error == nil ? Palette.line : Palette.bad, lineWidth: 1))
            if let error {
                Text(error).font(.system(size: TypeScale.secondary)).foregroundStyle(Palette.bad)
            }
        }
    }
}

private struct SendButton: View {
    let sending: Bool
    let action: () -> Void
    var body: some View {
        Button(action: action) {
            Text(sending ? "help_sending" : "help_send")
                .font(.system(size: 14).weight(.medium))
                .foregroundStyle(.white)
                .padding(.horizontal, Space.large).padding(.vertical, 11)
                .frame(minHeight: Space.touch)
                .background(Capsule().fill(sending ? Palette.accent.opacity(0.3) : Palette.accent))
        }
        .disabled(sending)
    }
}

struct HelpScreen: View {
    let viewModel: SupportViewModel
    let onBack: () -> Void
    let onOpenTicket: (String) -> Void

    @State private var current: HelpUiState?
    private var s: HelpUiState { current ?? viewModel.help.value }
    @State private var subject = ""
    @State private var message = ""

    var body: some View {
        VStack(spacing: 0) {
            Header(title: String(localized: "me_help"), onBack: onBack)
            List {
                Text("help_lede")
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Palette.textMuted)
                    .listRowBackground(Palette.ink)
                if s.loading && !s.loaded {
                    LoadingBlock().listRowBackground(Palette.ink)
                } else if let failure = s.error, case .unauthorized = onEnum(of: failure) {
                    FailureBlock(failure: failure).listRowBackground(Palette.ink)
                } else {
                    form.listRowBackground(Palette.ink)
                    Section {
                        if let failure = s.error, s.tickets.isEmpty {
                            FailureBlock(failure: failure) { viewModel.loadTickets(force: true) }
                                .listRowBackground(Palette.ink)
                        } else if s.loaded && s.tickets.isEmpty {
                            EmptyBlock(message: String(localized: "help_empty")).listRowBackground(Palette.ink)
                        }
                        ForEach(s.tickets, id: \.id) { t in
                            Button { onOpenTicket(t.id) } label: {
                                HStack {
                                    VStack(alignment: .leading, spacing: 2) {
                                        Text(t.subject)
                                            .font(.system(size: TypeScale.body))
                                            .foregroundStyle(Palette.text)
                                        Text(whenText(t.updatedAt, time: false))
                                            .font(.system(size: TypeScale.caption))
                                            .foregroundStyle(Palette.textFaint)
                                    }
                                    Spacer()
                                    Text(statusLabel(t.status))
                                        .font(.system(size: TypeScale.secondary))
                                        .foregroundStyle(t.status == "answered" ? Palette.accent : Palette.textMuted)
                                }
                                .frame(minHeight: Space.touch)
                            }
                            .listRowBackground(Palette.ink)
                        }
                        if s.nextCursor != nil {
                            Button { viewModel.loadOlderTickets() } label: {
                                Text("help_older").foregroundStyle(Palette.accent)
                                    .frame(minHeight: Space.touch)
                            }
                            .disabled(s.loadingMore)
                            .listRowBackground(Palette.ink)
                        }
                        if let failure = s.error, !s.tickets.isEmpty {
                            SendFailure(failure: failure).listRowBackground(Palette.ink)
                        }
                    } header: {
                        Text("help_mine")
                            .font(.system(size: TypeScale.caption).weight(.medium))
                            .foregroundStyle(Palette.text.opacity(0.45))
                    }
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
        }
        .background(Palette.ink)
        .task {
            viewModel.loadTickets(force: false)
            for await state in viewModel.help {
                current = state
                // Cleared by the view model after a successful send.
                if state.subject.isEmpty && !subject.isEmpty && !state.sending { subject = "" }
                if state.body.isEmpty && !message.isEmpty && !state.sending { message = "" }
            }
        }
    }

    private var form: some View {
        let fields = rejectedFields(s.sendError)
        return VStack(alignment: .leading, spacing: Space.small) {
            Text("help_new")
                .font(.system(size: TypeScale.body).weight(.semibold))
                .foregroundStyle(Palette.text)
            HelpField(hint: "help_subject", text: $subject, lines: 1,
                      error: s.subjectMissing ? String(localized: "help_need_subject") : fields?["subject"])
                .onChange(of: subject) { _, new in viewModel.onSubjectChanged(text: new) }
            HelpField(hint: "help_message", text: $message, lines: 4,
                      error: s.bodyMissing ? String(localized: "help_need_message") : fields?["body"])
                .onChange(of: message) { _, new in viewModel.onBodyChanged(text: new) }
            if let failure = s.sendError, fields?.isEmpty ?? true {
                SendFailure(failure: failure)
            }
            SendButton(sending: s.sending) {
                viewModel.submitTicket(onOpened: { id in onOpenTicket(id) })
            }
        }
    }
}

struct HelpTicketScreen: View {
    let viewModel: SupportViewModel
    let onBack: () -> Void

    @State private var current: HelpTicketUiState?
    private var s: HelpTicketUiState { current ?? viewModel.ticket.value }
    @State private var draft = ""

    var body: some View {
        VStack(spacing: 0) {
            Header(title: s.thread?.subject ?? String(localized: "me_help"), onBack: onBack)
            if let thread = s.thread {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: Space.tight) {
                        Text(statusLabel(thread.status))
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Palette.textMuted)
                        ForEach(thread.messages, id: \.id) { m in
                            VStack(alignment: .leading, spacing: 3) {
                                Text((m.fromStaff ? String(localized: "help_staff") : String(localized: "help_you"))
                                     + " · " + whenText(m.createdAt, time: true))
                                    .font(.system(size: TypeScale.caption))
                                    .foregroundStyle(Palette.textFaint)
                                Text(m.body).font(.system(size: 14)).foregroundStyle(Palette.text)
                            }
                            .padding(.horizontal, 13).padding(.vertical, 9)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(RoundedRectangle(cornerRadius: 14)
                                .fill(m.fromStaff ? Palette.surface : Palette.accent.opacity(0.18)))
                        }
                    }
                    .padding(.horizontal, Space.edge)
                }
                let fields = rejectedFields(s.sendError)
                VStack(alignment: .leading, spacing: Space.small) {
                    if let failure = s.sendError, fields?.isEmpty ?? true {
                        SendFailure(failure: failure)
                    }
                    HelpField(hint: "help_reply_hint", text: $draft, lines: 2,
                              error: s.draftMissing ? String(localized: "help_need_message") : fields?["body"])
                        .onChange(of: draft) { _, new in viewModel.onReplyChanged(text: new) }
                    SendButton(sending: s.sending) { viewModel.sendReply() }
                }
                .padding(.horizontal, Space.edge)
                .padding(.vertical, 10)
            } else if s.loading {
                Spacer(); LoadingBlock(); Spacer()
            } else if let failure = s.error {
                Spacer()
                if case .notFound = onEnum(of: failure) {
                    EmptyBlock(message: String(localized: "help_not_found"))
                } else {
                    FailureBlock(failure: failure) { viewModel.retryTicket() }
                }
                Spacer()
            } else {
                Spacer()
            }
        }
        .background(Palette.ink)
        .task {
            for await state in viewModel.ticket {
                current = state
                if state.draft.isEmpty && !draft.isEmpty && !state.sending { draft = "" }
            }
        }
    }
}

/// The customer's tax invoices; each opens its printable copy on the web.
struct InvoicesScreen: View {
    let viewModel: SupportViewModel
    let onBack: () -> Void

    @Environment(\.openURL) private var openURL
    @State private var current: InvoicesUiState?
    private var s: InvoicesUiState { current ?? viewModel.invoices.value }

    var body: some View {
        VStack(spacing: 0) {
            Header(title: String(localized: "me_invoices"), onBack: onBack)
            List {
                if s.loading && !s.loaded {
                    LoadingBlock().listRowBackground(Palette.ink)
                } else if let failure = s.error, s.items.isEmpty {
                    FailureBlock(failure: failure) { viewModel.loadInvoices(force: true) }
                        .listRowBackground(Palette.ink)
                } else if s.items.isEmpty {
                    EmptyBlock(message: String(localized: "invoices_empty")).listRowBackground(Palette.ink)
                } else {
                    Text("invoices_print_hint")
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.textMuted)
                        .listRowBackground(Palette.ink)
                }
                ForEach(s.items, id: \.id) { inv in
                    Button {
                        let path = (isArabicLocale ? "/ar/invoices/" : "/en/invoices/") + inv.id
                        if let url = URL(string: Config.webBaseUrl + path) { openURL(url) }
                    } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(inv.number)
                                    .font(.system(size: TypeScale.body))
                                    .foregroundStyle(Palette.text)
                                Text(String(inv.supplyDate.prefix(10))
                                     + (inv.kind == "credit_note" ? " · " + String(localized: "invoices_credit") : ""))
                                    .font(.system(size: TypeScale.caption))
                                    .foregroundStyle(Palette.textFaint)
                            }
                            Spacer()
                            Text(amount(inv.total.amount, inv.total.currency))
                                .font(.system(size: TypeScale.body))
                                .foregroundStyle(Palette.text)
                        }
                        .frame(minHeight: Space.touch)
                    }
                    .listRowBackground(Palette.ink)
                }
                if s.nextCursor != nil {
                    Button { viewModel.loadOlderInvoices() } label: {
                        Text("help_older").foregroundStyle(Palette.accent).frame(minHeight: Space.touch)
                    }
                    .disabled(s.loadingMore)
                    .listRowBackground(Palette.ink)
                }
            }
            .listStyle(.plain)
            .scrollContentBackground(.hidden)
        }
        .background(Palette.ink)
        .task {
            viewModel.loadInvoices(force: false)
            for await state in viewModel.invoices { current = state }
        }
    }
}
