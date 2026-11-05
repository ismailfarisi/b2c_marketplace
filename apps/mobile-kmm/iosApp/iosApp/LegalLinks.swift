import SwiftUI

/// Terms and Privacy policy (and Help or the kitchen portal, on Me), opened on the web in the app's language.
struct LegalLinks: View {
    var tint: Color
    var showHelp = false
    /// The kitchen console link; Me shows it to every signed-in viewer.
    var showKitchen: Bool?

    var body: some View {
        HStack(spacing: 20) {
            if showHelp { link("legal_help", page: "support") }
            // The kitchen console is web-only; vendors reach it from here.
            if showKitchen ?? showHelp { link("legal_kitchen", page: "console") }
            link("legal_terms", page: "terms")
            link("legal_privacy", page: "privacy")
        }
    }

    @ViewBuilder
    private func link(_ label: LocalizedStringKey, page: String) -> some View {
        if let url = URL(string: Config.webBaseUrl + (isArabicLocale ? "/ar/" : "/en/") + page) {
            Link(destination: url) {
                Text(label)
                    .font(.system(size: TypeScale.secondary))
                    .underline()
                    .foregroundStyle(tint)
                    .frame(minHeight: Space.touch)
            }
        }
    }
}
