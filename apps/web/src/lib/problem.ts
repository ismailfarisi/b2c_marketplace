import { ApiError } from "@marketplace/api-client";

type Locale = "en" | "ar";

/**
 * Field and reason codes the API emits, in both languages. The server's own
 * `message` is English-only and written for developers; these are for people.
 */
const FIELD: Record<string, { en: string; ar: string }> = {
  required: { en: "This is required.", ar: "هذا الحقل مطلوب." },
  invalid: { en: "This doesn't look right.", ar: "هذه القيمة غير صحيحة." },
  too_long: { en: "This is too long.", ar: "هذا النص طويل جدًا." },
  out_of_range: { en: "This is outside the allowed range.", ar: "هذه القيمة خارج النطاق المسموح." },
  before_start: { en: "This must be after the start date.", ar: "يجب أن يكون هذا بعد تاريخ البدء." },
  invalid_point: { en: "Pick a location on the map.", ar: "اختر موقعًا على الخريطة." },
  invalid_slot: { en: "That delivery slot isn't available.", ar: "فترة التوصيل هذه غير متاحة." },
  unknown_dish: { en: "That dish is no longer on the menu.", ar: "هذا الطبق لم يعد في القائمة." },
  invalid_status: { en: "This can't be changed right now.", ar: "لا يمكن تغيير هذا الآن." },
};

const REASON: Record<string, { en: string; ar: string }> = {
  otp_wrong_code: { en: "That code is not right.", ar: "هذا الرمز غير صحيح." },
  otp_too_many_attempts: {
    en: "Too many incorrect codes. Ask for a new one.",
    ar: "محاولات خاطئة كثيرة. اطلب رمزًا جديدًا.",
  },
  otp_expired: { en: "That code has expired. Ask for a new one.", ar: "انتهت صلاحية الرمز. اطلب رمزًا جديدًا." },
  cutoff_passed: { en: "It's past the cut-off for that day.", ar: "فات موعد الطلب لهذا اليوم." },
  already_cancelled: { en: "This was already cancelled.", ar: "تم الإلغاء مسبقًا." },
  already_renewed: { en: "This was already renewed.", ar: "تم التجديد مسبقًا." },
  not_renewable: { en: "This can't be renewed.", ar: "لا يمكن تجديد هذا." },
  not_payable: { en: "This can't be paid right now.", ar: "لا يمكن الدفع الآن." },
  over_meal_cap: { en: "That's over your meal allowance.", ar: "هذا يتجاوز حد الوجبات المسموح." },
  over_credit_limit: { en: "That's over the company's credit limit.", ar: "هذا يتجاوز حد الائتمان للشركة." },
  role_read_only: { en: "Your role can view this but not change it.", ar: "دورك يسمح بالعرض فقط دون التعديل." },
};

const FALLBACK = {
  en: { rejected: "That could not be done.", generic: "Something went wrong. Try again." },
  ar: { rejected: "تعذر تنفيذ ذلك.", generic: "حدث خطأ ما. حاول مرة أخرى." },
};

/** Field key → localized message, for rendering under each input. */
export function problemFields(err: unknown, locale: Locale): Record<string, string> {
  const out: Record<string, string> = {};
  if (!(err instanceof ApiError)) return out;
  for (const e of err.problem?.errors ?? []) {
    out[e.field] = FIELD[e.code]?.[locale] ?? e.message ?? FALLBACK[locale].rejected;
  }
  return out;
}

/**
 * One sentence for a failed mutation, in the reader's language. Field codes
 * first (joined), then the problem's reason code, then the server's detail for
 * English readers, then a generic line. Never a raw exception string.
 */
export function problemMessage(err: unknown, locale: Locale): string {
  if (err instanceof ApiError) {
    const fields = Object.values(problemFields(err, locale));
    if (fields.length) return Array.from(new Set(fields)).join(" ");
    const code = err.problem?.code;
    if (code && REASON[code]) return REASON[code][locale];
    if (locale === "en" && err.problem?.detail) return err.problem.detail;
    return err.status >= 500 ? FALLBACK[locale].generic : FALLBACK[locale].rejected;
  }
  return FALLBACK[locale].generic;
}
