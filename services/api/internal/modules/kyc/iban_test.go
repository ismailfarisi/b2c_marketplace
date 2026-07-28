package kyc

import (
	"strings"
	"testing"
)

func TestValidIBANsPass(t *testing.T) {
	valid := []string{
		"AE07 0331 2345 6789 0123 456", // UAE, as printed on a statement
		"AE070331234567890123456",      // and compacted
		"SA0380000000608010167519",     // Saudi Arabia
		"GB82 WEST 1234 5698 7654 32",  // the ISO reference example
		"DE89370400440532013000",
		"ae070331234567890123456", // lower case
	}
	for _, s := range valid {
		if err := ValidateIBAN(s); err != nil {
			t.Errorf("%q rejected: %v", s, err)
		}
	}
}

// The class of error this exists to catch: a person typing their own bank
// details gets one character wrong. A length check alone passes all of these.
func TestASingleWrongCharacterIsCaught(t *testing.T) {
	base := "AE070331234567890123456"
	for i := 4; i < len(base); i++ {
		for _, sub := range "0123456789" {
			if rune(base[i]) == sub {
				continue
			}
			mutated := base[:i] + string(sub) + base[i+1:]
			if err := ValidateIBAN(mutated); err == nil {
				t.Fatalf("%q passed — a single-digit typo reached a bank transfer", mutated)
			}
		}
	}
}

// Transpositions are the other common typo and the check digits catch almost
// all of them. "Almost" is honest: MOD-97 misses a vanishingly small fraction,
// and the test asserts the practical claim rather than a false absolute.
func TestAdjacentTranspositionsAreCaught(t *testing.T) {
	base := "AE070331234567890123456"
	caught, total := 0, 0
	for i := 4; i < len(base)-1; i++ {
		if base[i] == base[i+1] {
			continue
		}
		total++
		mutated := base[:i] + string(base[i+1]) + string(base[i]) + base[i+2:]
		if err := ValidateIBAN(mutated); err != nil {
			caught++
		}
	}
	if caught < total {
		t.Logf("caught %d of %d transpositions", caught, total)
	}
	if total > 0 && caught*100/total < 95 {
		t.Fatalf("only %d of %d transpositions caught", caught, total)
	}
}

func TestWrongLengthForAKnownCountryIsRejected(t *testing.T) {
	// A valid-shaped IBAN that is one character short for the UAE.
	if err := ValidateIBAN("AE07033123456789012345"); err == nil {
		t.Fatal("a 22-character UAE IBAN must be rejected — they are 23")
	}
	err := ValidateIBAN("AE0703312345678901234567")
	if err == nil || !strings.Contains(err.Error(), "23") {
		t.Fatalf("expected a length message naming 23, got %v", err)
	}
}

func TestMalformedInputIsRejectedWithoutPanicking(t *testing.T) {
	for _, bad := range []string{
		"", "A", "AE", "AE07", "1234567890123456", // no country code
		"AEXX0331234567890123456", // check digits are not digits
		"!!!!!!!!!!!!!!!!!!!!!!!",
		strings.Repeat("A", 40), // too long
	} {
		if err := ValidateIBAN(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestCountriesWithoutIBANsAreNamedRatherThanSilentlyFailing(t *testing.T) {
	err := ValidateIBAN("IN070331234567890123456")
	if err == nil {
		t.Fatal("India does not use IBANs")
	}
	if !strings.Contains(err.Error(), "IBAN") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

func TestNormalisationMakesTwoSpellingsOneRecord(t *testing.T) {
	a := NormaliseIBAN("AE07 0331 2345 6789 0123 456")
	b := NormaliseIBAN("ae070331234567890123456")
	if a != b {
		t.Fatalf("%q != %q — the same account would be stored twice", a, b)
	}
	if a != "AE070331234567890123456" {
		t.Fatalf("got %q", a)
	}
}

func TestMaskingLeavesEnoughToIdentifyAndNotEnoughToUse(t *testing.T) {
	got := MaskIBAN("AE070331234567890123456")
	if got != "AE07***************3456" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "0331234567890") {
		t.Fatal("the account body must not survive masking")
	}
	// A short or malformed value must not slip through unmasked.
	if got := MaskIBAN("AE07"); strings.Contains(got, "AE07") {
		t.Fatalf("got %q — too short to mask partially, so mask it entirely", got)
	}
}

// ── tiers ────────────────────────────────────────────────────────────────────

func TestVendorsNeedMoreThanIndividuals(t *testing.T) {
	if RequiredTier("vendor") != Enhanced {
		t.Error("a company receiving payouts needs a trade licence and a signatory")
	}
	if RequiredTier("driver") != Verified {
		t.Error("a driver needs government ID")
	}
	// An unrecognised subject type must fail toward more scrutiny, not less.
	if RequiredTier("something_new") != Enhanced {
		t.Error("an unknown subject type must default to the strictest tier")
	}
}

func TestTierRanking(t *testing.T) {
	if rank(Enhanced) <= rank(Verified) || rank(Verified) <= rank(Basic) {
		t.Fatal("tiers must be totally ordered — the eligibility check compares them")
	}
}
