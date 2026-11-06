package subscriptions

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNextWeekNoticeDue(t *testing.T) {
	for _, tz := range []string{"Asia/Dubai", "Asia/Riyadh"} {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Fatal(err)
		}
		at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, loc) }
		// 2026-11-12 is a Thursday; next week starts Sunday 15th.
		cases := []struct {
			now  time.Time
			due  bool
			name string
		}{
			{at(12, 17, 59), false, "Thursday before 18:00"},
			{at(12, 18, 0), true, "Thursday 18:00"},
			{at(13, 9, 0), true, "Friday catch-up"},
			{at(14, 23, 59), true, "Saturday catch-up"},
			{at(15, 0, 0), false, "Sunday"},
			{at(11, 20, 0), false, "Wednesday"},
		}
		for _, c := range cases {
			week, ok := NextWeekNoticeDue(c.now, loc)
			if ok != c.due {
				t.Errorf("%s %s: due=%v, want %v", tz, c.name, ok, c.due)
			}
			if ok && week.Format("2006-01-02") != "2026-11-15" {
				t.Errorf("%s %s: week %s, want 2026-11-15", tz, c.name, week.Format("2006-01-02"))
			}
		}
	}
	// 18:00 in Riyadh is 19:00 in Dubai: the same instant is due in Dubai but
	// not yet in Riyadh at 17:30 Riyadh.
	dubai, _ := time.LoadLocation("Asia/Dubai")
	riyadh, _ := time.LoadLocation("Asia/Riyadh")
	instant := time.Date(2026, 11, 12, 18, 30, 0, 0, dubai)
	if _, ok := NextWeekNoticeDue(instant, dubai); !ok {
		t.Error("18:30 Dubai should be due")
	}
	if _, ok := NextWeekNoticeDue(instant, riyadh); ok {
		t.Error("17:30 Riyadh should not be due yet")
	}
}

func TestPreviewMenuWeekAcrossCycle(t *testing.T) {
	week := func(name string) map[previewKey][]PreviewDish {
		m := map[previewKey][]PreviewDish{}
		for d := 0; d < 7; d++ {
			m[previewKey{weekday: d, slot: "lunch"}] = []PreviewDish{{ID: uuid.New(), Name: name}}
		}
		return m
	}
	anchor := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	menu := previewMenu{anchor: anchor, weeks: 3, byWeek: map[int]map[previewKey][]PreviewDish{
		1: week("Biryani"), 2: week("Kabsa"), 3: week("Sinigang"),
	}}
	cases := map[string]string{
		"2026-11-03": "Biryani", "2026-11-10": "Kabsa", "2026-11-19": "Sinigang",
		"2026-11-22": "Biryani", "2026-10-28": "Sinigang",
	}
	for date, want := range cases {
		d, _ := time.Parse("2006-01-02", date)
		got := menu.dishes(d, "lunch")
		if len(got) != 1 || got[0].Name != want {
			t.Errorf("%s: got %v, want %s", date, got, want)
		}
	}
	// A week not filled in yet falls back to week 1.
	delete(menu.byWeek, 2)
	d, _ := time.Parse("2006-01-02", "2026-11-10")
	if got := menu.dishes(d, "lunch"); len(got) != 1 || got[0].Name != "Biryani" {
		t.Errorf("empty week fallback: got %v", got)
	}
}

func TestRecapReady(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if RecapReady(start, start.AddDate(0, 0, 20)) {
		t.Error("week 3 must not show the recap")
	}
	if !RecapReady(start, start.AddDate(0, 0, 21)) {
		t.Error("first day of week 4 shows the recap")
	}
	if !RecapReady(start, start.AddDate(0, 0, 40)) {
		t.Error("after the cycle ends the recap stays")
	}
}

func TestNoticeDishes(t *testing.T) {
	ar := "مندي"
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	p := NextWeekPreview{Meals: []PreviewMeal{
		{Dishes: []PreviewDish{{ID: a, Name: "Mandi", NameAr: &ar}, {ID: b, Name: "Kabsa", Chosen: true}}},
		{Dishes: []PreviewDish{{ID: a, Name: "Mandi", NameAr: &ar}}},
		{Dishes: []PreviewDish{{ID: b, Name: "Kabsa"}}},
		{Dishes: []PreviewDish{{ID: c, Name: "Salad"}}},
	}}
	en, arOut := noticeDishes(p)
	if en != "Kabsa, Mandi, Salad" {
		t.Errorf("en = %q", en)
	}
	if arOut != "Kabsa، مندي، Salad" {
		t.Errorf("ar = %q", arOut)
	}
}
