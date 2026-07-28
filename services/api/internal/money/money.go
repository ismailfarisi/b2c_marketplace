// Package money handles integer minor units.
//
// There is no float anywhere in this package or downstream of it. Every amount
// crossing a boundary is an int64 plus a currency code.
package money

import (
	"fmt"
	"math"
)

type Amount struct {
	Minor    int64
	Currency string
}

func New(minor int64, currency string) Amount {
	return Amount{Minor: minor, Currency: currency}
}

func (a Amount) Add(b Amount) (Amount, error) {
	if a.Currency != b.Currency {
		return Amount{}, fmt.Errorf("money: cannot add %s to %s", b.Currency, a.Currency)
	}
	return Amount{Minor: a.Minor + b.Minor, Currency: a.Currency}, nil
}

func (a Amount) IsZero() bool { return a.Minor == 0 }

// ExtractTax returns the VAT contained in a tax-inclusive amount.
//
//	tax = total × rate / (1 + rate)
//
// GCC prices are displayed inclusive of VAT, so tax is a share OF the total,
// not an addition TO it. The intuitive `total × rate` overstates by a factor of
// (1 + rate) — 5% becomes 5.25% of net — and the error compounds through every
// downstream report and every filing.
func ExtractTax(total Amount, rateBasisPoints int) Amount {
	if rateBasisPoints <= 0 {
		return Amount{Minor: 0, Currency: total.Currency}
	}
	// Integer arithmetic throughout: total × rate / (10000 + rate), rounded
	// half up. Doing this in float64 and rounding afterwards produces
	// off-by-one fils on amounts that are exactly representable in integers.
	num := total.Minor * int64(rateBasisPoints)
	den := int64(10_000 + rateBasisPoints)
	return Amount{Minor: divRoundHalfUp(num, den), Currency: total.Currency}
}

// AddTax returns the VAT to add to a tax-exclusive amount. Present for
// completeness; GCC display pricing should use ExtractTax.
func AddTax(net Amount, rateBasisPoints int) Amount {
	if rateBasisPoints <= 0 {
		return Amount{Minor: 0, Currency: net.Currency}
	}
	return Amount{
		Minor:    divRoundHalfUp(net.Minor*int64(rateBasisPoints), 10_000),
		Currency: net.Currency,
	}
}

// Percentage applies a basis-point rate, rounding half up.
//
// Used for commission. Rounding matters more than it looks: at 15% on a 33.33
// AED order the difference between rounding modes is one fils, and one fils
// times a million orders is a reconciliation investigation.
func Percentage(a Amount, basisPoints int) Amount {
	return Amount{
		Minor:    divRoundHalfUp(a.Minor*int64(basisPoints), 10_000),
		Currency: a.Currency,
	}
}

func divRoundHalfUp(num, den int64) int64 {
	if den == 0 {
		return 0
	}
	if (num < 0) != (den < 0) {
		return -((-num + den/2) / den)
	}
	return (num + den/2) / den
}

// Exponent is the number of minor units per major unit.
func Exponent(currency string) int {
	switch currency {
	case "KWD", "BHD", "OMR":
		return 3
	default:
		return 2
	}
}

// Major renders the amount as a decimal string for display and logging only.
// Never parse this back — round-tripping through a decimal string is how
// precision gets lost.
func (a Amount) Major() string {
	exp := Exponent(a.Currency)
	div := int64(math.Pow10(exp))
	sign := ""
	v := a.Minor
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%0*d %s", sign, v/div, exp, v%div, a.Currency)
}
