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

/** The number as it will be texted, for "Code sent to …". Display only: the
 *  server does the real normalisation and refuses what it cannot read. */
export function displayPhone(raw: string, code: string): string {
  const typed = raw.trim();
  if (typed.startsWith("+")) return typed;
  if (typed.startsWith("00")) return `+${typed.slice(2)}`;
  const c = phoneCountry(code);
  const digits = typed.replace(/\D/g, "");
  const national = c.trunk && digits.startsWith("0") ? digits.slice(1) : digits;
  return `+${c.dial} ${national}`;
}

/** The field code of a 422 for the phone or country, if that is what it was. */
export function phoneProblem(problem: { errors?: { field: string; code: string }[] } | null | undefined): string | null {
  const e = problem?.errors?.find((x) => x.field === "phone" || x.field === "country");
  return e?.code ?? null;
}
