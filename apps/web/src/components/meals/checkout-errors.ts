import { ApiError } from "@marketplace/api-client";

import { loadErrorKind } from "@/components/load-error";
import { problemMessage } from "@/lib/problem";

type Locale = "en" | "ar";
type Pair = { en: string; ar: string };

/** Promo refusals, keyed by the API's promo_code field codes. */
const PROMO: Record<string, Pair> = {
  unknown: {
    en: "This promo code does not exist.",
    ar: "رمز الخصم هذا غير موجود.",
  },
  not_live: {
    en: "This promo has ended or has not started yet.",
    ar: "انتهى هذا العرض أو لم يبدأ بعد.",
  },
  wrong_kitchen: {
    en: "This promo does not cover this kitchen or plan.",
    ar: "هذا العرض لا يشمل هذا المطبخ أو الباقة.",
  },
  wrong_product: {
    en: "This promo does not cover this kitchen or plan.",
    ar: "هذا العرض لا يشمل هذا المطبخ أو الباقة.",
  },
  new_customers_only: {
    en: "This promo is for first-time subscribers.",
    ar: "هذا العرض للمشتركين الجدد فقط.",
  },
  already_used: {
    en: "You have already used this promo.",
    ar: "لقد استخدمت هذا العرض من قبل.",
  },
  exhausted: {
    en: "This promo has been fully used.",
    ar: "نفدت استخدامات هذا العرض.",
  },
  trial_too_small: {
    en: "This trial does not fit this plan.",
    ar: "هذه التجربة لا تناسب هذه الباقة.",
  },
};
const PROMO_OTHER: Pair = {
  en: "This promo code cannot be used here.",
  ar: "لا يمكن استخدام رمز الخصم هذا هنا.",
};

const START: Record<string, Pair> = {
  in_the_past: {
    en: "The start date can't be in the past.",
    ar: "لا يمكن أن يكون تاريخ البدء في الماضي.",
  },
  too_soon: {
    en: "The kitchen's cutoff for that day has passed. Pick a later start date.",
    ar: "فات موعد الطلب لدى المطبخ لهذا اليوم. اختر تاريخ بدء لاحقًا.",
  },
  no_deliveries: {
    en: "No delivery falls in that period. Pick another start date or days.",
    ar: "لا يوجد توصيل في هذه الفترة. اختر تاريخ بدء أو أيامًا أخرى.",
  },
};

const ADDRESS: Pair = {
  en: "This kitchen does not deliver to that address. Choose another one.",
  ar: "هذا المطبخ لا يوصل إلى هذا العنوان. اختر عنوانًا آخر.",
};

/** Office-lunch refusals (problem.code). */
const LUNCH: Record<string, Pair> = {
  over_meal_cap: {
    en: "This plan costs more per meal than your company covers.",
    ar: "تكلفة الوجبة في هذه الخطة أعلى مما تغطيه شركتك.",
  },
  over_credit_limit: {
    en: "Your company has reached its lunch credit limit. Contact your lunch admin.",
    ar: "بلغت شركتك حد رصيد الغداء. تواصل مع مسؤول الغداء.",
  },
  company_not_active: {
    en: "Your company's lunch account is not active.",
    ar: "حساب غداء شركتك غير مفعّل حاليًا.",
  },
  not_member: {
    en: "You are no longer listed as an employee of this company.",
    ar: "لم تعد ضمن موظفي هذه الشركة.",
  },
  duplicate_lunch: {
    en: "You already have this office lunch starting on that day.",
    ar: "لديك بالفعل غداء المكتب هذا يبدأ في ذلك اليوم.",
  },
};

const OFFLINE: Pair = {
  en: "You're offline. Check your connection and try again. Nothing was charged twice.",
  ar: "أنت غير متصل بالإنترنت. تحقق من الاتصال وحاول مرة أخرى. لن يتم الخصم مرتين.",
};

/** True when the refusal is about the promo code, so it shows under that field. */
export const isPromoProblem = (err: unknown) =>
  err instanceof ApiError && err.fieldErrors["promo_code"] != null;

/**
 * Checkout's failures in the reader's language: promo, start date, address and
 * office-lunch codes first, then an offline hint, then the shared mapping.
 */
export function checkoutProblem(err: unknown, locale: Locale): string {
  if (err instanceof ApiError) {
    const promo = err.fieldErrors["promo_code"];
    const promoCode = err.problem?.errors?.find(
      (e) => e.field === "promo_code",
    )?.code;
    if (promo != null) return (PROMO[promoCode ?? ""] ?? PROMO_OTHER)[locale];
    const startCode = err.problem?.errors?.find(
      (e) => e.field === "start_date",
    )?.code;
    if (startCode && START[startCode]) return START[startCode][locale];
    if (err.fieldErrors["address_id"] != null) return ADDRESS[locale];
    const code = err.problem?.code;
    if (code && LUNCH[code]) return LUNCH[code][locale];
  } else if (loadErrorKind(err) === "offline") {
    return OFFLINE[locale];
  }
  return problemMessage(err, locale);
}
