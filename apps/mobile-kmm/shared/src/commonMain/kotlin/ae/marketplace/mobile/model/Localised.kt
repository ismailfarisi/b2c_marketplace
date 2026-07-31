package ae.marketplace.mobile.model

/**
 * Picking the right one of a pair of server-supplied strings.
 *
 * # Why the server sends both
 *
 * Every vertical's provider declares its words twice — `Badge`/`BadgeAr`,
 * `Label`/`LabelAr` — and the API puts both on the wire rather than choosing.
 * That looked like indecision and is not: a feed page is cached, and a response
 * whose *content* depends on an `Accept-Language` header is a response that has
 * to vary by it. Sending both means one cache entry serves an English and an
 * Arabic reader, and the choice happens where it costs nothing.
 *
 * # What was going wrong
 *
 * Nothing was choosing. Every screen read `.badge`, `.title` and `.label`
 * directly, so an Arabic build rendered Arabic chrome around an English car:
 * "للإيجار" nowhere to be seen, "FOR RENT · PERMIT 7128394" and a button
 * reading "Enquire" under a nav bar reading "الرئيسية".
 *
 * It also produced a subtler failure worth naming, because it is the one that
 * survives a casual look. "2 bed apartment" placed in a right-to-left paragraph
 * renders as "bed apartment 2" — the bidirectional algorithm moves the leading
 * number to the other end, correctly, because in an RTL context that is where a
 * trailing number belongs. The text is not corrupt and no amount of staring at
 * the string will show the bug. The only fix is to not be showing English.
 */

/**
 * The Arabic form when the viewer reads Arabic and the vertical supplied one.
 *
 * Falls back rather than blanking. A vertical that has not translated its badge
 * yet should show the English one — a viewer reading "USED CAR" is mildly
 * jarred; a viewer reading an empty badge has lost information the layout still
 * reserves space for.
 */
fun localised(en: String, ar: String, arabic: Boolean): String =
    if (arabic && ar.isNotBlank()) ar else en

/**
 * Whether a locale tag means Arabic.
 *
 * Prefix-matched, because the tag arriving here is whatever the platform
 * reports — "ar", "ar-AE", "ar_AE", "ar-Arab-AE" are all the same answer, and a
 * build that only recognised the exact string "ar" would fall back to English
 * on every real device in the country.
 */
fun isArabic(locale: String): Boolean =
    locale.startsWith("ar", ignoreCase = true)

fun SubjectSummary.badgeFor(arabic: Boolean): String = localised(badge, badgeAr, arabic)
fun SubjectSummary.titleFor(arabic: Boolean): String = localised(title, titleAr, arabic)
fun SubjectSummary.lineFor(arabic: Boolean): String = localised(line, lineAr, arabic)
fun SubjectSummary.priceSuffixFor(arabic: Boolean): String =
    localised(priceSuffix, priceSuffixAr, arabic)

fun ResolvedAction.labelFor(arabic: Boolean): String = localised(label, labelAr, arabic)
