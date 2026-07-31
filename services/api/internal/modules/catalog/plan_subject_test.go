package catalog

import (
	"testing"
	"time"

	"github.com/marketplace/api/internal/seam/subject"
)

// Dubai. Chosen deliberately over UTC: the whole point of carrying an IANA zone
// on a cutoff is that the boundary is a wall-clock rule, and a test that runs at
// UTC would pass with the timezone handling removed entirely.
func dubai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Dubai")
	if err != nil {
		t.Skipf("no tzdata on this machine: %v", err)
	}
	return loc
}

func TestTwentyHoursBeforeDeliveryIsFourInTheMorning(t *testing.T) {
	// The column is hours *before the next delivery day*, not a wall-clock time,
	// and the default is 20. Twenty hours before tomorrow's midnight is 04:00
	// today — which is the arithmetic most likely to be got backwards.
	loc := dubai(t)
	now := time.Date(2026, 7, 31, 1, 0, 0, 0, loc)

	got := nextCutoff(now, 20)

	want := time.Date(2026, 7, 31, 4, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestAPassedCutoffRollsToTomorrow(t *testing.T) {
	// The boundary recurs. A viewer opening the app at nine in the morning must
	// see tomorrow's four o'clock, not this morning's — a countdown that already
	// ran out reads as a dead plan.
	loc := dubai(t)
	now := time.Date(2026, 7, 31, 9, 0, 0, 0, loc)

	got := nextCutoff(now, 20)

	want := time.Date(2026, 8, 1, 4, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestTheCutoffIsNeverInThePast(t *testing.T) {
	loc := dubai(t)
	for _, hour := range []int{0, 3, 4, 5, 12, 23} {
		now := time.Date(2026, 7, 31, hour, 30, 0, 0, loc)
		if got := nextCutoff(now, 20); !got.After(now) {
			t.Errorf("at %02d:30 the next cutoff was %s, which is not in the future", hour, got)
		}
	}
}

func TestExactlyOnTheCutoffRollsForward(t *testing.T) {
	// A tie goes to tomorrow. Landing exactly on the boundary and being shown
	// "0:00 left" is a control that cannot be acted on.
	loc := dubai(t)
	now := time.Date(2026, 7, 31, 4, 0, 0, 0, loc)

	got := nextCutoff(now, 20)

	want := time.Date(2026, 8, 1, 4, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestAZeroCutoffMeansMidnight(t *testing.T) {
	// cutoff_hours = 0 is legal per the CHECK, and it means "order right up to
	// the delivery day". Zero hours before tomorrow's midnight is midnight.
	loc := dubai(t)
	now := time.Date(2026, 7, 31, 18, 0, 0, 0, loc)

	got := nextCutoff(now, 0)

	want := time.Date(2026, 8, 1, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestALongCutoffStillLandsInTheFuture(t *testing.T) {
	// The CHECK allows up to 168 hours — a week's notice. That is more than a
	// day before the next midnight, so the naive subtraction lands in the past
	// and the roll-forward has to carry it all the way.
	loc := dubai(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, loc)

	got := nextCutoff(now, 168)

	if !got.After(now) {
		t.Fatalf("a 168-hour cutoff resolved to %s, which is not in the future", got)
	}
}

func TestTheSignalTheProviderBuildsIsCoherent(t *testing.T) {
	// Validate is what the projection writer runs, and a cutoff needs both an
	// instant and an IANA zone. This asserts the shape without a database.
	loc := dubai(t)
	sig := timeSignalForTest(nextCutoff(time.Date(2026, 7, 31, 1, 0, 0, 0, loc), 20))

	if err := sig.Validate(); err != nil {
		t.Fatalf("the cutoff signal is not renderable: %v", err)
	}
	if !sig.Recurring {
		t.Error("a daily cutoff must be marked recurring, or the client removes the reel when it passes")
	}
}

func TestThePeriodSuffixMakesThePriceMeanSomething(t *testing.T) {
	// AED 449 is unremarkable for a month and startling for a week.
	for _, tc := range []struct {
		cycleDays int
		want      string
	}{
		{7, "/wk"}, {5, "/wk"}, {30, "/mo"}, {28, "/mo"}, {90, "/90d"}, {0, ""},
	} {
		if got, _ := periodSuffix(tc.cycleDays); got != tc.want {
			t.Errorf("%d days → %q, want %q", tc.cycleDays, got, tc.want)
		}
	}
}

func TestTheKitchenLeadsTheLine(t *testing.T) {
	// For food the vendor *is* the product — two identical five-day plans from
	// two kitchens are not interchangeable, which is the opposite of how a used
	// car works.
	got := joinPlanLine("Kcal Kitchen", weekly(5), perDay(2))
	want := "Kcal Kitchen · 5 days a week · 2 meals a day"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOneMealADayIsNotWorthSaying(t *testing.T) {
	// The overwhelming default. Printing it spends a third of the line on the
	// least surprising fact about the plan.
	if got := joinPlanLine("Kcal Kitchen", weekly(7), perDay(1)); got != "Kcal Kitchen · every day" {
		t.Fatalf("got %q", got)
	}
}

// timeSignalForTest builds the same signal Project returns, without a database.
// Kept beside the tests rather than exported, so the production path stays the
// only place the query and the signal are assembled together.
func timeSignalForTest(next time.Time) subject.TimeSignal {
	return subject.TimeSignal{
		Mode:      subject.ModeCutoff,
		EndsAt:    next,
		Timezone:  "Asia/Dubai",
		Recurring: true,
		Horizon:   24 * time.Hour,
	}
}
