package kitchen

import (
	"testing"
	"time"
)

func TestCycleWeek(t *testing.T) {
	anchor := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) // a Sunday
	day := func(offset int) time.Time { return anchor.AddDate(0, 0, offset) }
	cases := []struct {
		weeks, offset, want int
	}{
		{1, 0, 1}, {1, 9, 1}, {1, -30, 1}, {0, 15, 1},
		{2, 0, 1}, {2, 6, 1}, {2, 7, 2}, {2, 13, 2}, {2, 14, 1},
		{3, 20, 3}, {3, 21, 1},
		{4, 27, 4}, {4, 28, 1}, {4, 35, 2},
		// Before the anchor the weeks count backwards.
		{2, -1, 2}, {2, -7, 2}, {2, -8, 1}, {4, -7, 4}, {4, -28, 1}, {4, -29, 4},
	}
	for _, c := range cases {
		if got := CycleWeek(anchor, c.weeks, day(c.offset)); got != c.want {
			t.Errorf("CycleWeek(weeks=%d, day %+d) = %d, want %d", c.weeks, c.offset, got, c.want)
		}
	}
	// Calendar days: the time of day and zone of either date do not matter.
	dubai := time.FixedZone("GST", 4*3600)
	late := time.Date(2026, 11, 8, 23, 30, 0, 0, dubai)
	if got := CycleWeek(anchor.Add(5*time.Hour), 2, late); got != 2 {
		t.Errorf("late on day 7 = %d, want 2", got)
	}
}

func TestMenuCalendar(t *testing.T) {
	a := "2026-11-01"
	menu := WeeklyMenu{Weeks: 2, AnchorDate: &a, Slots: []MenuSlot{
		{Week: 1, Weekday: 1, Slot: "lunch", Default: &MenuChoice{Name: "Biryani", IsActive: true}},
		{Week: 2, Weekday: 1, Slot: "lunch", Default: &MenuChoice{Name: "Kabsa", IsActive: true}},
	}}
	// Wednesday of the anchor's second week.
	cal := menu.Calendar(time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC), 2)
	if len(cal) != 2 || cal[0].StartsOn != "2026-11-08" || cal[1].StartsOn != "2026-11-15" {
		t.Fatalf("calendar weeks = %+v", cal)
	}
	if len(cal[0].Slots) != 1 || cal[0].Slots[0].Default.Name != "Kabsa" {
		t.Fatalf("this week = %+v, want week 2's Kabsa", cal[0].Slots)
	}
	if len(cal[1].Slots) != 1 || cal[1].Slots[0].Default.Name != "Biryani" {
		t.Fatalf("next week = %+v, want week 1's Biryani", cal[1].Slots)
	}
	if got := menu.LiveWeek(2); len(got) != 1 || got[0].Default.Name != "Kabsa" {
		t.Fatalf("LiveWeek(2) = %+v", got)
	}
}

func TestWeekGapsCoverEveryWeek(t *testing.T) {
	plans := []Plan{{IsActive: true, DeliveryWeekdays: []int{0, 1}, MealSlots: []string{"lunch"}}}
	menu := WeeklyMenu{Weeks: 1, Slots: []MenuSlot{
		{Week: 1, Weekday: 0, Slot: "lunch", Default: &MenuChoice{IsActive: true}},
		{Week: 1, Weekday: 1, Slot: "lunch", Default: &MenuChoice{IsActive: true}},
		{Week: 2, Weekday: 0, Slot: "lunch", Default: &MenuChoice{IsActive: true}},
	}}
	if gaps := MenuGaps(plans, menu); len(gaps) != 0 {
		t.Fatalf("one live week has gaps %v", gaps)
	}
	gaps := WeekGaps(plans, menu, 2)
	if len(gaps) != 1 || gaps[0] != (MenuGap{Weekday: 1, Slot: "lunch", Week: 2}) {
		t.Fatalf("two weeks gaps = %v, want Monday lunch in week 2", gaps)
	}
}
