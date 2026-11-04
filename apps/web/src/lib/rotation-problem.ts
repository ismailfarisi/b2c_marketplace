import { ApiError } from "@marketplace/api-client";

import { problemMessage } from "@/lib/problem";

type Locale = "en" | "ar";

/**
 * Rotation preview/create/renew refusals, by the stable code the API sends.
 * The API's own messages are English-only.
 */
const ROTATION: Record<string, { en: string; ar: string }> = {
  four_kitchens: {
    en: "A rotation needs exactly 4 kitchens.",
    ar: "يحتاج التناوب إلى ٤ مطابخ بالضبط.",
  },
  same_kitchen_twice: {
    en: "Choose 4 different kitchens.",
    ar: "اختر ٤ مطابخ مختلفة.",
  },
  not_in_rotation: {
    en: "One of these kitchens is no longer in the rotation. Swap it to continue.",
    ar: "أحد هذه المطابخ لم يعد ضمن التناوب. استبدله للمتابعة.",
  },
  plan_unavailable: {
    en: "One of these kitchens' plans is no longer on sale. Swap it to continue.",
    ar: "خطة أحد هذه المطابخ لم تعد متاحة. استبدله للمتابعة.",
  },
  incompatible_plans: {
    en: "These kitchens' plans don't cover the same number of days and meals. Swap one to continue.",
    ar: "خطط هذه المطابخ لا تغطي نفس عدد الأيام والوجبات. استبدل أحدها للمتابعة.",
  },
  no_delivery_window: {
    en: "One of these kitchens has no delivery time for your meals. Swap it to continue.",
    ar: "أحد هذه المطابخ ليس لديه وقت توصيل لوجباتك. استبدله للمتابعة.",
  },
  wrong_day_count: {
    en: "Choose as many delivery days as the plans cover each week.",
    ar: "اختر عدد أيام التوصيل الذي تغطيه الخطط كل أسبوع.",
  },
  not_a_delivery_day: {
    en: "One of these kitchens doesn't deliver on a day you chose.",
    ar: "أحد هذه المطابخ لا يوصل في يوم اخترته.",
  },
  in_the_past: {
    en: "Choose a later start date.",
    ar: "اختر تاريخ بدء لاحقًا.",
  },
  outside_delivery_area: {
    en: "One of these kitchens doesn't deliver to this address.",
    ar: "أحد هذه المطابخ لا يوصل إلى هذا العنوان.",
  },
  unknown: {
    en: "That promo code doesn't exist.",
    ar: "رمز الخصم هذا غير موجود.",
  },
  unavailable: {
    en: "Promo codes can't be used right now.",
    ar: "لا يمكن استخدام رموز الخصم الآن.",
  },
  already_renewed: {
    en: "This rotation has already been renewed.",
    ar: "تم تجديد هذا التناوب مسبقًا.",
  },
  not_paid: {
    en: "Pay for this rotation before renewing it.",
    ar: "ادفع لهذا التناوب قبل تجديده.",
  },
  not_renewable: {
    en: "This rotation can't be renewed.",
    ar: "لا يمكن تجديد هذا التناوب.",
  },
  not_live: {
    en: "Only a running rotation can change renewal.",
    ar: "لا يمكن تغيير التجديد إلا لتناوب جارٍ.",
  },
  already_cancelled: {
    en: "This rotation was already cancelled.",
    ar: "تم إلغاء هذا التناوب مسبقًا.",
  },
};

/** One sentence for a failed rotation call, in the reader's language. */
export function rotationProblem(err: unknown, locale: Locale, fallback: string): string {
  if (!(err instanceof ApiError)) return fallback;
  const codes = [
    ...(err.problem?.errors ?? []).map((e) => e.code),
    err.problem?.code,
  ].filter((c): c is string => !!c);
  const known = Array.from(new Set(codes.map((c) => ROTATION[c]?.[locale]).filter(Boolean)));
  if (known.length) return known.join(" ");
  const generic = problemMessage(err, locale);
  // problemMessage falls back to the server's English text for field codes it
  // doesn't know; an Arabic reader gets the page's own line instead.
  if (locale === "ar" && /[A-Za-z]{3,}/.test(generic)) return fallback;
  return err.status >= 500 ? fallback : generic;
}
