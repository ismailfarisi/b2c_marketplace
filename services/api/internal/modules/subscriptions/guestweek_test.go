package subscriptions

import (
	"testing"
	"time"
)

func TestNextWeekIsAlwaysTheComingSundayToSaturday(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Dubai")
	cases := []struct {
		now  string
		from string
	}{
		{"2026-11-07T10:00:00+04:00", "2026-11-08"}, // Saturday -> tomorrow
		{"2026-11-08T10:00:00+04:00", "2026-11-15"}, // Sunday -> a full week ahead, never today
		{"2026-11-11T23:30:00+04:00", "2026-11-15"}, // Wednesday night
		{"2026-11-07T21:30:00Z", "2026-11-15"},      // 01:30 Sunday in Dubai: Sunday already started
	}
	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339, c.now)
		from, to := NextWeek(now, loc)
		if got := from.Format("2006-01-02"); got != c.from {
			t.Errorf("NextWeek(%s) from = %s, want %s", c.now, got, c.from)
		}
		if from.Weekday() != time.Sunday || to.Weekday() != time.Saturday || to.Sub(from) != 6*24*time.Hour {
			t.Errorf("NextWeek(%s) = %s..%s, want Sunday..Saturday", c.now, from, to)
		}
	}
}

func TestPerMealSpreadsTheCycleOverEverySlot(t *testing.T) {
	if got := PerMeal(60_000, 20, 1); got != 3_000 {
		t.Fatalf("20 days x 1 slot: got %d want 3000", got)
	}
	if got := PerMeal(60_000, 20, 2); got != 1_500 {
		t.Fatalf("20 days x 2 slots: got %d want 1500", got)
	}
	if got := PerMeal(60_001, 20, 1); got != 3_000 {
		t.Fatalf("floors: got %d", got)
	}
	if PerMeal(60_000, 0, 1) != 0 || PerMeal(0, 20, 1) != 0 || PerMeal(60_000, 20, 0) != 0 {
		t.Fatal("degenerate inputs must be 0, not a panic")
	}
}

func TestWithinBandIsExactInMinorUnits(t *testing.T) {
	cur := int64(3_000)
	for _, c := range []struct {
		cand int64
		ok   bool
	}{
		{3_000, true}, {3_450, true}, {2_550, true}, // exactly +-15% is inside
		{3_451, false}, {2_549, false}, {0, false},
	} {
		if got := WithinBand(c.cand, cur, GuestWeekBandPercent); got != c.ok {
			t.Errorf("WithinBand(%d, %d) = %v want %v", c.cand, cur, got, c.ok)
		}
	}
	if WithinBand(100, 0, 15) {
		t.Fatal("no current price means no band")
	}
}

func TestGuestWeekSplitNeverCreatesOrLosesMoney(t *testing.T) {
	cases := []struct {
		credit, week, wallet, due, leftover int64
	}{
		{15_000, 15_000, 15_000, 0, 0},     // same price: wallet pays all
		{15_000, 17_250, 15_000, 2_250, 0}, // guest 15% dearer: card pays the difference
		{15_000, 12_750, 12_750, 0, 2_250}, // guest 15% cheaper: difference stays in the wallet
		{0, 10_000, 0, 10_000, 0},          // nothing credited (cycle fully recognised): card pays
		{-5, -5, 0, 0, 0},                  // garbage in, zeros out
	}
	for _, c := range cases {
		w, d, l := GuestWeekSplit(c.credit, c.week)
		if w != c.wallet || d != c.due || l != c.leftover {
			t.Errorf("GuestWeekSplit(%d, %d) = (%d, %d, %d), want (%d, %d, %d)",
				c.credit, c.week, w, d, l, c.wallet, c.due, c.leftover)
		}
		if c.credit >= 0 && c.week >= 0 && (w+d != c.week || w+l != c.credit) {
			t.Errorf("GuestWeekSplit(%d, %d): wallet+due must equal the week and wallet+leftover the credit", c.credit, c.week)
		}
	}
}

func TestPauseCreditForAGuestWeek(t *testing.T) {
	// A 4-week, 5-day plan at 600 AED: one paused week is a quarter of the
	// cycle; after 3 weeks were recognised only the last quarter is left.
	if got := PauseCredit(60_000, 5, 20, 0); got != 15_000 {
		t.Fatalf("fresh cycle: got %d want 15000", got)
	}
	if got := PauseCredit(60_000, 5, 20, 45_000); got != 15_000 {
		t.Fatalf("three weeks recognised: got %d want 15000", got)
	}
	if got := PauseCredit(60_000, 5, 20, 50_000); got != 10_000 {
		t.Fatalf("credit is capped at what is unearned: got %d want 10000", got)
	}
}

func TestValidateCancellationCode(t *testing.T) {
	for _, c := range CancellationCodes {
		if err := ValidateCancellationCode(c); err != nil {
			t.Errorf("%q should be valid: %v", c, err)
		}
	}
	if err := ValidateCancellationCode(""); err != nil {
		t.Errorf("empty code is 'not given', not invalid: %v", err)
	}
	if err := ValidateCancellationCode("hangry"); err == nil {
		t.Error("unknown code must be rejected")
	}
}
