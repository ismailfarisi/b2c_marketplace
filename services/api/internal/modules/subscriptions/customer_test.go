package subscriptions

import (
	"testing"
	"time"
)

func TestCutoffAt(t *testing.T) {
	dubai, _ := time.LoadLocation("Asia/Dubai")
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, dubai)
	got := CutoffAt(day, 14, dubai)
	want := time.Date(2026, 11, 2, 10, 0, 0, 0, dubai)
	if !got.Equal(want) {
		t.Fatalf("CutoffAt = %v, want %v", got, want)
	}
	for _, now := range []time.Time{got.Add(-time.Minute), got, got.Add(time.Minute)} {
		if CanModify(day, 14, dubai, now) != now.Before(got) {
			t.Fatalf("CanModify disagrees with CutoffAt at %v", now)
		}
	}
}
