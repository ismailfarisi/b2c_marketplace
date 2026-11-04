import { ApiError } from "@marketplace/api-client";

import { problemFields, problemMessage } from "@/lib/problem";

type Locale = "en" | "ar";
type Text = { en: string; ar: string };

/** Office-lunch field codes, keyed "field:code" first, then by code alone. */
const FIELD: Record<string, Text> = {
  "trn:invalid_trn": { en: "A TRN is 15 digits.", ar: "الرقم الضريبي مكوّن من 15 رقمًا." },
  "billing_email:invalid": { en: "Enter a billing email.", ar: "أدخل بريد الفواتير." },
  "office.location:invalid_point": {
    en: "Pick the office location.",
    ar: "حدّد موقع المكتب.",
  },
  "phone:invalid_phone": {
    en: "Enter a mobile number like 050 123 4567 or +971 50 123 4567.",
    ar: "أدخل رقم جوال مثل 050 123 4567 أو ‎+971 50 123 4567.",
  },
  "weekdays:required": { en: "Pick at least one day.", ar: "اختر يومًا واحدًا على الأقل." },
  "meal_cap_minor:invalid": {
    en: "Enter a cap of at least AED 1.",
    ar: "أدخل حدًا لا يقل عن 1 درهم.",
  },
  "reference:required": {
    en: "Enter the bank transfer reference.",
    ar: "أدخل مرجع التحويل البنكي.",
  },
  "credit_limit_minor:required": {
    en: "Set a credit limit to approve.",
    ar: "حدّد حد الائتمان للموافقة.",
  },
  "schedule:not_covered": {
    en: "Your company doesn't cover those days or slots.",
    ar: "لا تغطي شركتك هذه الأيام أو الفترات.",
  },
  "schedule:office_only": {
    en: "Company lunches go to an office.",
    ar: "غداء الشركة يُوصَّل إلى المكتب.",
  },
  "office_id:not_found": { en: "Pick one of your company's offices.", ar: "اختر أحد مكاتب شركتك." },
};

const REASON: Record<string, Text> = {
  company_exists: {
    en: "You already have an account for this company.",
    ar: "لديك حساب لهذه الشركة بالفعل.",
  },
  not_company_admin: {
    en: "Only a lunch admin can do this.",
    ar: "يمكن لمسؤول الغداء فقط القيام بذلك.",
  },
  already_member: {
    en: "That number is already on the list.",
    ar: "هذا الرقم موجود في القائمة بالفعل.",
  },
  cannot_remove: {
    en: "Keep at least one lunch admin and one office.",
    ar: "يجب الإبقاء على مسؤول غداء واحد ومكتب واحد على الأقل.",
  },
  company_not_active: {
    en: "This company account isn't active.",
    ar: "حساب الشركة هذا غير مفعّل.",
  },
  not_member: {
    en: "You're not on this company's lunch list.",
    ar: "أنت لست ضمن قائمة غداء هذه الشركة.",
  },
  duplicate_lunch: {
    en: "You already have this office lunch starting on that day.",
    ar: "لديك بالفعل غداء المكتب هذا يبدأ في ذلك اليوم.",
  },
  over_credit_limit: {
    en: "Your company has reached its lunch credit limit. Ask your lunch admin.",
    ar: "بلغت شركتك حد ائتمان الغداء. تواصل مع مسؤول الغداء.",
  },
  nothing_to_bill: {
    en: "There is nothing to bill up to that date.",
    ar: "لا يوجد ما يُفوتر حتى ذلك التاريخ.",
  },
  currency_mismatch: {
    en: "This kitchen bills in another currency.",
    ar: "هذا المطبخ يفوتر بعملة أخرى.",
  },
};

/** Field key → message in the reader's language, for under each input. */
export function companyFields(err: unknown, locale: Locale): Record<string, string> {
  const out = problemFields(err, locale);
  if (!(err instanceof ApiError)) return out;
  for (const e of err.problem?.errors ?? []) {
    const own = FIELD[`${e.field}:${e.code}`];
    if (own) out[e.field] = own[locale];
  }
  return out;
}

/** One sentence for a failed office-lunch call: fields, then reason code. */
export function companyMessage(err: unknown, locale: Locale): string {
  if (err instanceof ApiError) {
    const fields = Object.values(companyFields(err, locale));
    if (fields.length) return Array.from(new Set(fields)).join(" ");
    const code = err.problem?.code;
    if (code && REASON[code]) return REASON[code][locale];
  }
  return problemMessage(err, locale);
}
