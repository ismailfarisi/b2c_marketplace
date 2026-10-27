package kitchen

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/httpx"
)

func codes(t *testing.T, err error) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err == nil {
		return out
	}
	var ve *httpx.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	for _, f := range ve.Fields {
		out[f.Field] = f.Code
	}
	return out
}

func validPlan() PlanInput {
	return PlanInput{
		Name: "Lunch, weekdays", CycleDays: 28, DaysPerWeek: 5,
		MealSlots: []string{"lunch"}, DeliveryWeekdays: []int{0, 1, 2, 3, 4},
		CutoffHours: 20, PriceMinor: 120000,
	}
}

func ptr[T any](v T) *T { return &v }

func TestNormalisePlanSortsAndDedupes(t *testing.T) {
	in := validPlan()
	in.MealSlots = []string{"dinner", "lunch", "dinner"}
	in.DeliveryWeekdays = []int{4, 0, 2, 1, 3, 0}
	out, err := NormalisePlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.MealSlots; len(got) != 2 || got[0] != "lunch" || got[1] != "dinner" {
		t.Fatalf("slots = %v", got)
	}
	if got := out.DeliveryWeekdays; len(got) != 5 || got[0] != 0 || got[4] != 4 {
		t.Fatalf("weekdays = %v", got)
	}
}

func TestNormalisePlanRejects(t *testing.T) {
	cases := map[string]struct {
		mut   func(*PlanInput)
		field string
		code  string
	}{
		"no name":            {func(p *PlanInput) { p.Name = "  " }, "name", "required"},
		"short cycle":        {func(p *PlanInput) { p.CycleDays = 3 }, "cycle_days", "out_of_range"},
		"bad slot":           {func(p *PlanInput) { p.MealSlots = []string{"brunch"} }, "meal_slots", "invalid_slot"},
		"more days than run": {func(p *PlanInput) { p.DaysPerWeek = 6 }, "days_per_week", "exceeds_delivery_days"},
		"free":               {func(p *PlanInput) { p.PriceMinor = 0 }, "price_minor", "required"},
		"rotation unpriced":  {func(p *PlanInput) { p.RotationEnabled = true }, "rotation_week_price_minor", "required"},
		"http image":         {func(p *PlanInput) { p.ImageURL = ptr("http://x/y.jpg") }, "image_url", "invalid_url"},
		"duplicate tier": {func(p *PlanInput) {
			p.Tiers = []TierInput{{Key: "std", Name: "Standard", PriceMinor: 1}, {Key: "std", Name: "Again", PriceMinor: 1}}
		}, "tiers[1].key", "duplicate"},
		"tier too many meals": {func(p *PlanInput) {
			p.Tiers = []TierInput{{Key: "big", Name: "Big", PriceMinor: 1, MealsPerDay: ptr(2)}}
		}, "tiers[0].meals_per_day", "exceeds_meal_slots"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := validPlan()
			tc.mut(&in)
			_, err := NormalisePlan(in)
			if got := codes(t, err)[tc.field]; got != tc.code {
				t.Fatalf("%s: want %s, got %q (all: %v)", tc.field, tc.code, got, codes(t, err))
			}
		})
	}
}

func TestNormalisePlanRotationPriced(t *testing.T) {
	in := validPlan()
	in.RotationEnabled = true
	in.RotationWeekPriceMinor = ptr(int64(32000))
	if _, err := NormalisePlan(in); err != nil {
		t.Fatal(err)
	}
}

func TestNormaliseDish(t *testing.T) {
	out, err := NormaliseDish(DishInput{
		Name: "  Chicken biryani ", Allergens: []string{"Milk", "milk", "tree_nuts"},
		Tags: []string{"Spicy", "spicy", "high-protein"}, Description: ptr("   "),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "Chicken biryani" || out.Description != nil {
		t.Fatalf("not trimmed: %+v", out)
	}
	if len(out.Allergens) != 2 || len(out.Tags) != 2 {
		t.Fatalf("not deduped: %v %v", out.Allergens, out.Tags)
	}

	_, err = NormaliseDish(DishInput{Name: "x", Allergens: []string{"nuts"}, Calories: ptr(-1)})
	c := codes(t, err)
	if c["allergens"] != "unknown_allergen" || c["calories"] != "out_of_range" {
		t.Fatalf("codes = %v", c)
	}
}

func TestValidateMenuSlot(t *testing.T) {
	a := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	b := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	none := uuid.NullUUID{}

	if err := ValidateMenuSlot(1, "lunch", a, b); err != nil {
		t.Fatalf("default + alternative: %v", err)
	}
	if err := ValidateMenuSlot(1, "lunch", none, none); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if c := codes(t, ValidateMenuSlot(1, "lunch", none, b)); c["alternative_dish_id"] != "default_required" {
		t.Fatalf("alt without default: %v", c)
	}
	if c := codes(t, ValidateMenuSlot(1, "lunch", a, a)); c["alternative_dish_id"] != "same_as_default" {
		t.Fatalf("same dish twice: %v", c)
	}
	if c := codes(t, ValidateMenuSlot(7, "brunch", a, none)); c["weekday"] == "" || c["slot"] == "" {
		t.Fatalf("bad cell: %v", c)
	}
}

func TestMenuGaps(t *testing.T) {
	plans := []Plan{
		{IsActive: true, DeliveryWeekdays: []int{1, 2}, MealSlots: []string{"lunch", "dinner"}},
		{IsActive: false, DeliveryWeekdays: []int{5}, MealSlots: []string{"breakfast"}},
	}
	menu := WeeklyMenu{Slots: []MenuSlot{
		{Weekday: 1, Slot: "lunch", Default: &MenuChoice{IsActive: true}},
		{Weekday: 1, Slot: "dinner", Default: &MenuChoice{IsActive: false}},
		{Weekday: 2, Slot: "lunch", Default: &MenuChoice{IsActive: true}},
	}}
	got := MenuGaps(plans, menu)
	want := []MenuGap{{1, "dinner"}, {2, "dinner"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("gaps = %v, want %v", got, want)
	}
}

func TestCanEdit(t *testing.T) {
	if !CanEdit("owner") || !CanEdit("manager") || CanEdit("staff") {
		t.Fatal("role gate")
	}
}
