import SwiftUI
import Shared

/// The one screen between an account-shaped action and the account.
///
/// It is here rather than at launch because this is the first thing in the
/// product that genuinely cannot be anonymous: a listing belongs to someone and
/// a buyer has to be able to message them. The feed, the filters and search all
/// work signed out, and asking a visitor for a phone number before they have
/// seen a reel is how a discovery app loses the visit.
///
/// An SMS login has more dead ends than any other screen in the product. Four
/// are handled explicitly — a wrong code, a dead challenge, a rate limit, and
/// no connection — because the correct next action differs for every one, and
/// getting it wrong strands someone at the door.
struct SignInGate: View {
    let viewModel: SignInViewModel
    let onCancel: () -> Void
    var titleKey = "signin_title"
    var whyKey = "signin_why"

    @State private var current: SignInUiState?
    private var state: SignInUiState { current ?? viewModel.state.value }
    @State private var phone: String = ""
    @State private var code: String = ""
    @State private var now: Int64 = 0

    @FocusState private var focused: Bool

    private var onCodeStep: Bool { !state.challengeId.isEmpty }
    private var cooldown: Int { max(0, Int((state.resendAtMillis - now) / 1000)) }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button(action: onCancel) {
                Image(systemName: "xmark")
                    .font(.system(size: 18))
                    .foregroundStyle(Palette.text)
                    .frame(minWidth: Space.touch, minHeight: Space.touch)
            }
            .accessibilityLabel(Text("action_close"))
            .padding(.bottom, Space.xl)

            Text(LocalizedStringKey(titleKey))
                .font(.system(size: 21).weight(.semibold))
                .foregroundStyle(Palette.text)

            Text(onCodeStep
                 ? String(format: String(localized: "signin_sent_to"),
                          phone.trimmingCharacters(in: .whitespaces))
                 : NSLocalizedString(whyKey, comment: ""))
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Palette.text.opacity(0.55))
                .padding(.top, Space.small)
                .padding(.bottom, 26)

            if onCodeStep {
                field($code, placeholder: "signin_code_hint", keyboard: .numberPad)
                    .onChange(of: code) { _, new in
                        viewModel.onCode(value: new)
                        // Submitted on the sixth digit rather than on a button
                        // press. The code is a known fixed length, so there is
                        // nothing to confirm — asking for a tap after the last
                        // digit is a step that exists only because the form was
                        // built before anyone thought about it.
                        if state.code.count == 6 && !state.busy { viewModel.verify() }
                    }

                primaryButton("signin_verify", enabled: state.code.count == 6 && !state.busy) {
                    viewModel.verify()
                }

                HStack {
                    Button { viewModel.changeNumber() } label: {
                        Text("signin_change_number")
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Palette.text.opacity(0.5))
                    }
                    Spacer()
                    Button { viewModel.sendCode(nowMillis: Self.millis()) } label: {
                        // The countdown is shown rather than the button being
                        // silently inert. A disabled control with no explanation
                        // is read as a bug, and this one is disabled for a
                        // reason the viewer would agree with if told.
                        Text(cooldown > 0
                             ? String(format: String(localized: "signin_resend_in"), cooldown)
                             : String(localized: "signin_resend"))
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(cooldown > 0 ? Palette.text.opacity(0.3) : Palette.accent)
                    }
                    .disabled(cooldown > 0 || state.busy)
                }
                .padding(.top, Space.large)
            } else {
                field($phone, placeholder: "signin_phone_hint", keyboard: .phonePad)
                    .onChange(of: phone) { _, new in viewModel.onPhone(value: new) }

                primaryButton("signin_send_code",
                              enabled: !phone.trimmingCharacters(in: .whitespaces).isEmpty && !state.busy) {
                    viewModel.sendCode(nowMillis: Self.millis())
                }
            }

            if let problem = state.error {
                Text(Self.words(problem))
                    .font(.system(size: 12))
                    .foregroundStyle(Palette.accent)
                    .padding(.top, 14)
            }

            Spacer()
        }
        .padding(.horizontal, 22)
        .padding(.vertical, Space.large)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Palette.ink)
        .task {
            for await s in viewModel.state {
                current = s
                if s.code != code { code = s.code }
            }
        }
        .task {
            // Ticks only while a cooldown is running. The deadline is stored as
            // an absolute instant in the view model rather than a counter here,
            // so it survives a backgrounding — a counter decremented by the UI
            // resets to thirty every time the view is recreated, which turns the
            // cooldown into a suggestion.
            while !Task.isCancelled {
                now = Self.millis()
                try? await Task.sleep(for: .milliseconds(500))
            }
        }
        .onChange(of: onCodeStep) { _, _ in focused = true }
    }

    private func field(
        _ binding: Binding<String>,
        placeholder: LocalizedStringKey,
        keyboard: UIKeyboardType
    ) -> some View {
        TextField("", text: binding, prompt:
            Text(placeholder).foregroundStyle(Palette.textGhost))
            .textFieldStyle(.plain)
            .keyboardType(keyboard)
            .focused($focused)
            .foregroundStyle(Palette.text)
            .font(.system(size: 16))
            .padding(14)
            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Palette.line, lineWidth: 1))
    }

    private func primaryButton(
        _ key: LocalizedStringKey, enabled: Bool, action: @escaping () -> Void
    ) -> some View {
        Button(action: action) {
            Text(key)
                .font(.system(size: TypeScale.body).weight(.semibold))
                .foregroundStyle(.white)
                .frame(maxWidth: .infinity, minHeight: Space.touch)
                .padding(.vertical, 10)
                .background(RoundedRectangle(cornerRadius: 10)
                    .fill(enabled ? Palette.accent : Palette.accent.opacity(0.3)))
        }
        .disabled(!enabled)
        .padding(.top, Space.large)
    }

    private static func millis() -> Int64 { Int64(Date().timeIntervalSince1970 * 1000) }

    /// Words for a problem, chosen by what the viewer should do next.
    ///
    /// Every one ends in an instruction. "Something went wrong" was the message
    /// this screen used to show for all of them, and it is the one message that
    /// cannot be acted on.
    private static func words(_ problem: SignInProblem) -> String {
        switch problem {
        case .badPhone: return String(localized: "signin_err_phone")
        case .wrongCode: return String(localized: "signin_err_code")
        case .needsNewCode: return String(localized: "signin_err_stale")
        case .tooMany: return String(localized: "signin_err_too_many")
        case .offline: return String(localized: "signin_err_offline")
        default: return String(localized: "signin_err_unknown")
        }
    }
}
