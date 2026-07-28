package subscriptions

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var dubai = mustLoad("Asia/Dubai")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// Windows without tzdata. The embedded database is imported by the
		// binary (see doc.go); if this fails the test environment is the
		// problem, not the code.
		return time.FixedZone("Asia/Dubai", 4*60*60)
	}
	return loc
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, dubai)
}

// A Sunday-to-Thursday lunch plan, which is the ordinary GCC working week.
func weekdayLunch() []Rule {
	window := uuid.New()
	var rules []Rule
	for _, wd := range []Weekday{0, 1, 2, 3, 4} { // Sunday..Thursday
		rules = append(rules, Rule{Weekday: wd, Slot: "lunch", WindowID: window})
	}
	return rules
}

func activeSub() Subscription {
	return Subscription{
		ID:        uuid.New(),
		Start:     date(2026, 8, 1),
		Location:  dubai,
		AddressID: uuid.New(),
	}
}

func TestPlanCoversOnlyTheScheduledWeekdays(t *testing.T) {
	sub := activeSub()
	now := date(2026, 8, 2) // Sunday

	got := Plan(sub, weekdayLunch(), nil, nil, now, now.AddDate(0, 0, 13), now)

	if len(got) == 0 {
		t.Fatal("a fourteen-day window produced no deliveries")
	}
	for _, o := range got {
		switch o.Date.Weekday() {
		case time.Friday, time.Saturday:
			t.Fatalf("delivery scheduled on %s, which the plan does not cover",
				o.Date.Format("Monday 2006-01-02"))
		}
	}
}

func TestWeekdayNumberingStartsOnSunday(t *testing.T) {
	// 0 = Sunday, matching branch_hours and menu_cycle_days. ISO-8601 starts on
	// Monday, and mixing the two delivers every meal one day late — invisible in
	// review, extremely visible to the customer.
	sub := activeSub()
	now := date(2026, 8, 2) // a Sunday

	rules := []Rule{{Weekday: 0, Slot: "lunch", WindowID: uuid.New()}}
	got := Plan(sub, rules, nil, nil, now, now.AddDate(0, 0, 6), now)

	if len(got) != 1 {
		t.Fatalf("a single-weekday rule produced %d deliveries in a week", len(got))
	}
	if got[0].Date.Weekday() != time.Sunday {
		t.Fatalf("Weekday(0) resolved to %s, want Sunday", got[0].Date.Weekday())
	}
}

func TestPlanNeverDescribesThePast(t *testing.T) {
	// PLAN.md §4: pauses and skips "only touch unmaterialized or
	// future-materialized fulfillments — never past ones". The cleanest
	// guarantee is a planner that cannot describe the past at all.
	sub := activeSub()
	now := date(2026, 8, 10)

	got := Plan(sub, weekdayLunch(), nil, nil, date(2026, 8, 1), now.AddDate(0, 0, 5), now)

	for _, o := range got {
		if o.Date.Before(now) {
			t.Fatalf("planned a delivery for %s, which is in the past",
				o.Date.Format("2006-01-02"))
		}
	}
}

func TestPlanIsIdempotent(t *testing.T) {
	// The nightly job, a manual trigger and a retry after a partial failure all
	// call this. It describes desired state rather than a change, so two calls
	// with the same inputs must be identical — otherwise the unique index on
	// (subscription, date, sequence) becomes the mechanism rather than the
	// safety net.
	sub := activeSub()
	now := date(2026, 8, 2)
	rules := weekdayLunch()

	first := Plan(sub, rules, nil, nil, now, now.AddDate(0, 0, 13), now)
	second := Plan(sub, rules, nil, nil, now, now.AddDate(0, 0, 13), now)

	if len(first) != len(second) {
		t.Fatalf("two identical runs produced %d and %d occurrences", len(first), len(second))
	}
	for i := range first {
		if !first[i].Date.Equal(second[i].Date) ||
			first[i].Slot != second[i].Slot ||
			first[i].Sequence != second[i].Sequence {
			t.Fatalf("run %d differs: %+v vs %+v", i, first[i], second[i])
		}
	}
}

func TestPauseSuppressesItsWholeRangeInclusive(t *testing.T) {
	// "Away from the 3rd to the 7th" includes the 7th. Reading the range
	// exclusively delivers one meal to an empty flat every single time.
	sub := activeSub()
	now := date(2026, 8, 2)
	pause := Pause{From: date(2026, 8, 3), To: date(2026, 8, 6), Mode: "extend"}

	got := Plan(sub, weekdayLunch(), []Pause{pause}, nil, now, now.AddDate(0, 0, 13), now)

	for _, o := range got {
		if !o.Date.Before(pause.From) && !o.Date.After(pause.To) {
			t.Fatalf("delivery on %s falls inside the pause %s..%s",
				o.Date.Format("2006-01-02"),
				pause.From.Format("2006-01-02"), pause.To.Format("2006-01-02"))
		}
	}
}

func TestCancelledPauseStopsSuppressing(t *testing.T) {
	sub := activeSub()
	now := date(2026, 8, 2)
	pause := Pause{From: date(2026, 8, 3), To: date(2026, 8, 6), Inactive: true}

	with := Plan(sub, weekdayLunch(), []Pause{pause}, nil, now, now.AddDate(0, 0, 13), now)
	without := Plan(sub, weekdayLunch(), nil, nil, now, now.AddDate(0, 0, 13), now)

	if len(with) != len(without) {
		t.Fatalf("a cancelled pause still suppressed deliveries: %d vs %d",
			len(with), len(without))
	}
}

func TestSkipRemovesOneSlotNotTheWholeDay(t *testing.T) {
	sub := activeSub()
	now := date(2026, 8, 2)
	window := uuid.New()
	rules := []Rule{
		{Weekday: 1, Slot: "lunch", WindowID: window},
		{Weekday: 1, Slot: "dinner", WindowID: window},
	}
	skip := Skip{Date: date(2026, 8, 3), Slot: "lunch"} // the Monday

	got := Plan(sub, rules, nil, []Skip{skip}, now, now.AddDate(0, 0, 6), now)

	var onSkipDay []Occurrence
	for _, o := range got {
		if o.Date.Equal(skip.Date) {
			onSkipDay = append(onSkipDay, o)
		}
	}
	if len(onSkipDay) != 1 {
		t.Fatalf("skipping one slot left %d deliveries that day, want 1", len(onSkipDay))
	}
	if onSkipDay[0].Slot != "dinner" {
		t.Fatalf("the wrong slot survived: %q", onSkipDay[0].Slot)
	}
}

func TestASlotlessSkipRemovesTheWholeDay(t *testing.T) {
	sub := activeSub()
	now := date(2026, 8, 2)
	window := uuid.New()
	rules := []Rule{
		{Weekday: 1, Slot: "lunch", WindowID: window},
		{Weekday: 1, Slot: "dinner", WindowID: window},
	}
	skip := Skip{Date: date(2026, 8, 3)}

	for _, o := range Plan(sub, rules, nil, []Skip{skip}, now, now.AddDate(0, 0, 6), now) {
		if o.Date.Equal(skip.Date) {
			t.Fatalf("a whole-day skip left %q on %s", o.Slot, o.Date.Format("2006-01-02"))
		}
	}
}

func TestSkippingDoesNotRenumberTheRemainingSlots(t *testing.T) {
	// Sequence is the uniqueness key on fulfillments. If skipping lunch promoted
	// dinner from sequence 2 to sequence 1, the next run would see sequence 2 as
	// missing and create a duplicate dinner.
	sub := activeSub()
	now := date(2026, 8, 2)
	window := uuid.New()
	rules := []Rule{
		{Weekday: 1, Slot: "lunch", WindowID: window},
		{Weekday: 1, Slot: "dinner", WindowID: window},
	}

	plain := Plan(sub, rules, nil, nil, now, now.AddDate(0, 0, 6), now)
	skipped := Plan(sub, rules, nil, []Skip{{Date: date(2026, 8, 3), Slot: "lunch"}},
		now, now.AddDate(0, 0, 6), now)

	seqFor := func(occ []Occurrence, d time.Time, slot string) int {
		for _, o := range occ {
			if o.Date.Equal(d) && o.Slot == slot {
				return o.Sequence
			}
		}
		return -1
	}

	want := seqFor(plain, date(2026, 8, 3), "dinner")
	got := seqFor(skipped, date(2026, 8, 3), "dinner")
	if want != got {
		t.Fatalf("dinner's sequence changed from %d to %d when lunch was skipped", want, got)
	}
}

func TestSlotsAreSequencedChronologically(t *testing.T) {
	sub := activeSub()
	now := date(2026, 8, 2)
	window := uuid.New()
	// Deliberately declared out of order — the caller's ordering must not decide
	// the delivery sheet's.
	rules := []Rule{
		{Weekday: 1, Slot: "dinner", WindowID: window},
		{Weekday: 1, Slot: "breakfast", WindowID: window},
		{Weekday: 1, Slot: "lunch", WindowID: window},
	}

	got := Plan(sub, rules, nil, nil, now, now.AddDate(0, 0, 6), now)

	var day []Occurrence
	for _, o := range got {
		if o.Date.Equal(date(2026, 8, 3)) {
			day = append(day, o)
		}
	}
	if len(day) != 3 {
		t.Fatalf("got %d slots on the day, want 3", len(day))
	}
	want := []string{"breakfast", "lunch", "dinner"}
	for i, slot := range want {
		if day[i].Slot != slot || day[i].Sequence != i+1 {
			t.Fatalf("position %d is %q seq %d, want %q seq %d",
				i, day[i].Slot, day[i].Sequence, slot, i+1)
		}
	}
}

func TestRamadanSlotsSortWhereTheyFallInTheDay(t *testing.T) {
	// PLAN.md §12: meal subscriptions invert during Ramadan — suhoor and iftar
	// *are* the schedule, not exotic additions. Sorting them to the end produces
	// a delivery sheet in the wrong order for a month.
	if slotOrder("suhoor") >= slotOrder("breakfast") {
		t.Error("suhoor does not sort before breakfast")
	}
	if slotOrder("iftar") >= slotOrder("dinner") {
		t.Error("iftar does not sort before dinner")
	}
	if slotOrder("lunch") >= slotOrder("iftar") {
		t.Error("lunch does not sort before iftar")
	}
}

func TestAnUnknownSlotIsOrderedLastNotRejected(t *testing.T) {
	// A vertical adding a slot the engine has never heard of must not need a
	// change in this package.
	sub := activeSub()
	now := date(2026, 8, 2)
	window := uuid.New()
	rules := []Rule{
		{Weekday: 1, Slot: "midnight_snack", WindowID: window},
		{Weekday: 1, Slot: "lunch", WindowID: window},
	}

	got := Plan(sub, rules, nil, nil, now, now.AddDate(0, 0, 6), now)
	if len(got) != 1*2 {
		t.Fatalf("got %d occurrences, want 2", len(got))
	}
	if got[0].Slot != "lunch" {
		t.Fatalf("the unknown slot sorted first: %q", got[0].Slot)
	}
}

func TestPlanStopsAtTheEndDate(t *testing.T) {
	sub := activeSub()
	sub.End = date(2026, 8, 5)
	now := date(2026, 8, 2)

	for _, o := range Plan(sub, weekdayLunch(), nil, nil, now, now.AddDate(0, 0, 13), now) {
		if o.Date.After(sub.End) {
			t.Fatalf("planned %s, past the end date %s",
				o.Date.Format("2006-01-02"), sub.End.Format("2006-01-02"))
		}
	}
}

func TestPlanDoesNotStartBeforeTheStartDate(t *testing.T) {
	sub := activeSub()
	sub.Start = date(2026, 8, 10)
	now := date(2026, 8, 2)

	for _, o := range Plan(sub, weekdayLunch(), nil, nil, now, now.AddDate(0, 0, 20), now) {
		if o.Date.Before(sub.Start) {
			t.Fatalf("planned %s, before the start date", o.Date.Format("2006-01-02"))
		}
	}
}

func TestASuspendedSubscriptionMaterialisesNothing(t *testing.T) {
	// The dunning grace period's effect. PLAN.md §6: a failed renewal suspends
	// fulfillments and never deletes the subscription — so the rules, the
	// address and the history all survive, and only new work stops.
	sub := activeSub()
	sub.Suspended = true
	now := date(2026, 8, 2)

	if got := Plan(sub, weekdayLunch(), nil, nil, now, now.AddDate(0, 0, 13), now); len(got) != 0 {
		t.Fatalf("a suspended subscription produced %d deliveries", len(got))
	}
}

func TestDatesAreLocalNotUTC(t *testing.T) {
	// The bug this guards is specific and easy to write: at 21:00 in Dubai it is
	// already 17:00 UTC on the same day, but at 03:00 Dubai it is 23:00 UTC on
	// the *previous* day. Truncating in UTC shifts every early-morning
	// materialisation back a day for the whole Gulf.
	sub := activeSub()
	// 02:30 on Monday the 3rd, Dubai time — 22:30 Sunday in UTC.
	now := time.Date(2026, 8, 3, 2, 30, 0, 0, dubai)

	rules := []Rule{{Weekday: 1, Slot: "lunch", WindowID: uuid.New()}} // Monday
	got := Plan(sub, rules, nil, nil, now, now.AddDate(0, 0, 2), now)

	if len(got) == 0 {
		t.Fatal("no delivery planned for today")
	}
	if got[0].Date.Day() != 3 {
		t.Fatalf("planned for day %d, want 3 — the date was truncated in the wrong zone",
			got[0].Date.Day())
	}
}

func TestPerDayAddressOverride(t *testing.T) {
	// Office Sunday to Thursday, home on Friday is the normal case in this
	// market, not an edge one.
	sub := activeSub()
	office := uuid.New()
	now := date(2026, 8, 2)

	rules := []Rule{
		{Weekday: 0, Slot: "lunch", WindowID: uuid.New(),
			AddressID: uuid.NullUUID{UUID: office, Valid: true}},
		{Weekday: 5, Slot: "lunch", WindowID: uuid.New()}, // Friday, no override
	}

	got := Plan(sub, rules, nil, nil, now, now.AddDate(0, 0, 6), now)

	for _, o := range got {
		switch o.Date.Weekday() {
		case time.Sunday:
			if o.AddressID != office {
				t.Error("the Sunday override was not applied")
			}
		case time.Friday:
			if o.AddressID != sub.AddressID {
				t.Error("Friday did not fall back to the subscription address")
			}
		}
	}
}

// --- cutoff -------------------------------------------------------------------

func TestCutoffRefusesALateSkip(t *testing.T) {
	// Past the cutoff the kitchen has bought the ingredients and often cooked
	// them. A skip after it is waste the vendor absorbs.
	service := date(2026, 8, 5)

	// 20 hours before midnight on the 5th is 04:00 on the 4th.
	if CanModify(service, 20, dubai, time.Date(2026, 8, 4, 6, 0, 0, 0, dubai)) {
		t.Error("a skip six hours past the cutoff was allowed")
	}
	if !CanModify(service, 20, dubai, time.Date(2026, 8, 4, 2, 0, 0, 0, dubai)) {
		t.Error("a skip two hours before the cutoff was refused")
	}
}

func TestCutoffIsRefusedExactlyAtTheBoundary(t *testing.T) {
	// A customer who is one second late, gets their skip accepted, and is then
	// charged is a support ticket. One who is refused sees a clear message.
	service := date(2026, 8, 5)
	exactly := time.Date(2026, 8, 4, 4, 0, 0, 0, dubai)

	if CanModify(service, 20, dubai, exactly) {
		t.Fatal("modification exactly at the cutoff was allowed")
	}
}

func TestAZeroCutoffAllowsChangesUpToMidnight(t *testing.T) {
	service := date(2026, 8, 5)
	if !CanModify(service, 0, dubai, time.Date(2026, 8, 4, 23, 59, 0, 0, dubai)) {
		t.Error("a zero cutoff refused a change the day before")
	}
	if CanModify(service, 0, dubai, time.Date(2026, 8, 5, 0, 1, 0, 0, dubai)) {
		t.Error("a zero cutoff allowed a change on the service day")
	}
}

// --- counting -----------------------------------------------------------------

func TestPausedDaysCountsDeliveryDaysNotCalendarDays(t *testing.T) {
	// A customer on a Sunday-to-Thursday plan who pauses over a weekend has lost
	// nothing. Charging them two pause-days against their allowance, or
	// crediting them for two meals they were never going to get, is wrong in
	// both directions.
	sub := activeSub()
	now := date(2026, 8, 2)
	// 2026-08-07 is a Friday; 08-08 a Saturday.
	weekend := Pause{From: date(2026, 8, 7), To: date(2026, 8, 8)}

	if got := PausedDays(sub, weekdayLunch(), weekend, now); got != 0 {
		t.Fatalf("a weekend pause consumed %d delivery days, want 0", got)
	}

	week := Pause{From: date(2026, 8, 9), To: date(2026, 8, 13)} // Sun..Thu
	if got := PausedDays(sub, weekdayLunch(), week, now); got != 5 {
		t.Fatalf("a full working week consumed %d delivery days, want 5", got)
	}
}

func TestPausedDaysIgnoresDaysAlreadyDelivered(t *testing.T) {
	// A pause backdated over meals already delivered does not un-deliver them,
	// and crediting for them would be a refund dressed up as a pause.
	sub := activeSub()
	now := date(2026, 8, 12) // Wednesday
	backdated := Pause{From: date(2026, 8, 9), To: date(2026, 8, 13)}

	got := PausedDays(sub, weekdayLunch(), backdated, now)
	if got != 2 { // the 12th and the 13th
		t.Fatalf("a backdated pause counted %d days, want 2", got)
	}
}

func TestDeliveryDaysBetweenIsTheRecognitionDivisor(t *testing.T) {
	// Frozen when a cycle opens, so a mid-cycle pause cannot retroactively
	// change the daily rate applied to days already recognised.
	got := DeliveryDaysBetween(weekdayLunch(), dubai, date(2026, 8, 2), date(2026, 8, 29))
	if got != 20 { // four Sun–Thu weeks
		t.Fatalf("a four-week Sunday-to-Thursday cycle has %d delivery days, want 20", got)
	}
}

func TestNoRulesMeansNoDeliveries(t *testing.T) {
	sub := activeSub()
	now := date(2026, 8, 2)
	if got := Plan(sub, nil, nil, nil, now, now.AddDate(0, 0, 13), now); len(got) != 0 {
		t.Fatalf("a subscription with no schedule produced %d deliveries", len(got))
	}
}
