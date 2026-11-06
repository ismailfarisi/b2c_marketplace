package kitchen

import (
	"testing"
	"time"
)

func TestCycleWeek(t *testing.T) {
	anchor := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) // a Sunday
	day := func(off int) time.Time { return anchor.AddDate(0, 0, off) }
	cases := []struct {
		weeks, off, want int
	}{
		{1, 30, 1},
		{2, 0, 1}, {2, 6, 1}, {2, 7, 2}, {2, 13, 2}, {2, 14, 1},
		{3, 21, 1}, {3, 20, 3},
		{4, -1, 4}, {4, -7, 4}, {4, -8, 3}, {4, -28, 1},
	}
	for _, c := range cases {
		if got := CycleWeek(anchor, c.weeks, day(c.off)); got != c.want {
			t.Errorf("weeks=%d off=%d: got %d, want %d", c.weeks, c.off, got, c.want)
		}
	}
	dubai, _ := time.LoadLocation("Asia/Dubai")
	late := time.Date(2026, 11, 8, 23, 30, 0, 0, dubai)
	if got := CycleWeek(anchor, 2, late); got != 2 {
		t.Errorf("calendar day in zone: got %d, want 2", got)
	}
}
