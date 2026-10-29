import { formatMoney } from "@marketplace/api-client";

import type { Offer } from "@/lib/meals-api";

type Locale = "en" | "ar";

const pct = (bp: number) =>
  `${(bp / 100).toLocaleString("en", { maximumFractionDigits: 2 })}%`;

/** A one-line description of what an offer gives, e.g. "3-day trial · 20% off". */
export function offerLabel(o: Offer, locale: Locale): string {
  const money = (n: number) =>
    formatMoney({ amount: n, currency: o.currency }, locale);
  const parts: string[] = [];
  if (o.trial_days) {
    parts.push(
      locale === "ar"
        ? `تجربة ${o.trial_days} أيام`
        : `${o.trial_days}-day trial`,
    );
  }
  if (o.discount_type === "percent") {
    const off =
      locale === "ar" ? `خصم ${pct(o.percent_bp)}` : `${pct(o.percent_bp)} off`;
    parts.push(
      o.max_discount_minor
        ? `${off} ${locale === "ar" ? "حتى" : "up to"} ${money(o.max_discount_minor)}`
        : off,
    );
  } else if (o.discount_type === "fixed") {
    parts.push(
      locale === "ar"
        ? `خصم ${money(o.amount_minor)}`
        : `${money(o.amount_minor)} off`,
    );
  }
  return parts.join(" · ");
}

export const offerName = (o: Offer, locale: Locale) =>
  (locale === "ar" && o.name_ar) || o.name;

export const OFFER_COPY = {
  en: {
    newOnly: "First-time subscribers",
    trialNote: (d: number) =>
      `A ${d}-day trial. It does not renew; continue with a full plan after.`,
  },
  ar: {
    newOnly: "للمشتركين الجدد",
    trialNote: (d: number) =>
      `تجربة لمدة ${d} أيام لا تتجدد، ويمكنك الاشتراك في خطة كاملة بعدها.`,
  },
} as const;
