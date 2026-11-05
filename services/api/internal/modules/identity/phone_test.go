package identity

import (
	"errors"
	"testing"

	"github.com/marketplace/api/internal/httpx"
)

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		raw, country, want, bad string
	}{
		{"+971501234567", "", "+971501234567", ""},
		{"+971 50 123 4567", "", "+971501234567", ""},
		{"00971501234567", "", "+971501234567", ""},
		{"00447700900123", "", "+447700900123", ""},
		// E.164 needs no country, and a picker left on UAE does not rewrite
		// someone else's E.164.
		{"+966551230003", "AE", "+966551230003", ""},
		// National numbers follow the picker.
		{"0501234567", "AE", "+971501234567", ""},
		{"050-123-4567", "ae", "+971501234567", ""},
		{"501234567", "AE", "+971501234567", ""},
		{"971501234567", "AE", "+971501234567", ""},
		{"0551230003", "SA", "+966551230003", ""},
		{"551230003", "SA", "+966551230003", ""},
		{"33123456", "QA", "+97433123456", ""},
		{"50123456", "KW", "+96550123456", ""},
		{"36001234", "BH", "+97336001234", ""},
		{"92123456", "OM", "+96892123456", ""},
		// The trunk 0 kept after the country code.
		{"+971 050 123 4567", "", "+971501234567", ""},
		// Ambiguous without a country: valid in both the UAE and Saudi Arabia.
		{"0551230003", "", "", PhoneCountryRequired},
		{"501234567", "", "", PhoneCountryRequired},
		// Wrong length for the chosen country.
		{"050123456", "AE", "", PhoneWrongLength},
		{"0331234567", "QA", "", PhoneWrongLength},
		{"+9715012345", "", "", PhoneWrongLength},
		{"+9665512300031", "", "", PhoneWrongLength},
		{"+97433123", "", "", PhoneWrongLength},
		{"0001234567", "AE", "", PhoneInvalid},
		{"0501234567", "US", "", PhoneCountryInvalid},
		{"+12", "", "", PhoneInvalid},
	}
	for _, tc := range cases {
		got, bad := NormalizePhone(tc.raw, tc.country)
		if got != tc.want || bad != tc.bad {
			t.Errorf("NormalizePhone(%q, %q) = %q, %q; want %q, %q",
				tc.raw, tc.country, got, bad, tc.want, tc.bad)
		}
	}
}

func TestPhoneErrorIsLocalized(t *testing.T) {
	msg := func(loc, code string) httpx.FieldError {
		var ve *httpx.ValidationError
		if !errors.As(phoneError(loc, code), &ve) || len(ve.Fields) != 1 {
			t.Fatalf("phoneError(%s, %s) is not one field error", loc, code)
		}
		return ve.Fields[0]
	}
	en, ar := msg("en", PhoneCountryRequired), msg("ar", PhoneCountryRequired)
	if en.Field != "phone" || en.Code != PhoneCountryRequired || en.Message == "" {
		t.Fatalf("en = %+v", en)
	}
	if ar.Message == en.Message || ar.Code != en.Code {
		t.Fatalf("arabic should differ in words only: %+v vs %+v", ar, en)
	}
	if f := msg("en", PhoneCountryInvalid); f.Field != "country" {
		t.Fatalf("unsupported country should name the country field, got %q", f.Field)
	}
}
