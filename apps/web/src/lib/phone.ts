/**
 * Sign-in countries: the six GCC states, UAE first because it is the default.
 *
 * A national number means nothing without its country — 0551230003 is a valid
 * Emirati and a valid Saudi number — so every sign-in form sends the picked
 * country with the number and the server reads the national form in it. Keep
 * this list in step with the API (`identity/phone.go`) and KMM
 * (`identity/PhoneCountry.kt`).
 */
export type PhoneCountryCode = "AE" | "SA" | "QA" | "KW" | "BH" | "OM";

export interface PhoneCountry {
  code: PhoneCountryCode;
  dial: string;
  /** Digits after the country code. */
  nsn: number;
  /** Whether numbers are written with a leading 0 at home (050…, 055…). */
  trunk: boolean;
  /** How the number is written locally, for the placeholder. */
  example: string;
  name: { en: string; ar: string };
}

export const PHONE_COUNTRIES: readonly PhoneCountry[] = [
  { code: "AE", dial: "971", nsn: 9, trunk: true, example: "050 123 4567", name: { en: "UAE", ar: "الإمارات" } },
  { code: "SA", dial: "966", nsn: 9, trunk: true, example: "055 123 4567", name: { en: "Saudi Arabia", ar: "السعودية" } },
  { code: "QA", dial: "974", nsn: 8, trunk: false, example: "3312 3456", name: { en: "Qatar", ar: "قطر" } },
  { code: "KW", dial: "965", nsn: 8, trunk: false, example: "5012 3456", name: { en: "Kuwait", ar: "الكويت" } },
  { code: "BH", dial: "973", nsn: 8, trunk: false, example: "3600 1234", name: { en: "Bahrain", ar: "البحرين" } },
  { code: "OM", dial: "968", nsn: 8, trunk: false, example: "9212 3456", name: { en: "Oman", ar: "عُمان" } },
];

export const DEFAULT_PHONE_COUNTRY: PhoneCountryCode = "AE";

export function phoneCountry(code: string): PhoneCountry {
  return PHONE_COUNTRIES.find((c) => c.code === code) ?? (PHONE_COUNTRIES[0] as PhoneCountry);
}

/**
 * What the server's `NormalizePhone` (services/api identity/phone.go) makes of
 * a typed number: E.164, or null where the server would refuse it. Mirrors it
 * step for step so "Code sent to …" names the number that was actually texted.
 */
export function normalizePhone(raw: string, code: string): string | null {
  let out = "";
  for (const ch of raw.trim()) {
    if (ch === "+" && out.length === 0) out += ch;
    else if (ch >= "0" && ch <= "9") out += ch;
  }
  if (out.startsWith("+")) {
    // already international
  } else if (out.startsWith("00")) {
    out = `+${out.slice(2)}`;
  } else {
    const c = PHONE_COUNTRIES.find((x) => x.code === code);
    if (!c) return null;
    let digits = out;
    if (digits.startsWith(c.dial) && digits.length === c.dial.length + c.nsn) {
      out = `+${digits}`;
    } else {
      if (c.trunk && digits.startsWith("0")) digits = digits.slice(1);
      if (digits.length !== c.nsn || digits.startsWith("0")) return null;
      out = `+${c.dial}${digits}`;
    }
  }
  const c = PHONE_COUNTRIES.find((x) => out.startsWith(`+${x.dial}`));
  if (c) {
    let nsn = out.slice(1 + c.dial.length);
    if (c.trunk && nsn.length === c.nsn + 1 && nsn.startsWith("0")) nsn = nsn.slice(1);
    if (nsn.length !== c.nsn || nsn.startsWith("0")) return null;
    out = `+${c.dial}${nsn}`;
  }
  return /^\+[1-9]\d{7,14}$/.test(out) ? out : null;
}

/** The number as it will be texted, for "Code sent to …": the server's E.164
 *  with a space after a GCC dial code. Falls back to the typed text when the
 *  server would refuse it anyway. */
export function displayPhone(raw: string, code: string): string {
  const e164 = normalizePhone(raw, code);
  if (!e164) return raw.trim();
  const c = PHONE_COUNTRIES.find((x) => e164.startsWith(`+${x.dial}`));
  return c ? `+${c.dial} ${e164.slice(1 + c.dial.length)}` : e164;
}

/** Wraps a phone number in a left-to-right isolate (U+2066…U+2069) so "+971 50…"
 *  keeps its order inside Arabic text. */
export function ltr(text: string): string {
  return `\u2066${text}\u2069`;
}

/** The field code of a 422 for the phone or country, if that is what it was. */
export function phoneProblem(problem: { errors?: { field: string; code: string }[] } | null | undefined): string | null {
  const e = problem?.errors?.find((x) => x.field === "phone" || x.field === "country");
  return e?.code ?? null;
}
