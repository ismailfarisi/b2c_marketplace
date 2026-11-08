import type { RotationVariety, RotationVarietyWarning } from "@/lib/customer-api";
import { tagLabel } from "@/components/meals/kitchen-card";

type Locale = "en" | "ar";

const WEEKDAYS = {
  en: ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"],
  ar: ["الأحد", "الاثنين", "الثلاثاء", "الأربعاء", "الخميس", "الجمعة", "السبت"],
};

export const VARIETY_COPY = {
  en: {
    title: "Variety",
    level: { low: "Low variety", good: "Good variety", great: "Great variety" },
    hint: {
      low: "Your weeks may feel samey. Swapping a kitchen helps.",
      good: "A decent mix. A small swap could make it even better.",
      great: "Four different tastes across your month.",
    },
    advisory: "This is only advice — you can still start any four kitchens.",
    sameCuisine: (cuisines: string, n: number) =>
      `${n} of your kitchens cook ${cuisines} food.`,
    repeatedDish: (dish: string, day: string, n: number) =>
      `${dish} comes from ${n} kitchens on ${day}s.`,
    acrossWeeks: (dish: string, n: number) =>
      `${dish} shows up in ${n} of your weeks.`,
    and: " and ",
  },
  ar: {
    title: "التنوّع",
    level: { low: "تنوّع قليل", good: "تنوّع جيد", great: "تنوّع ممتاز" },
    hint: {
      low: "قد تتشابه أسابيعك. استبدال مطبخ واحد يساعد.",
      good: "مزيج جيد. تبديل بسيط قد يجعله أفضل.",
      great: "أربعة مذاقات مختلفة على مدار الشهر.",
    },
    advisory: "هذه مجرد نصيحة، ويمكنك بدء التناوب بأي أربعة مطابخ.",
    sameCuisine: (cuisines: string, n: number) =>
      `${n} من مطابخك تقدّم أكلات ${cuisines}.`,
    repeatedDish: (dish: string, day: string, n: number) =>
      n === 2
        ? `${dish} يأتي من مطبخين في أيام ${day}.`
        : `${dish} يأتي من ${n} مطابخ في أيام ${day}.`,
    acrossWeeks: (dish: string, n: number) =>
      n === 2
        ? `${dish} يتكرر في أسبوعين من أسابيعك.`
        : `${dish} يتكرر في ${n} من أسابيعك.`,
    and: " و",
  },
};

// An English dish name inside an Arabic sentence (or the reverse) is isolated
// with FSI/PDI so the surrounding numbers and words keep their order.
const isolate = (s: string) => (s ? `\u2068${s}\u2069` : s);

export function varietyWarningText(w: RotationVarietyWarning, locale: Locale): string {
  const t = VARIETY_COPY[locale];
  const dish = isolate((locale === "ar" && w.dish_ar) || w.dish || "");
  switch (w.code) {
    case "same_cuisine":
      return t.sameCuisine(
        (w.cuisines ?? []).map((c) => tagLabel(c, locale)).join(t.and),
        w.kitchens,
      );
    case "repeated_dish":
      return t.repeatedDish(dish, WEEKDAYS[locale][w.weekday ?? 0] ?? "", w.kitchens);
    default:
      return t.acrossWeeks(dish, w.kitchens);
  }
}

/** The quote's variety check: a three-step meter and plain-language warnings. */
export function VarietyMeter({ variety, locale }: { variety: RotationVariety; locale: Locale }) {
  const t = VARIETY_COPY[locale];
  const steps = variety.level === "great" ? 3 : variety.level === "good" ? 2 : 1;
  return (
    <section className={`ms-variety ms-variety-${variety.level}`} aria-label={t.title}>
      <p className="ms-variety-head">
        <strong>{t.level[variety.level]}</strong>
        <span className="ms-variety-bar" role="meter" aria-valuemin={0} aria-valuemax={100}
          aria-valuenow={variety.score} aria-valuetext={t.level[variety.level]}>
          {[1, 2, 3].map((i) => (
            <span key={i} className={i <= steps ? "on" : undefined} />
          ))}
        </span>
      </p>
      <p className="ms-sub">{t.hint[variety.level]}</p>
      {variety.warnings.length > 0 && (
        <ul className="ms-variety-warnings">
          {variety.warnings.map((w, i) => (
            <li key={i} dir="auto">
              {varietyWarningText(w, locale)}
            </li>
          ))}
        </ul>
      )}
      {(variety.level !== "great" || variety.warnings.length > 0) && (
        <p className="ms-note">{t.advisory}</p>
      )}
    </section>
  );
}
