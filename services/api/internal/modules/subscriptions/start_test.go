package subscriptions

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEarliestStartSkipsDaysPastCutoff(t *testing.T) {
	dubai, _ := time.LoadLocation("Asia/Dubai")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, dubai)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		now    string
		cutoff int
		want   string
	}{
		// 20h cutoff: tomorrow's deadline is 04:00 today.
		{"2026-11-05 03:59", 20, "2026-11-06"},
		{"2026-11-05 04:00", 20, "2026-11-07"},
		{"2026-11-05 18:00", 20, "2026-11-07"},
		// 48h cutoff at noon: the day after tomorrow's deadline passed at midnight.
		{"2026-11-05 12:00", 48, "2026-11-08"},
		// No cutoff: the deadline is the start of the day itself, so tomorrow.
		{"2026-11-05 23:00", 0, "2026-11-06"},
		// 30h cutoff at 01:00: tomorrow's deadline was 18:00 yesterday.
		{"2026-11-05 01:00", 30, "2026-11-07"},
	}
	for _, c := range cases {
		got := EarliestStart(c.cutoff, dubai, at(c.now)).Format("2006-01-02")
		if got != c.want {
			t.Errorf("now %s cutoff %dh: got %s, want %s", c.now, c.cutoff, got, c.want)
		}
		// The day before the answer is always locked, the answer never is.
		e := EarliestStart(c.cutoff, dubai, at(c.now))
		if !CanModify(e, c.cutoff, dubai, at(c.now)) {
			t.Errorf("now %s: earliest %s is already locked", c.now, got)
		}
	}
}

func TestSameRulesIgnoresOrder(t *testing.T) {
	w1, w2 := uuid.New(), uuid.New()
	a := []Rule{{Weekday: 0, Slot: "lunch", WindowID: w1}, {Weekday: 1, Slot: "lunch", WindowID: w1}}
	b := []Rule{{Weekday: 1, Slot: "lunch", WindowID: w1}, {Weekday: 0, Slot: "lunch", WindowID: w1}}
	if !SameRules(a, b) {
		t.Fatal("same schedule in another order should match")
	}
	c := []Rule{{Weekday: 0, Slot: "lunch", WindowID: w1}, {Weekday: 1, Slot: "lunch", WindowID: w2}}
	if SameRules(a, c) {
		t.Fatal("a different window must not match")
	}
	if SameRules(a, a[:1]) {
		t.Fatal("a shorter schedule must not match")
	}
}
