package kyc

import (
	"fmt"
	"strings"
)

// IBAN validation, ISO 13616 / ISO 7064 MOD-97-10.
//
// Worth doing properly rather than checking the length, because of what the
// failure looks like. A payout sent to a mistyped IBAN does not bounce
// immediately: it is accepted by the sending bank, rejected somewhere in the
// correspondent chain days later, and returned minus fees — or, when the typo
// happens to produce another valid account, not returned at all.
//
// The check digits catch every single-character error and almost every
// transposition, which is exactly the class of mistake a person typing their
// own bank details makes.

// ibanLengths is the expected length per country.
//
// Only the countries this platform pays into. A country absent from the map is
// accepted on shape and check digits alone rather than rejected, because a
// vendor with a legitimate account somewhere unlisted should not be blocked by
// a table this file forgot to update — but the length check is the stronger
// signal where it exists, so it is applied where it is known.
var ibanLengths = map[string]int{
	"AE": 23, // United Arab Emirates
	"SA": 24, // Saudi Arabia
	"BH": 22, // Bahrain
	"KW": 30, // Kuwait
	"OM": 23, // Oman
	"QA": 29, // Qatar
	"JO": 30, // Jordan
	"EG": 29, // Egypt
	"GB": 22,
	"DE": 22,
	"FR": 27,
	"IN": 0, // India has no IBAN; listed so the error names the reason
}

// NormaliseIBAN strips formatting and upper-cases.
//
// People write IBANs in groups of four with spaces, because that is how a bank
// statement prints them. Rejecting that is user-hostile; storing it is worse,
// because two records for the same account then differ by whitespace.
func NormaliseIBAN(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidateIBAN checks structure and check digits.
func ValidateIBAN(raw string) error {
	iban := NormaliseIBAN(raw)

	if len(iban) < 15 || len(iban) > 34 {
		return fmt.Errorf("iban: %d characters is outside the 15–34 range", len(iban))
	}
	country := iban[:2]
	for _, r := range country {
		if r < 'A' || r > 'Z' {
			return fmt.Errorf("iban: the first two characters must be a country code")
		}
	}
	if iban[2] < '0' || iban[2] > '9' || iban[3] < '0' || iban[3] > '9' {
		return fmt.Errorf("iban: characters 3 and 4 must be the check digits")
	}

	if want, ok := ibanLengths[country]; ok {
		if want == 0 {
			return fmt.Errorf("iban: %s does not use IBANs", country)
		}
		if len(iban) != want {
			return fmt.Errorf("iban: %s accounts are %d characters, this one is %d",
				country, want, len(iban))
		}
	}

	if !mod97(iban) {
		return fmt.Errorf("iban: the check digits do not match — this is almost always a typo")
	}
	return nil
}

// mod97 implements ISO 7064 MOD-97-10.
//
// The four leading characters move to the end, letters become their position in
// the alphabet plus nine, and the resulting (very large) decimal number must
// leave a remainder of 1 when divided by 97. Computed digit by digit because
// the number is far wider than any integer type.
func mod97(iban string) bool {
	rearranged := iban[4:] + iban[:4]

	rem := 0
	for _, r := range rearranged {
		switch {
		case r >= '0' && r <= '9':
			rem = rem*10 + int(r-'0')
		case r >= 'A' && r <= 'Z':
			// Two digits, so the remainder must be folded twice.
			v := int(r-'A') + 10
			rem = rem*10 + v/10
			rem %= 97
			rem = rem*10 + v%10
		default:
			return false
		}
		rem %= 97
	}
	return rem == 1
}

// MaskIBAN renders an account for display.
//
// Country, check digits, and the last four. Support agents need to confirm
// which account a vendor means; nobody needs the whole number on a screen that
// a shoulder can see.
func MaskIBAN(iban string) string {
	iban = NormaliseIBAN(iban)
	if len(iban) < 8 {
		return strings.Repeat("*", len(iban))
	}
	return iban[:4] + strings.Repeat("*", len(iban)-8) + iban[len(iban)-4:]
}
