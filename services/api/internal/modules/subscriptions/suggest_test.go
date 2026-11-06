package subscriptions

import (
	"testing"

	"github.com/google/uuid"
)

func cand(cuisine string, price int64, rating float64, count int) suggestCandidate {
	return suggestCandidate{
		PlanID: uuid.New(), BranchID: uuid.New(), Cuisines: []string{cuisine},
		Rating: &rating, RatingCount: count, WeekPrice: price, Currency: "AED",
		DaysPerWeek: 5, MealsPerDay: 1, Weekdays: []int{0, 1, 2, 3, 4}, Slots: []string{"lunch"},
	}
}

func TestSuggestionPoolDropsPoorlyRatedAndOffSchedule(t *testing.T) {
	poor := cand("arab", 20000, 2.5, 12)
	newKitchen := cand("lebanese", 20000, 2.0, 1)
	weekend := cand("kerala", 20000, 4.8, 20)
	weekend.Weekdays = []int{5, 6}
	weekend.DaysPerWeek = 2
	dinner := cand("indian", 20000, 4.8, 20)
	dinner.Slots = []string{"dinner"}
	ok := cand("filipino", 20000, 4.5, 20)

	pool, days, slots := suggestionPool([]suggestCandidate{poor, newKitchen, weekend, dinner, ok},
		[]int{0, 1, 2, 3, 4}, []string{"lunch"})
	got := map[uuid.UUID]bool{}
	for _, c := range pool {
		got[c.BranchID] = true
	}
	if got[poor.BranchID] || got[weekend.BranchID] || got[dinner.BranchID] {
		t.Fatalf("ineligible kitchen in pool")
	}
	if !got[newKitchen.BranchID] || !got[ok.BranchID] {
		t.Fatalf("eligible kitchen missing")
	}
	if len(days) != 5 || len(slots) != 1 || slots[0] != "lunch" {
		t.Fatalf("schedule %v %v", days, slots)
	}
}

func TestSuggestionPoolPicksTheSharedSchedule(t *testing.T) {
	cands := []suggestCandidate{cand("a", 1, 4, 5), cand("b", 1, 4, 5), cand("c", 1, 4, 5)}
	odd := cand("d", 1, 4, 5)
	odd.Weekdays = []int{1, 2, 3, 4, 5}
	cands = append(cands, odd)
	pool, days, slots := suggestionPool(cands, nil, nil)
	if len(pool) != 3 || len(days) != 5 || days[0] != 0 || days[4] != 4 || slots[0] != "lunch" {
		t.Fatalf("pool %d days %v slots %v", len(pool), days, slots)
	}
}

func TestSuggestionPoolSeparatesCurrencies(t *testing.T) {
	a, b, c := cand("a", 1, 4, 5), cand("b", 1, 4, 5), cand("c", 1, 4, 5)
	d := cand("d", 1, 4, 5)
	d.Currency = "SAR"
	pool, _, _ := suggestionPool([]suggestCandidate{a, b, c, d}, nil, nil)
	if len(pool) != 3 {
		t.Fatalf("pool %d", len(pool))
	}
}

func TestPickBalancedPrefersCuisineDiversityThenPriceBand(t *testing.T) {
	pool := []suggestCandidate{
		cand("indian", 20000, 5, 50), cand("indian", 20000, 5, 50), cand("indian", 20000, 5, 50),
		cand("kerala", 21000, 4.2, 10), cand("arab", 22000, 4.1, 10),
		cand("filipino", 90000, 4.9, 30), cand("lebanese", 23000, 4.0, 10),
	}
	got := pickBalanced(pool)
	if len(got) != 4 {
		t.Fatalf("picked %d", len(got))
	}
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c.Cuisines[0]] {
			t.Fatalf("repeated cuisine %s", c.Cuisines[0])
		}
		seen[c.Cuisines[0]] = true
		if c.WeekPrice > 30000 {
			t.Fatalf("out of price band: %d", c.WeekPrice)
		}
	}
}

func TestPickBalancedAvoidsSameDayDish(t *testing.T) {
	biryani := []VarietyDish{{Weekday: 1, Name: "Chicken biryani"}}
	a, b, c, d, e := cand("indian", 1, 4.5, 5), cand("kerala", 1, 4.5, 5), cand("arab", 1, 4.5, 5),
		cand("pakistani", 1, 4.5, 5), cand("lebanese", 1, 4.4, 5)
	a.dishes, b.dishes, c.dishes, d.dishes = biryani, biryani, biryani, biryani
	got := pickBalanced([]suggestCandidate{a, b, c, d, e})
	found := false
	for _, k := range got {
		found = found || k.BranchID == e.BranchID
	}
	if !found {
		t.Fatal("expected the kitchen without biryani to replace one with it")
	}
}
