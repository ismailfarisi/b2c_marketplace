package company

import (
	"testing"

	"github.com/marketplace/api/internal/modules/subscriptions"
)

func TestPolicyCovers(t *testing.T) {
	p := Policy{Weekdays: []int{0, 1, 2, 3, 4}, Slots: []string{"lunch"}, MealCapMinor: 3500}
	if !p.covers(0, "lunch") {
		t.Fatal("Sunday lunch should be covered")
	}
	if p.covers(5, "lunch") || p.covers(1, "dinner") {
		t.Fatal("Friday and dinner are outside the policy")
	}
	if err := (Policy{Weekdays: []int{7}, Slots: []string{"lunch"}, MealCapMinor: 1}).validate(); err == nil {
		t.Fatal("weekday 7 must be rejected")
	}
	if err := (Policy{Weekdays: []int{1}, Slots: []string{"lunch"}}).validate(); err == nil {
		t.Fatal("a zero cap must be rejected")
	}
}

func TestPerMeal(t *testing.T) {
	rules := []subscriptions.Rule{{Weekday: 1, Slot: "lunch"}, {Weekday: 2, Slot: "lunch"}}
	// 20 deliverable days, one meal a day, AED 600 → AED 30 a meal.
	if got := perMeal(subscriptions.SubscriptionPreview{TotalMinor: 60000, DeliverableDays: 20}, rules); got != 3000 {
		t.Fatalf("per meal = %d, want 3000", got)
	}
	two := append(rules, subscriptions.Rule{Weekday: 1, Slot: "dinner"}, subscriptions.Rule{Weekday: 2, Slot: "dinner"})
	if got := perMeal(subscriptions.SubscriptionPreview{TotalMinor: 60000, DeliverableDays: 20}, two); got != 1500 {
		t.Fatalf("per meal with two meals a day = %d, want 1500", got)
	}
}

func TestCreditAvailable(t *testing.T) {
	c := Credit{UnbilledMinor: 40000, OutstandingMinor: 50000}.withLimit(100000)
	if c.AvailableMinor != 10000 {
		t.Fatalf("available = %d, want 10000", c.AvailableMinor)
	}
	if c = (Credit{UnbilledMinor: 200000}).withLimit(100000); c.AvailableMinor != 0 {
		t.Fatalf("available over limit = %d, want 0", c.AvailableMinor)
	}
}

func TestRegisterValidation(t *testing.T) {
	ok := RegisterInput{Name: "Acme", LegalName: "Acme LLC", BillingEmail: "ap@acme.ae",
		Office: OfficeInput{Label: "HQ", City: "Dubai", Lat: 25.2, Lng: 55.3}}
	if err := ok.validate(); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	bad := ok
	bad.TRN = "12345"
	if bad.validate() == nil {
		t.Fatal("short TRN must be rejected")
	}
	bad = ok
	bad.Office.Lat, bad.Office.Lng = 0, 0
	if bad.validate() == nil {
		t.Fatal("missing office location must be rejected")
	}
}
