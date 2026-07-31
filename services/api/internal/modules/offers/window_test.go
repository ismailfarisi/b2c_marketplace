package offers

import (
	"testing"
	"time"
)

func dubai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Dubai")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	return loc
}

// A late-night offer: 22:00 to 02:00, every day. The case the vertical was named
// after before it was renamed for what it actually is.
func lateNight() Window {
	return Window{StartLocal: MinutesFromTime(22, 0), EndLocal: MinutesFromTime(2, 0)}
}

// A midday one: 14:00 to 16:00, weekdays only. The case the old name excluded.
func middayWeekdays() Window {
	return Window{
		StartLocal: MinutesFromTime(14, 0),
		EndLocal:   MinutesFromTime(16, 0),
		Days:       []int{1, 2, 3, 4, 5},
	}
}

func TestAnOvernightWindowIsFourHoursNotTwenty(t *testing.T) {
	if got := lateNight().Duration(); got != 4*time.Hour {
		t.Fatalf("22:00–02:00 lasted %s", got)
	}
	if !lateNight().CrossesMidnight() {
		t.Error("22:00–02:00 does not cross midnight?")
	}
}

func TestHalfPastMidnightIsStillInsideLastNightsWindow(t *testing.T) {
	// The bug this exists for: resolving from today only would report a live
	// offer as "starts at 22:00 tonight" while it is actually running, and the
	// CTA would be greyed out for four hours every night.
	loc := dubai(t)
	now := time.Date(2026, 7, 31, 0, 30, 0, 0, loc)

	occ, ok := lateNight().Resolve(now)
	if !ok {
		t.Fatal("no occurrence found")
	}
	if !occ.Live {
		t.Fatalf("00:30 was not inside the window that started %s", occ.Start)
	}
	if occ.Start.Day() != 30 {
		t.Errorf("the live window should have started yesterday, got %s", occ.Start)
	}
}

func TestAClaimAtHalfPastMidnightBelongsToLastNight(t *testing.T) {
	// Occurrence.Date files a claim under the night it began. Filing it under
	// the calendar day would let one person claim the same nightly offer twice
	// — once at 23:00 and again at 00:30.
	loc := dubai(t)
	occ, _ := lateNight().Resolve(time.Date(2026, 7, 31, 0, 30, 0, 0, loc))

	if occ.Date.Day() != 30 {
		t.Fatalf("claim filed under day %d, want 30", occ.Date.Day())
	}
}

func TestBeforeItOpensTheNextOccurrenceIsTonight(t *testing.T) {
	loc := dubai(t)
	now := time.Date(2026, 7, 31, 18, 0, 0, 0, loc)

	occ, ok := lateNight().Resolve(now)
	if !ok {
		t.Fatal("no occurrence found")
	}
	if occ.Live {
		t.Error("18:00 is not inside a 22:00 window")
	}
	want := time.Date(2026, 7, 31, 22, 0, 0, 0, loc)
	if !occ.Start.Equal(want) {
		t.Fatalf("next start %s, want %s", occ.Start, want)
	}
}

func TestAfterItClosesTheNextOccurrenceIsTomorrow(t *testing.T) {
	loc := dubai(t)
	// 03:00 — last night's window ended an hour ago.
	now := time.Date(2026, 7, 31, 3, 0, 0, 0, loc)

	occ, _ := lateNight().Resolve(now)

	want := time.Date(2026, 7, 31, 22, 0, 0, 0, loc)
	if !occ.Start.Equal(want) {
		t.Fatalf("next start %s, want tonight %s", occ.Start, want)
	}
}

func TestAMiddayWindowWorksToo(t *testing.T) {
	// The whole reason for the rename. 2026-07-31 is a Friday.
	loc := dubai(t)
	now := time.Date(2026, 7, 31, 15, 0, 0, 0, loc)

	occ, ok := middayWeekdays().Resolve(now)
	if !ok || !occ.Live {
		t.Fatalf("15:00 on a Friday should be inside 14:00–16:00 weekdays, got %+v ok=%v", occ, ok)
	}
	if got := middayWeekdays().Duration(); got != 2*time.Hour {
		t.Errorf("duration %s", got)
	}
}

func TestTheWeekendIsSkippedToMonday(t *testing.T) {
	loc := dubai(t)
	// Saturday.
	now := time.Date(2026, 8, 1, 15, 0, 0, 0, loc)

	occ, ok := middayWeekdays().Resolve(now)
	if !ok {
		t.Fatal("no occurrence found")
	}
	if occ.Live {
		t.Error("a weekday-only offer was live on Saturday")
	}
	if occ.Start.Weekday() != time.Monday {
		t.Fatalf("next start is a %s, want Monday", occ.Start.Weekday())
	}
}

func TestAFridayNightOfferRunsIntoSaturdayMorning(t *testing.T) {
	// The subtle one. The day-of-week set is tested against the *start* day, so
	// a Friday 22:00–02:00 offer is still running at 01:00 on Saturday. Testing
	// the end day would close it at midnight, four hours early, every week.
	loc := dubai(t)
	fridayNights := Window{
		StartLocal: MinutesFromTime(22, 0),
		EndLocal:   MinutesFromTime(2, 0),
		Days:       []int{5},
	}
	// 01:00 Saturday.
	now := time.Date(2026, 8, 1, 1, 0, 0, 0, loc)

	occ, ok := fridayNights.Resolve(now)
	if !ok || !occ.Live {
		t.Fatalf("a Friday-night offer was not live at 01:00 Saturday: %+v ok=%v", occ, ok)
	}
}

func TestACalendarRangeThatHasPassedHasNoOccurrence(t *testing.T) {
	loc := dubai(t)
	weekendOnly := lateNight()
	weekendOnly.RunsFrom = time.Date(2026, 7, 3, 0, 0, 0, 0, loc)
	weekendOnly.RunsUntil = time.Date(2026, 7, 5, 0, 0, 0, 0, loc)

	if _, ok := weekendOnly.Resolve(time.Date(2026, 7, 31, 23, 0, 0, 0, loc)); ok {
		t.Fatal("an offer whose run ended weeks ago still resolved")
	}
}

func TestTheLastNightOfARangeStillRunsPastMidnight(t *testing.T) {
	// runs_until is a date and the window crosses midnight, so the final
	// occurrence legitimately ends the morning *after* the last day. Truncating
	// at midnight would cut the closing night short.
	loc := dubai(t)
	w := lateNight()
	w.RunsUntil = time.Date(2026, 7, 31, 0, 0, 0, 0, loc)

	occ, ok := w.Resolve(time.Date(2026, 8, 1, 1, 0, 0, 0, loc))
	if !ok || !occ.Live {
		t.Fatalf("the last night was cut off at midnight: %+v ok=%v", occ, ok)
	}
}

func TestAWindowStartingBeforeTheRangeIsNotLive(t *testing.T) {
	loc := dubai(t)
	w := lateNight()
	w.RunsFrom = time.Date(2026, 8, 5, 0, 0, 0, 0, loc)

	occ, ok := w.Resolve(time.Date(2026, 7, 31, 23, 0, 0, 0, loc))
	if !ok {
		t.Fatal("a future range should still resolve to its first occurrence")
	}
	if occ.Live {
		t.Error("an offer that has not started yet was reported live")
	}
	if occ.Start.Day() != 5 {
		t.Fatalf("first occurrence %s, want the 5th", occ.Start)
	}
}

func TestTheResolvedWindowIsAlwaysInTheFuture(t *testing.T) {
	loc := dubai(t)
	for hour := 0; hour < 24; hour++ {
		now := time.Date(2026, 7, 31, hour, 17, 0, 0, loc)
		occ, ok := lateNight().Resolve(now)
		if !ok {
			t.Fatalf("%02d:17 resolved nothing", hour)
		}
		if !occ.End.After(now) {
			t.Errorf("%02d:17 resolved to a window that already ended at %s", hour, occ.End)
		}
		if occ.Live && occ.Start.After(now) {
			t.Errorf("%02d:17 was marked live but starts at %s", hour, occ.Start)
		}
	}
}

func TestAnEveryDayWindowNeverGoesQuiet(t *testing.T) {
	// A nightly offer must resolve on all seven days, or the reel drops out of
	// the feed one day a week for no reason anyone would find.
	loc := dubai(t)
	for day := 1; day <= 7; day++ {
		now := time.Date(2026, 7, day, 12, 0, 0, 0, loc)
		if _, ok := lateNight().Resolve(now); !ok {
			t.Errorf("no occurrence on 2026-07-%02d", day)
		}
	}
}
