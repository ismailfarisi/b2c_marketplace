package subscriptions

import (
	"testing"
	"time"
)

func TestCutoffAt(t *testing.T) {
	dubai, _ := time.LoadLocation("Asia/Dubai")
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, dubai)
	got := CutoffAt(day, 12*time.Hour, 14, dubai)
	want := time.Date(2026, 11, 2, 22, 0, 0, 0, dubai)
	if !got.Equal(want) {
		t.Fatalf("CutoffAt = %v, want %v", got, want)
	}
	if got := CutoffAt(day, 12*time.Hour, 0, dubai); !got.Equal(day.Add(12 * time.Hour)) {
		t.Fatalf("zero cutoff = %v", got)
	}
}
