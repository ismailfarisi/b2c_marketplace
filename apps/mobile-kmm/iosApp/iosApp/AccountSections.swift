import SwiftUI
import Shared
import UserNotifications

/// The account half of Me: who is signed in, saved addresses, the marketing
/// opt-out, notifications and language. Rows for the Me list, drawn first
/// because most people here are meal customers rather than sellers.
struct AccountSections: View {
    let vm: AccountViewModel
    let state: AccountUiState
    let pushOff: Bool
    let onEdit: () -> Void
    let onRemove: (Address) -> Void

    var body: some View {
        if state.loading && state.profile == nil {
            LoadingBlock().listRowBackground(Palette.ink)
        } else if let failure = state.error, state.profile == nil {
            FailureBlock(failure: failure) { vm.load(force: true) }.listRowBackground(Palette.ink)
        } else {
            profile
            addresses
            notifications
            language
        }
    }

    private func heading(_ key: LocalizedStringKey) -> some View {
        Text(key)
            .font(.system(size: TypeScale.caption).weight(.medium))
            .foregroundStyle(Palette.text.opacity(0.6))
    }

    @ViewBuilder
    private var profile: some View {
        if let p = state.profile {
            Section {
                HStack {
                    VStack(alignment: .leading, spacing: Space.hair) {
                        Text(p.displayName.flatMap { $0.isEmpty ? nil : $0 } ?? String(localized: "account_no_name"))
                            .font(.system(size: TypeScale.body).weight(.semibold))
                            .foregroundStyle(Palette.text)
                        Text(p.email ?? String(localized: "account_no_email"))
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Palette.textMuted)
                        // LTR isolate: a +971 number must not be reordered in Arabic.
                        Text(String(format: NSLocalizedString("account_phone", comment: ""), "\u{2066}\(p.phone)\u{2069}"))
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Palette.textMuted)
                    }
                    Spacer()
                    Button(action: onEdit) {
                        Text("account_edit")
                            .font(.system(size: TypeScale.secondary).weight(.semibold))
                            .foregroundStyle(Palette.accent)
                            .frame(minHeight: Space.touch)
                    }
                    .buttonStyle(.borderless)
                }
                .listRowBackground(Palette.ink)
            } header: { heading("account_profile") }
        }
    }

    private var addresses: some View {
        Section {
            if state.addresses.isEmpty {
                Text("account_no_addresses")
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Palette.textMuted)
                    .listRowBackground(Palette.ink)
            }
            ForEach(state.addresses, id: \.id) { a in
                VStack(alignment: .leading, spacing: Space.tight) {
                    HStack {
                        Text(a.label)
                            .font(.system(size: TypeScale.body).weight(.semibold))
                            .foregroundStyle(Palette.text)
                        Spacer()
                        if a.isDefault {
                            Text("account_default")
                                .font(.system(size: TypeScale.caption).weight(.bold))
                                .foregroundStyle(Palette.good)
                        }
                    }
                    Text(a.line)
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.textMuted)
                    HStack(spacing: Space.large) {
                        if !a.isDefault {
                            Button { vm.makeDefault(id: a.id) } label: {
                                Text("account_make_default")
                                    .font(.system(size: TypeScale.secondary).weight(.semibold))
                                    .foregroundStyle(Palette.accent)
                                    .frame(minHeight: Space.touch)
                            }
                            .buttonStyle(.borderless)
                        }
                        Button { onRemove(a) } label: {
                            Text("account_remove")
                                .font(.system(size: TypeScale.secondary).weight(.semibold))
                                .foregroundStyle(Palette.bad)
                                .frame(minHeight: Space.touch)
                        }
                        .buttonStyle(.borderless)
                    }
                    .disabled(state.addressBusy != nil)
                }
                .listRowBackground(Palette.ink)
            }
            if let failure = state.addressError {
                Text(failure.words)
                    .font(.system(size: TypeScale.secondary))
                    .foregroundStyle(Palette.bad)
                    .listRowBackground(Palette.ink)
            }
        } header: { heading("account_addresses") }
    }

    private var notifications: some View {
        Section {
            if pushOff {
                HStack {
                    Text("account_push_off")
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.text)
                    Spacer()
                    Button { openSettings() } label: {
                        Text("account_open_settings")
                            .font(.system(size: TypeScale.secondary).weight(.semibold))
                            .foregroundStyle(Palette.accent)
                            .frame(minHeight: Space.touch)
                    }
                    .buttonStyle(.borderless)
                }
                .listRowBackground(Palette.ink)
            }
            Text("account_marketing_note")
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Palette.textMuted)
                .listRowBackground(Palette.ink)
            ForEach(state.preferences, id: \.channel) { p in
                VStack(alignment: .leading, spacing: Space.tight) {
                    Toggle(isOn: Binding(get: { p.allowed }, set: { vm.setPreference(pref: p, allowed: $0) })) {
                        Text(LocalizedStringKey("account_marketing_" + p.channel))
                            .font(.system(size: TypeScale.body))
                            .foregroundStyle(Palette.text)
                    }
                    .tint(Palette.accent)
                    .disabled(!p.optional)
                    if let failure = state.preferenceErrors[p.channel] {
                        Text(failure.words)
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Palette.bad)
                    }
                }
                .listRowBackground(Palette.ink)
            }
        } header: { heading("account_notifications") }
    }

    /// iOS keeps each app's language on its Settings page.
    private var language: some View {
        Section {
            Button { openSettings() } label: {
                HStack {
                    VStack(alignment: .leading, spacing: Space.hair) {
                        Text("account_language_current")
                            .font(.system(size: TypeScale.body))
                            .foregroundStyle(Palette.text)
                        Text("account_language_settings")
                            .font(.system(size: TypeScale.secondary))
                            .foregroundStyle(Palette.textMuted)
                    }
                    Spacer()
                    Image(systemName: "chevron.forward")
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.textFaint)
                }
                .frame(minHeight: Space.touch)
            }
            .listRowBackground(Palette.ink)
        } header: { heading("account_language") }
    }

    private func openSettings() {
        if let url = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(url) }
    }
}

/// Name and email. The phone is the sign-in identity and changes via support.
struct ProfileEditor: View {
    let vm: AccountViewModel
    let profile: Profile
    let saving: Bool
    let failure: Failure?
    let onClose: () -> Void

    @State private var name = ""
    @State private var email = ""

    private var fields: [String: String] {
        guard let failure, case .rejected(let r) = onEnum(of: failure) else { return [:] }
        return r.fields
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField(String(localized: "account_name"), text: $name)
                        .textContentType(.name)
                    if let e = fields["display_name"] { Text(e).foregroundStyle(Palette.bad) }
                    TextField(String(localized: "account_email"), text: $email)
                        .keyboardType(.emailAddress)
                        .textContentType(.emailAddress)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    if let e = fields["email"] { Text(e).foregroundStyle(Palette.bad) }
                } footer: {
                    Text("account_phone_note")
                }
                if let failure, fields.isEmpty {
                    Text(failure.words).foregroundStyle(Palette.bad)
                }
            }
            .navigationTitle(Text("account_profile"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(action: onClose) { Text("account_cancel") }.disabled(saving)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button { vm.saveProfile(displayName: name, email: email) } label: { Text("account_save") }
                        .disabled(saving)
                }
            }
        }
        .onAppear {
            name = profile.displayName ?? ""
            email = profile.email ?? ""
            vm.clearProfileError()
        }
    }
}
