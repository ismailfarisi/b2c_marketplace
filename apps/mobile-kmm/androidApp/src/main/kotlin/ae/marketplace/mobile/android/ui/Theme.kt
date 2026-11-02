package ae.marketplace.mobile.android.ui

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The one place a colour, a gap or a type size is decided.
 *
 * # Why this exists
 *
 * Before it, `Accent`, `Ink` and `Line` were declared three times in three
 * packages, with two of the three definitions agreeing by coincidence rather
 * than by reference. Text alpha was a literal at every call site — 0.5f, 0.55f,
 * 0.45f and 0.4f all appeared for what was meant to be "the secondary line",
 * and no two screens picked the same one. Padding was 18.dp on some lists and
 * 22.dp on others for the same left margin, which is visible as a jitter when
 * you switch tabs.
 *
 * None of that is fatal on its own. Together they are the difference between a
 * product and a set of screens, and the cost of fixing it later grows with
 * every screen added.
 *
 * # Why not MaterialTheme
 *
 * The app does install a `MaterialTheme(darkColorScheme())`, and Material's
 * roles — primary, surface, onSurfaceVariant — are a good vocabulary for an app
 * that looks like Material. This one does not: it is a full-bleed video surface
 * with content drawn over it, which is closer to a player than to a form. Its
 * real constraints are "legible on top of an unknown video frame" and "readable
 * in direct sun on a Dubai afternoon", and Material's tonal palettes have
 * nothing to say about either.
 *
 * So this is a small, flat, deliberate set rather than a theme system. The
 * discipline it enforces is only that there *is* a set.
 */
object Palette {

    /** The page: warm cream, the same paper as the meals screens and the web. */
    val Ink = Color(0xFFFFF8F0)

    /** One step up, for anything sitting on the page — cards, thumbnails, fields. Cream-tinted, never pure white. */
    val Surface = Color(0xFFFFFCF7)

    /** Hairlines and field borders. */
    val Line = Color(0xFFF0E2D2)

    /** Chilli Red: price, primary actions and focus. Dark enough for white text (AA). */
    val Accent = Color(0xFFC43B16)

    /** Live, approved, in stock (leaf green). */
    val Good = Color(0xFF276C44)

    /** Rejected, failed, expired. Distinct from [Accent], which is not an alarm. */
    val Bad = Color(0xFFBF2921)

    /** Text on [Ink]. */
    val Text = Color(0xFF1E1410)

    /** The second line of a row: attributes, distances, timestamps. */
    val TextMuted = Color(0xFF76675B)

    /** Labels, counts, section headers — present but not read unless looked for. */
    val TextFaint = Color(0xFFA89889)

    /** Placeholder text inside an empty field. */
    val TextGhost = Color(0xFFC9BBAE)

    /** A scrim for content over a photo or video. */
    val Scrim = Color.Black.copy(alpha = 0.45f)
}

/**
 * The full-bleed video surfaces (feed, reel detail, camera) stay dark: text
 * there sits over an unknown video frame. Accent is the same Chilli Red.
 */
object Video {
    /** Not pure black: an OLED true black makes scroll edges invisible. */
    val Ink = Color(0xFF0E0E10)
    val Surface = Color(0xFF1C1C20)
    val Line = Color(0xFF2A2A2E)
    val Accent = Palette.Accent
    val Text = Color.White
    val TextMuted = Color.White.copy(alpha = 0.55f)
}

/**
 * Spacing, on a 4dp grid with names rather than numbers.
 *
 * The names are the point. `Space.Edge` used at every screen's horizontal
 * margin means the margin is the same on every screen and can be changed in one
 * place; `18.dp` typed sixty times means it cannot.
 */
object Space {
    /** Between a label and the thing it labels. */
    val Hair = 2.dp
    val Tight = 4.dp
    val Small = 8.dp
    val Medium = 12.dp
    val Large = 16.dp
    val XL = 24.dp
    val XXL = 40.dp

    /** The horizontal margin of every full-width screen. One value, everywhere. */
    val Edge = 18.dp

    /**
     * The smallest a tappable thing may be.
     *
     * 48dp is the Android accessibility floor and it is not advisory: several
     * controls in this app were 20–28dp of text with a `clickable` on it, which
     * is a coin-flip to hit for anyone whose hands are not steady.
     */
    val Touch = 48.dp
}

/**
 * Type, as a closed set of roles.
 *
 * Deliberately six sizes. Every additional one is a decision someone has to
 * make on every new screen, and the screens in this product are lists, a
 * viewfinder and a video overlay — none of which needs a typographic range.
 */
object Type {
    val Title = 22.sp
    val Heading = 17.sp
    val Body = 15.sp
    val Secondary = 13.sp
    val Caption = 11.sp

    /** Badges and pills only. Below this, an uppercase label stops being legible. */
    val Micro = 9.sp
}

object Shape {
    val Field = 10.dp
    val Card = 8.dp
    val Pill = 4.dp
    val Thumb = 6.dp
}

/**
 * Whether this build is being read in Arabic.
 *
 * The server puts both language forms of every vertical-supplied string on the
 * wire — see `model/Localised.kt` for why — so the client has to choose, and
 * this is the choice.
 *
 * Read from the configuration rather than from `LocalLayoutDirection`. The two
 * agree today, because Arabic is the only RTL language this app ships and the
 * only non-English one. They would stop agreeing the day Urdu or Farsi is added
 * — both RTL, neither Arabic — and the failure would be silent: Arabic content
 * served to a Farsi reader, which is worse than the English fallback.
 */
@androidx.compose.runtime.Composable
fun rememberArabic(): Boolean {
    val config = androidx.compose.ui.platform.LocalConfiguration.current
    return androidx.compose.runtime.remember(config) {
        ae.marketplace.mobile.model.isArabic(config.locales[0].language)
    }
}
