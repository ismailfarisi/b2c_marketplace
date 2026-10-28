package subscriptions

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDayLabels(t *testing.T) {
	en, ar := dayLabels(time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
	if en != "Thu 29 Oct" || ar != "الخميس 29/10" {
		t.Fatalf("got %q / %q", en, ar)
	}
}

func TestServedDishPrefersChoiceThenDefault(t *testing.T) {
	ar := "برياني"
	a := Dish{ID: uuid.New(), Name: "Biryani", NameAr: &ar, IsDefault: true}
	b := Dish{ID: uuid.New(), Name: "Salad"}
	m := UpcomingMeal{Options: []Dish{b, a}}
	if got := servedDish(m); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("default not served: %v", got)
	}
	m.ChosenID = &b.ID
	if got := servedDish(m); got[0].ID != b.ID {
		t.Fatalf("choice not served: %v", got)
	}
	en, arNames := dishNames([]Dish{a, b})
	if en != "Biryani or Salad" || arNames != "برياني أو Salad" {
		t.Fatalf("got %q / %q", en, arNames)
	}
}
