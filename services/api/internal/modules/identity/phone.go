package identity

import (
	"strings"

	"github.com/marketplace/api/internal/httpx"
)

// PhoneCountry is a sign-in country: the six GCC states the apps offer in the
// country picker.
//
// National numbers are only meaningful together with a country. The old rule
// read every ten-digit 0-prefixed number as Emirati, so a Saudi customer typing
// 0551230003 was texted at +971551230003 — someone else's phone. The country is
// now an explicit field and a national number without one is refused rather
// than guessed at.
type PhoneCountry struct {
	ISO  string
	Dial string
	// NSN is the length of the national significant number (digits after the
	// country code, without the trunk 0).
	NSN int
	// Trunk is whether the national form starts with a 0 that E.164 drops.
	// Only the UAE and Saudi Arabia write numbers that way; the others do not.
	Trunk bool
}

// PhoneCountries mirrors the clients' pickers (web lib/phone.ts, KMM
// identity/PhoneNumber.kt). UAE first: it is the default.
var PhoneCountries = []PhoneCountry{
	{ISO: "AE", Dial: "971", NSN: 9, Trunk: true},
	{ISO: "SA", Dial: "966", NSN: 9, Trunk: true},
	{ISO: "QA", Dial: "974", NSN: 8},
	{ISO: "KW", Dial: "965", NSN: 8},
	{ISO: "BH", Dial: "973", NSN: 8},
	{ISO: "OM", Dial: "968", NSN: 8},
}

func countryByISO(iso string) (PhoneCountry, bool) {
	iso = strings.ToUpper(strings.TrimSpace(iso))
	for _, c := range PhoneCountries {
		if c.ISO == iso {
			return c, true
		}
	}
	return PhoneCountry{}, false
}

func countryByE164(e string) (PhoneCountry, bool) {
	for _, c := range PhoneCountries {
		if strings.HasPrefix(e, "+"+c.Dial) {
			return c, true
		}
	}
	return PhoneCountry{}, false
}

// Field codes for a refused phone number. Wire contract, like the OTP codes.
const (
	PhoneInvalid         = "invalid"
	PhoneCountryRequired = "country_required"
	PhoneWrongLength     = "wrong_length"
	PhoneCountryInvalid  = "unsupported_country"
)

// NormalizePhone turns what someone typed into E.164, or says why it cannot.
//
// Accepted: E.164 (+9665…), the 00 international prefix (009665…), and — only
// when country is set — the national form (0551234567 for SA, 33123456 for QA)
// or the number with the country code but no plus. Numbers under a GCC code are
// held to that country's length, which also catches a trunk 0 kept after the
// code (+971 050…), the commonest typo, by stripping it first.
//
// The returned string is the field error code when err is non-nil.
func NormalizePhone(raw, country string) (string, string) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		if r == '+' && b.Len() == 0 {
			b.WriteRune(r)
		} else if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	out := b.String()

	var picked *PhoneCountry
	if country != "" {
		c, ok := countryByISO(country)
		if !ok {
			return "", PhoneCountryInvalid
		}
		picked = &c
	}

	switch {
	case strings.HasPrefix(out, "+"):
	case strings.HasPrefix(out, "00"):
		out = "+" + out[2:]
	case picked == nil:
		// A national number with no country: 0551230003 is a valid Saudi and
		// a valid Emirati number. Refuse rather than text a stranger.
		return "", PhoneCountryRequired
	default:
		c := *picked
		digits := out
		if strings.HasPrefix(digits, c.Dial) && len(digits) == len(c.Dial)+c.NSN {
			out = "+" + digits
			break
		}
		if c.Trunk && strings.HasPrefix(digits, "0") {
			digits = digits[1:]
		}
		if len(digits) != c.NSN || digits[0] == '0' {
			return "", PhoneWrongLength
		}
		out = "+" + c.Dial + digits
	}

	if c, ok := countryByE164(out); ok {
		nsn := out[1+len(c.Dial):]
		if c.Trunk && len(nsn) == c.NSN+1 && nsn[0] == '0' {
			nsn = nsn[1:]
			out = "+" + c.Dial + nsn
		}
		if len(nsn) != c.NSN || nsn[0] == '0' {
			return "", PhoneWrongLength
		}
	}
	if !e164.MatchString(out) {
		return "", PhoneInvalid
	}
	return out, ""
}

// phoneError is the 422 for a refused number, worded in the caller's language.
// The apps word it from the code; the sentence is for the web consoles and for
// anyone reading the API directly.
func phoneError(locale, code string) error {
	field := "phone"
	msg := map[string][2]string{
		PhoneInvalid: {"Enter a mobile number with its country code, e.g. +971501234567.",
			"أدخل رقم الجوال مع رمز الدولة، مثل ‎+971501234567."},
		PhoneCountryRequired: {"Choose the country for this number, or enter it with + and the country code.",
			"اختر دولة هذا الرقم، أو أدخله مع + ورمز الدولة."},
		PhoneWrongLength: {"That number has the wrong number of digits for its country.",
			"عدد أرقام هذا الرقم غير صحيح لدولته."},
		PhoneCountryInvalid: {"Sign-in supports UAE, Saudi Arabia, Qatar, Kuwait, Bahrain and Oman numbers.",
			"يدعم تسجيل الدخول أرقام الإمارات والسعودية وقطر والكويت والبحرين وعُمان."},
	}[code]
	if code == PhoneCountryInvalid {
		field = "country"
	}
	text := msg[0]
	if locale == "ar" {
		text = msg[1]
	}
	return &httpx.ValidationError{Fields: []httpx.FieldError{{Field: field, Code: code, Message: text}}}
}
