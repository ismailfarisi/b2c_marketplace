// Package payouts turns the ledger into a document a vendor can argue with.
//
// The governing rule, and the reason this package is thin: **a statement is
// derived, never authoritative.** Every number on it is a sum over
// `ledger_entries` for a period. The statement exists so a vendor can be shown a
// stable version of that sum — a screen that recomputes on every load shows a
// different total the moment a late refund lands, and then nobody can be paid
// against it. If a statement and the ledger ever disagree, the ledger is right
// and the statement is a bug.
//
// PLAN.md §2.2 put it as: the ledger is the source of truth for money, and
// everything else is a view.
package payouts

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Kind classifies a statement line.
//
// Closed set, because "how much of last month was refunds" is a question the
// finance side asks every month and free text cannot answer it.
type Kind string

const (
	KindOrder        Kind = "order"
	KindSubscription Kind = "subscription"
	KindRefund       Kind = "refund"
	KindAdjustment   Kind = "adjustment"
	// KindCODOffset is cash a driver collected on this vendor's behalf. It is
	// already in the vendor's payable — the platform never received it — so it
	// appears as a line and is deducted, otherwise the vendor is paid twice for
	// the same order: once in cash at the door and once in the payout.
	KindCODOffset Kind = "cod_offset"
	KindPenalty   Kind = "penalty"
)

// Line is one entry on a statement.
type Line struct {
	JournalID     uuid.NullUUID
	OccurredOn    time.Time
	Kind          Kind
	ReferenceType string
	ReferenceID   uuid.NullUUID
	Description   string

	GrossMinor      int64
	CommissionMinor int64
	VATMinor        int64
	NetMinor        int64
}

// Totals is what appears at the bottom of the document.
type Totals struct {
	GrossMinor       int64
	CommissionMinor  int64
	VATMinor         int64
	AdjustmentsMinor int64
	NetMinor         int64
}

// Sum folds lines into totals.
//
// Net is computed from the components rather than summed from the lines'
// own net figures, and the difference matters: summing line nets would let a
// line whose own arithmetic is wrong pass through into a total that still looks
// internally consistent. Deriving it means the database CHECK on
// `payout_statements` is testing something real.
func Sum(lines []Line) Totals {
	var t Totals
	for _, l := range lines {
		switch l.Kind {
		case KindAdjustment, KindPenalty, KindCODOffset:
			// Adjustments carry their whole value in net and never attract
			// commission — the platform does not take a cut of a goodwill
			// credit or of a penalty it levied itself.
			t.AdjustmentsMinor += l.NetMinor
		default:
			t.GrossMinor += l.GrossMinor
			t.CommissionMinor += l.CommissionMinor
			t.VATMinor += l.VATMinor
		}
	}
	t.NetMinor = t.GrossMinor - t.CommissionMinor - t.VATMinor + t.AdjustmentsMinor
	return t
}

// Validate re-checks the invariant the database also enforces.
//
// Both, deliberately. The CHECK constraint is what makes a bad statement
// impossible to store; this is what makes the failure legible — a constraint
// violation surfacing from a payout run tells whoever is on call that something
// is wrong and nothing about what.
func (t Totals) Validate() error {
	want := t.GrossMinor - t.CommissionMinor - t.VATMinor + t.AdjustmentsMinor
	if t.NetMinor != want {
		return fmt.Errorf(
			"payouts: statement does not balance — gross %d - commission %d - vat %d + adjustments %d = %d, but net is %d",
			t.GrossMinor, t.CommissionMinor, t.VATMinor, t.AdjustmentsMinor, want, t.NetMinor)
	}
	if t.CommissionMinor < 0 || t.VATMinor < 0 || t.GrossMinor < 0 {
		return fmt.Errorf("payouts: negative gross, commission or VAT — %+v", t)
	}
	if t.VATMinor > t.CommissionMinor {
		// Platform VAT is charged on the platform's commission. VAT exceeding it
		// means the two were computed from different bases, which is a bug that
		// otherwise surfaces as a vendor querying a number that is quietly wrong
		// by a few percent every month.
		return fmt.Errorf("payouts: VAT %d exceeds commission %d", t.VATMinor, t.CommissionMinor)
	}
	return nil
}

// Payable reports whether this statement is worth issuing.
//
// A zero or negative net is not an error — a vendor whose refunds exceeded their
// sales genuinely owes the platform — but it must not become a payment
// instruction. Carrying it forward as an adjustment on the next period is the
// standard treatment and the one a vendor expects; issuing a bill for AED 12
// against a vendor who will trade again next week is a relationship cost with no
// cash benefit.
func (t Totals) Payable() bool { return t.NetMinor > 0 }

// Period is a payout window.
//
// Weekly by default and aligned to the GCC working week: Sunday to Saturday.
// An ISO week starting Monday cuts the Gulf weekend in half, so a vendor's
// busiest two days land in different statements and their weekly figures
// oscillate for reasons that have nothing to do with their business.
type Period struct {
	Start time.Time
	End   time.Time
}

// WeekOf returns the Sunday–Saturday period containing t, in the given location.
func WeekOf(t time.Time, loc *time.Location) Period {
	if loc == nil {
		loc = time.UTC
	}
	local := t.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	// time.Weekday() is already Sunday = 0, which is the convention this market
	// uses and the same one the Phase 5 scheduler settled on.
	start := day.AddDate(0, 0, -int(day.Weekday()))
	return Period{Start: start, End: start.AddDate(0, 0, 6)}
}

// PreviousWeek is the period a payout run should close.
//
// Never the current one. A statement covering a period that is still accruing is
// a number that changes after it was shown, and vendors reconcile against the
// version they saw.
func PreviousWeek(now time.Time, loc *time.Location) Period {
	current := WeekOf(now, loc)
	return Period{
		Start: current.Start.AddDate(0, 0, -7),
		End:   current.End.AddDate(0, 0, -7),
	}
}

func (p Period) Contains(t time.Time) bool {
	return !t.Before(p.Start) && !t.After(p.End.AddDate(0, 0, 1).Add(-time.Nanosecond))
}
