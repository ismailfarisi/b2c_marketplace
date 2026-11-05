"use client";

import { PHONE_COUNTRIES, phoneCountry } from "@/lib/phone";

/**
 * Country code picker plus number, for every sign-in form. The pair stays
 * left-to-right in Arabic too: "+971 050…" reversed is a different number.
 */
export function PhoneInput({
  country,
  onCountry,
  phone,
  onPhone,
  locale,
  countryLabel,
  inputClassName,
  selectClassName,
  className,
}: {
  country: string;
  onCountry: (code: string) => void;
  phone: string;
  onPhone: (value: string) => void;
  locale: "en" | "ar";
  countryLabel: string;
  inputClassName?: string;
  selectClassName?: string;
  className?: string;
}) {
  return (
    <div className={className ?? "phone-input"} dir="ltr">
      <select
        className={selectClassName}
        value={country}
        onChange={(e) => onCountry(e.target.value)}
        aria-label={countryLabel}
      >
        {PHONE_COUNTRIES.map((c) => (
          <option key={c.code} value={c.code}>
            {`+${c.dial} ${c.name[locale]}`}
          </option>
        ))}
      </select>
      <input
        className={inputClassName}
        value={phone}
        onChange={(e) => onPhone(e.target.value)}
        type="tel"
        inputMode="tel"
        autoComplete="tel-national"
        placeholder={phoneCountry(country).example}
        dir="ltr"
        autoFocus
        required
      />
    </div>
  );
}
