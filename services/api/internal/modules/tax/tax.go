// Package tax computes VAT.
//
// Two things about this package are unusual and both come from PLAN.md §7.
//
// **Prices are tax-inclusive.** The plan is explicit: "Consumer prices must be
// displayed tax-inclusive. Bake into the pricing engine, don't add at
// checkout." So the primitive here is not `net + tax = gross`; it is *given a
// gross, what was the net and what was the tax*. Building it the other way
// round is the mistake that produces a checkout page where the total changes
// after the customer has decided, which is prohibited rather than merely
// unpopular.
//
// **Rates are chosen by date, not by "now".** An invoice reissued next year
// must print the rate that applied on the date of supply. A system that stores
// only the current rate rewrites its own history the moment a rate moves, and
// KSA moved from 5% to 15% in July 2020 with three weeks' notice.
package tax

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Category is the VAT treatment of a line.
//
// Not every line in a marketplace order is standard-rated, which is why this is
// per-line rather than per-order. The residential rent portion of a serviced
// apartment is exempt; an export is zero-rated; a platform commission charged
// to a vendor in another country may be out of scope entirely.
type Category string

const (
	Standard   Category = "standard"
	ZeroRated  Category = "zero_rated"
	Exempt     Category = "exempt"
	OutOfScope Category = "out_of_scope"
)

// Rate is a rate as it applied on a date.
type Rate struct {
	Country  string
	Region   string
	Category Category
	RateBP   int
	Label    string
	From     time.Time
	To       time.Time // zero when open-ended
}

// ErrNoRate means no rate is on file for that jurisdiction on that date.
//
// A distinct error rather than a zero rate, and the distinction is the point:
// "we do not know the rate" and "the rate is zero" produce the same invoice and
// mean entirely different things, one of which is a bug that under-collects tax
// silently.
var ErrNoRate = errors.New("tax: no rate on file")

type Service struct {
	pool *pgxpool.Pool

	mu    sync.RWMutex
	cache map[string]Rate
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, cache: map[string]Rate{}}
}

// RateOn returns the rate for a jurisdiction and category on a date.
//
// Cached by (country, region, category, date). Rates are immutable per date
// once issued — that is the entire reason the table is date-ranged — so a cache
// with no invalidation is correct here rather than merely convenient. A new
// rate row covers future dates, which have their own cache keys.
func (s *Service) RateOn(ctx context.Context, country, region string, cat Category, on time.Time) (Rate, error) {
	key := fmt.Sprintf("%s|%s|%s|%s", country, region, cat, on.Format("2006-01-02"))

	s.mu.RLock()
	r, ok := s.cache[key]
	s.mu.RUnlock()
	if ok {
		return r, nil
	}

	var (
		found Rate
		to    *time.Time
		from  time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT country, coalesce(region, ''), category, rate_bp, label,
		       lower(valid), upper(valid)
		  FROM tax_rates
		 WHERE country = $1 AND coalesce(region, '') = $2 AND category = $3
		   AND valid @> $4::date
		 LIMIT 1`, country, region, string(cat), on).
		Scan(&found.Country, &found.Region, &found.Category, &found.RateBP, &found.Label, &from, &to)

	if errors.Is(err, pgx.ErrNoRows) && region != "" {
		// Fall back to the country-wide rate. A region row exists only where a
		// jurisdiction diverges, which is nowhere in the GCC today, and
		// requiring one per emirate would mean seven rows that must be kept
		// identical.
		return s.RateOn(ctx, country, "", cat, on)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Rate{}, fmt.Errorf("%w: %s/%s on %s", ErrNoRate, country, cat, on.Format("2006-01-02"))
	}
	if err != nil {
		return Rate{}, fmt.Errorf("tax: load rate: %w", err)
	}

	found.From = from
	if to != nil {
		found.To = *to
	}

	s.mu.Lock()
	s.cache[key] = found
	s.mu.Unlock()
	return found, nil
}

// --- the arithmetic ---------------------------------------------------------

// Split decomposes a tax-inclusive amount into net and tax.
//
// The invariant is `net + tax == gross`, exactly, for every input. That is
// stronger than "correct to the nearest fils" and it is what the invoice's own
// CHECK constraint requires: a document whose arithmetic does not close is
// rejected by an accountant and by the database.
//
// Achieved by computing the tax with rounding and deriving the net by
// subtraction, never by rounding both. Rounding both is the classic way to
// produce an invoice that is one fils out — and it happens on roughly half of
// all amounts at a 5% rate, not on rare edge cases.
func Split(grossMinor int64, rateBP int) (netMinor, taxMinor int64) {
	if rateBP <= 0 {
		return grossMinor, 0
	}
	// tax = gross × rate / (10000 + rate), rounded half away from zero.
	//
	// Half *away from zero* rather than half up, so a refund of −1050 splits to
	// the exact negative of a charge of 1050. Half-up would round −25 to −24 and
	// leave a fils behind on every reversal.
	denom := int64(10000 + rateBP)
	num := grossMinor * int64(rateBP)
	taxMinor = divRoundHalfAway(num, denom)
	return grossMinor - taxMinor, taxMinor
}

// Add computes the tax on a net amount and the resulting gross.
//
// The business-to-business direction: a vendor's commission invoice is quoted
// net and VAT is added, because both parties reclaim it. Consumer prices never
// use this — see the package comment.
func Add(netMinor int64, rateBP int) (taxMinor, grossMinor int64) {
	if rateBP <= 0 {
		return 0, netMinor
	}
	taxMinor = divRoundHalfAway(netMinor*int64(rateBP), 10000)
	return taxMinor, netMinor + taxMinor
}

func divRoundHalfAway(num, denom int64) int64 {
	if denom == 0 {
		return 0
	}
	if num < 0 {
		return -divRoundHalfAway(-num, denom)
	}
	return (num + denom/2) / denom
}

// Line is one taxable row.
type Line struct {
	Description string
	Category    Category
	RateBP      int
	GrossMinor  int64
}

// Totals is a decomposed document.
type Totals struct {
	NetMinor   int64
	TaxMinor   int64
	GrossMinor int64
	// ByRate is the per-rate summary a tax invoice must print when more than
	// one rate appears on it. Keyed by basis points.
	ByRate map[int]RateTotal
}

type RateTotal struct {
	NetMinor int64
	TaxMinor int64
}

// SumLines decomposes each line and totals them.
//
// Per line, then summed — never the whole document at once. The two differ by a
// fils or two on multi-line orders, and only the per-line version matches what
// the invoice prints. A total computed independently of its own lines is the
// discrepancy an auditor finds first.
func SumLines(lines []Line) Totals {
	t := Totals{ByRate: map[int]RateTotal{}}
	for _, l := range lines {
		net, tax := Split(l.GrossMinor, l.RateBP)
		t.NetMinor += net
		t.TaxMinor += tax
		t.GrossMinor += l.GrossMinor

		rt := t.ByRate[l.RateBP]
		rt.NetMinor += net
		rt.TaxMinor += tax
		t.ByRate[l.RateBP] = rt
	}
	return t
}

// Closes reports whether the totals are internally consistent.
func (t Totals) Closes() bool { return t.NetMinor+t.TaxMinor == t.GrossMinor }
