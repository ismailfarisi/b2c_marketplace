package subscriptions

import (
	"testing"
	"time"
)

func TestFirstChangeableDay(t *testing.T) {
	dubai, _ := time.LoadLocation("Asia/Dubai")
	// 20h cutoff: tomorrow's cutoff was 04:00 today, so at 09:00 the first
	// day that can still change is the day after tomorrow.
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, dubai)
	got := FirstChangeableDay(20, dubai, now)
	want := time.Date(2026, 11, 5, 0, 0, 0, 0, dubai)
	if !got.Equal(want) {
		t.Fatalf("FirstChangeableDay = %v, want %v", got, want)
	}
	if !CanModify(got, 20, dubai, now) || CanModify(got.AddDate(0, 0, -1), 20, dubai, now) {
		t.Fatal("FirstChangeableDay disagrees with CanModify")
	}
}

func TestResumeWindow(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		name                 string
		from, to, changeable time.Time
		resumeOn, newTo      time.Time
		cancel, ok           bool
	}{
		{"not started yet: drop the whole pause", d(10), d(14), d(6), d(10), time.Time{}, true, true},
		{"starts on the first changeable day", d(10), d(14), d(10), d(10), time.Time{}, true, true},
		{"under way: keep the locked days", d(4), d(14), d(8), d(8), d(7), false, true},
		{"last day still changeable", d(4), d(14), d(14), d(14), d(13), false, true},
		{"everything left is locked", d(4), d(7), d(8), time.Time{}, time.Time{}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resumeOn, newTo, cancel, ok := ResumeWindow(c.from, c.to, c.changeable)
			if ok != c.ok || cancel != c.cancel || !resumeOn.Equal(c.resumeOn) || !newTo.Equal(c.newTo) {
				t.Fatalf("ResumeWindow = (%v, %v, %v, %v), want (%v, %v, %v, %v)",
					resumeOn, newTo, cancel, ok, c.resumeOn, c.newTo, c.cancel, c.ok)
			}
		})
	}
}
