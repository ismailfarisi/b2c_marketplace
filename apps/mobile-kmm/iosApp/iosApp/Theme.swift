import SwiftUI

/// The same palette the Android app uses, and for the same reasons.
///
/// Duplicated rather than shared, deliberately. Colours could live in
/// `commonMain` and be exported — but a `Color` is not a value the shared layer
/// has any business holding: it would need a platform type on each side anyway,
/// and the only thing crossing the boundary would be six hex integers. What
/// matters is that the two files agree, and the way to keep them agreeing is
/// that each is small enough to read side by side.
///
/// The reasoning behind the choices is in
/// `androidApp/.../ui/Theme.kt` and is not repeated here.
enum Palette {
    /// Not pure black: an OLED true black makes scroll edges invisible.
    static let ink = Color(red: 0.055, green: 0.055, blue: 0.063)
    static let surface = Color(red: 0.110, green: 0.110, blue: 0.125)
    static let line = Color(red: 0.165, green: 0.165, blue: 0.180)

    /// Burnt orange rather than blue — it has to survive being drawn over
    /// arbitrary video, and blue disappears against the sky in roughly half the
    /// car reels on this platform.
    static let accent = Color(red: 0.706, green: 0.396, blue: 0.165)

    static let good = Color(red: 0.298, green: 0.686, blue: 0.314)
    static let bad = Color(red: 0.851, green: 0.325, blue: 0.310)

    /// The filled heart, and only the filled heart. See the Android note: a
    /// liked heart in the price's orange reads as decoration, not as state.
    static let heart = Color(red: 1.0, green: 0.188, blue: 0.251)

    static let text = Color.white
    static let textMuted = Color.white.opacity(0.55)
    static let textFaint = Color.white.opacity(0.38)
    static let textGhost = Color.white.opacity(0.25)
}

enum Space {
    static let hair: CGFloat = 2
    static let tight: CGFloat = 4
    static let small: CGFloat = 8
    static let medium: CGFloat = 12
    static let large: CGFloat = 16
    static let xl: CGFloat = 24
    static let xxl: CGFloat = 40

    /// The horizontal margin of every full-width screen.
    static let edge: CGFloat = 18

    /// Apple's minimum tappable size. Smaller than Android's 48dp and it is the
    /// platform's number, not a compromise between them.
    static let touch: CGFloat = 44
}

/// Corner radii, matching `ui/Theme.kt`'s `Shape`.
///
/// Four values and no fifth. A radius picked per view is how two cards on the
/// same screen end up 8 and 12, which nobody reports and everybody feels.
enum Radius {
    static let field: CGFloat = 10
    static let card: CGFloat = 12
    static let pill: CGFloat = 4
    static let thumb: CGFloat = 10

    /// Primary and secondary action buttons.
    static let button: CGFloat = 10

    /// Bottom sheets.
    static let sheet: CGFloat = 16
}

enum TypeScale {
    static let title: CGFloat = 22
    static let heading: CGFloat = 17
    static let body: CGFloat = 15
    static let secondary: CGFloat = 13
    static let caption: CGFloat = 11
    static let micro: CGFloat = 9
}

/// Whether the app is being read in Arabic.
///
/// Same question the Compose side asks through `rememberArabic`, and answered
/// the same way — from the locale rather than from the layout direction. The
/// two agree today because Arabic is the only right-to-left language shipped;
/// they would stop agreeing the day Urdu or Farsi is added, and the failure
/// would be Arabic content served to a Farsi reader.
var isArabicLocale: Bool {
    Locale.current.language.languageCode?.identifier == "ar"
}

/// Picks between the two forms of a server-supplied string.
///
/// The API sends `badge` and `badge_ar` together so one cached feed page serves
/// both readers. Falls back rather than blanking: a vertical that has not
/// translated a badge should show the English one, because a viewer reading
/// "USED CAR" is mildly jarred and a viewer reading an empty badge has lost
/// information the layout still reserves space for.
func localised(_ en: String, _ ar: String) -> String {
    isArabicLocale && !ar.trimmingCharacters(in: .whitespaces).isEmpty ? ar : en
}

/// Thousands separator, matching the Android `group()`.
///
/// Western digits regardless of locale. Arabic-Indic digits are correct in a
/// great deal of Arabic typesetting and wrong here: Gulf prices are written in
/// Western digits almost universally, and a price rendered ١٨٩٬٠٠٠ beside a
/// card showing 189,000 reads as two different cars.
func grouped(_ n: Int64) -> String {
    let formatter = NumberFormatter()
    formatter.numberStyle = .decimal
    formatter.groupingSeparator = ","
    formatter.locale = Locale(identifier: "en_US_POSIX")
    return formatter.string(from: NSNumber(value: n)) ?? "\(n)"
}
