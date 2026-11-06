package subscriptions

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// A meal's options come from the cycle week its date falls in.
func TestCycleMenuOnPicksTheDatesWeek(t *testing.T) {
	anchor := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) // Sunday, week 1
	one, two := Dish{ID: uuid.New(), Name: "Biryani"}, Dish{ID: uuid.New(), Name: "Kabsa"}
	m := cycleMenu{anchor: anchor, weeks: 2, dishes: map[menuKey][]Dish{
		{1, 1, "lunch"}: {one},
		{2, 1, "lunch"}: {two},
	}}
	cases := map[string]string{"2026-11-02": "Biryani", "2026-11-09": "Kabsa", "2026-11-16": "Biryani"}
	for day, want := range cases {
		d, _ := time.Parse(time.DateOnly, day)
		got := m.on(d, "lunch")
		if len(got) != 1 || got[0].Name != want {
			t.Errorf("%s: %v, want %s", day, got, want)
		}
	}
	m.weeks = 1
	if got := m.on(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), "lunch"); len(got) != 1 || got[0].Name != "Biryani" {
		t.Errorf("one-week menu: %v", got)
	}
}
