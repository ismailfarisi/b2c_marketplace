import SwiftUI

/// Terms and Privacy policy, opened on the web in the app's language.
struct LegalLinks: View {
    var tint: Color
    var showHelp = false

    var body: some View {
        HStack(spacing: 20) {
            if showHelp { link("legal_help", page: "support") }
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
