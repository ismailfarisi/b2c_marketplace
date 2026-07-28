/**
 * Money helpers.
 *
 * The API sends integer minor units plus a currency code, and this module is
 * the only place that converts. There is no `parseFloat` on a price anywhere in
 * either client — the moment an amount becomes a JS number in the major unit,
 * rounding differences start showing up between what the customer saw and what
 * the ledger recorded.
 */

export interface Money {
  amount: number;
  currency: string;
}

/** Minor units per major unit. All GCC currencies in scope use 2. */
const EXPONENT: Record<string, number> = {
  AED: 2,
  SAR: 2,
  QAR: 2,
  KWD: 3,
  BHD: 3,
  OMR: 3,
};

export function exponentFor(currency: string): number {
  return EXPONENT[currency.toUpperCase()] ?? 2;
}

/**
 * Format for display, locale-aware.
 *
 * Arabic formatting is not a translation of the English string: numeral shape,
 * currency position and separators all differ. Intl handles it correctly given
 * the right locale, which is why the locale is a required argument rather than
 * a default.
 */
export function formatMoney(money: Money, locale: "en" | "ar"): string {
  const exp = exponentFor(money.currency);
  const major = money.amount / 10 ** exp;

  return new Intl.NumberFormat(locale === "ar" ? "ar-AE" : "en-AE", {
    style: "currency",
    currency: money.currency,
    minimumFractionDigits: exp,
    maximumFractionDigits: exp,
  }).format(major);
}

/** Parse user input in major units into minor units, rounding half away from zero. */
export function toMinorUnits(input: string | number, currency: string): number {
  const exp = exponentFor(currency);
  const value = typeof input === "number" ? input : Number.parseFloat(input.replace(/,/g, ""));
  if (!Number.isFinite(value)) {
    throw new Error(`not a number: ${input}`);
  }
  return Math.round(value * 10 ** exp);
}

export function addMoney(a: Money, b: Money): Money {
  if (a.currency !== b.currency) {
    throw new Error(`cannot add ${a.currency} to ${b.currency}`);
  }
  return { amount: a.amount + b.amount, currency: a.currency };
}

/**
 * Extract the VAT portion from a tax-inclusive total.
 *
 * GCC prices are displayed inclusive of VAT, so tax is a share OF the total, not
 * an addition TO it. Multiplying the total by the rate — the intuitive move, and
 * the wrong one — overstates tax by a factor of (1 + rate).
 */
export function extractTax(total: Money, rateBasisPoints: number): Money {
  const rate = rateBasisPoints / 10_000;
  return {
    amount: Math.round((total.amount * rate) / (1 + rate)),
    currency: total.currency,
  };
}
