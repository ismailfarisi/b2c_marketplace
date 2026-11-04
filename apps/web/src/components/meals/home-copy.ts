/** Home-page wording that the shared kitchen-card COPY doesn't carry. */

type Locale = "en" | "ar";

const arRules = new Intl.PluralRules("ar");

/**
 * "N kitchen(s) deliver(s) …" in Arabic: the noun and verb agree with the
 * count (مطبخ واحد / مطبخان / ٣ مطابخ / ١١ مطبخًا / ١٠٠ مطبخ).
 */
export function arKitchensDeliver(n: number, to: string) {
  switch (arRules.select(n)) {
    case "zero":
      return `لا يوجد مطبخ يوصل ${to}`;
    case "one":
      return `مطبخ واحد يوصل ${to}`;
    case "two":
      return `مطبخان يوصلان ${to}`;
    case "few":
      return `${n} مطابخ توصل ${to}`;
    case "many":
      return `${n} مطبخًا يوصل ${to}`;
    default:
      return `${n} مطبخ يوصل ${to}`;
  }
}

/** "N kitchen(s)" in Arabic, for counts without a verb. */
export function arKitchens(n: number) {
  switch (arRules.select(n)) {
    case "zero":
      return "لا مطابخ";
    case "one":
      return "مطبخ واحد";
    case "two":
      return "مطبخان";
    case "few":
      return `${n} مطابخ`;
    case "many":
      return `${n} مطبخًا`;
    default:
      return `${n} مطبخ`;
  }
}

export const HOME_COPY: Record<
  Locale,
  { noMatches: string; blocked: string; noFix: string }
> = {
  en: {
    noMatches: "No matches",
    blocked:
      "Location is blocked in your browser settings, so we're showing Dubai Marina. Allow location for this site, or choose an area.",
    noFix:
      "We couldn't get your location, so we're showing Dubai Marina. Try again or choose an area.",
  },
  ar: {
    noMatches: "لا توجد نتائج",
    blocked:
      "الموقع محظور في إعدادات المتصفح، لذا نعرض دبي مارينا. اسمح بالموقع لهذا الموقع أو اختر منطقة.",
    noFix:
      "تعذر تحديد موقعك، لذا نعرض دبي مارينا. حاول مرة أخرى أو اختر منطقة.",
  },
};

