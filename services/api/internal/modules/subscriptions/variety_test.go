package subscriptions

import (
	"errors"
	"strings"
	"testing"

	"github.com/marketplace/api/internal/httpx"
)

func ptr[T any](v T) *T { return &v }

func kitchenWith(cuisine string, dishes ...VarietyDish) VarietyKitchen {
	return VarietyKitchen{Key: cuisine, Cuisines: []string{cuisine}, Dishes: dishes}
}

func TestScoreVarietyFourDistinctCuisinesIsGreat(t *testing.T) {
	v := ScoreVariety([]VarietyKitchen{
		kitchenWith("kerala", VarietyDish{Weekday: 1, Name: "Fish curry"}),
		kitchenWith("pakistani", VarietyDish{Weekday: 1, Name: "Nihari"}),
		kitchenWith("lebanese", VarietyDish{Weekday: 1, Name: "Shish tawook"}),
		kitchenWith("filipino", VarietyDish{Weekday: 1, Name: "Adobo"}),
	})
	if v.Level != VarietyGreat || v.Score != 100 || v.DistinctCuisines != 4 || len(v.Warnings) != 0 {
		t.Fatalf("got %+v", v)
	}
}

func TestScoreVarietySameCuisineAndMondayBiryaniIsLow(t *testing.T) {
	biryani := VarietyDish{Weekday: 1, Name: "Chicken Biryani"}
	v := ScoreVariety([]VarietyKitchen{
		kitchenWith("indian", biryani),
		kitchenWith("Indian", VarietyDish{Weekday: 1, Name: "chicken biryani!"}),
		kitchenWith("indian", VarietyDish{Weekday: 1, Name: "Biryani, chicken"}),
		kitchenWith("indian", VarietyDish{Weekday: 2, Name: "Dal tadka"}),
	})
	if v.Level != VarietyLow || v.DistinctCuisines != 1 {
		t.Fatalf("level %s distinct %d", v.Level, v.DistinctCuisines)
	}
	if len(v.Warnings) != 2 {
		t.Fatalf("warnings %+v", v.Warnings)
	}
	same := v.Warnings[0]
	if same.Code != WarnSameCuisine || len(same.Cuisines) != 1 || same.Cuisines[0] != "indian" || same.Kitchens != 4 {
		t.Fatalf("same cuisine %+v", same)
	}
	rep := v.Warnings[1]
	if rep.Code != WarnRepeatedDish || rep.Dish != "Chicken Biryani" || rep.Weekday == nil || *rep.Weekday != 1 || rep.Kitchens != 3 {
		t.Fatalf("repeated dish %+v", rep)
	}
}

func TestScoreVarietyRepeatAcrossWeeksIsGood(t *testing.T) {
	v := ScoreVariety([]VarietyKitchen{
		kitchenWith("kerala", VarietyDish{Weekday: 0, Name: "Chicken biryani"}),
		kitchenWith("pakistani", VarietyDish{Weekday: 3, Name: "Chicken biryani"}),
		kitchenWith("lebanese", VarietyDish{Weekday: 1, Name: "Kibbeh"}),
		kitchenWith("indian", VarietyDish{Weekday: 1, Name: "Paneer"}),
	})
	if len(v.Warnings) != 1 || v.Warnings[0].Code != WarnRepeatedAcrossWeeks || v.Warnings[0].Kitchens != 2 ||
		v.Warnings[0].Weekday != nil {
		t.Fatalf("warnings %+v", v.Warnings)
	}
	if v.Score != 95 || v.Level != VarietyGreat {
		t.Fatalf("score %d level %s", v.Score, v.Level)
	}
}

func TestScoreVarietyMultiCuisineKitchensCountOnce(t *testing.T) {
	v := ScoreVariety([]VarietyKitchen{
		{Cuisines: []string{"indian", "kerala"}},
		{Cuisines: []string{"indian"}},
		{Cuisines: []string{"indian", "arab"}},
		{Cuisines: []string{"arab"}},
	})
	if v.DistinctCuisines != 3 || v.Level != VarietyGood || v.Score != 80 {
		t.Fatalf("got %+v", v)
	}
	if v.Warnings[0].Code != WarnSameCuisine || v.Warnings[0].Cuisines[0] != "indian" {
		t.Fatalf("warning %+v", v.Warnings[0])
	}
}

// Two kitchens tagged "indian, pakistani" are the same food twice; the
// matching must not hand one "indian" and the other "pakistani".
func TestScoreVarietyIdenticalMultiTagSetsCountOnce(t *testing.T) {
	v := ScoreVariety([]VarietyKitchen{
		{Cuisines: []string{"indian", "pakistani"}},
		{Cuisines: []string{"pakistani", "indian"}},
		{Cuisines: []string{"arab"}},
		{Cuisines: []string{"filipino"}},
	})
	if v.DistinctCuisines != 3 || v.Level == VarietyGreat {
		t.Fatalf("got %+v", v)
	}
	if len(v.Warnings) == 0 || v.Warnings[0].Code != WarnSameCuisine || v.Warnings[0].Kitchens != 2 {
		t.Fatalf("warning %+v", v.Warnings)
	}
}

func TestScoreVarietyMatchesArabicNames(t *testing.T) {
	v := ScoreVariety([]VarietyKitchen{
		kitchenWith("kerala", VarietyDish{Weekday: 2, Name: "Chicken machboos", NameAr: ptr("مَجْبُوس الدجاج")}),
		kitchenWith("arab", VarietyDish{Weekday: 2, Name: "Machboos dajaj", NameAr: ptr("مجبوس دجاج")}),
		kitchenWith("pakistani"),
		kitchenWith("filipino"),
	})
	if len(v.Warnings) != 1 || v.Warnings[0].Code != WarnRepeatedDish || v.Warnings[0].Kitchens != 2 ||
		v.Warnings[0].DishAr == nil {
		t.Fatalf("warnings %+v", v.Warnings)
	}
}

func TestNormalizeDishName(t *testing.T) {
	cases := []struct{ a, b string }{
		{"Chicken Biryani", "biryani - chicken"},
		{"Lentils with rice", "rice & lentil"},
		{"مَجْبُوس الدجاج", "مجبوس دجاج"},
		{"كبسة بالدجاج", "كبسه دجاج"},
		{"أرز", "ارز"},
		{"مـنـسـف", "منسف"},
		{"فتة", "فته"},
		{"شوربة عدس ٢", "شوربه عدس 2"},
		{"حلوى", "حلوي"},
		{"چکن بریانی", "جكن برياني"},
		{"کڑاہی گوشت", "كراهي كوشت"},
		{"کھانا ۲", "كهانا 2"},
		{"دجاج وبطاطس", "دجاج بطاطس"},
		{"چائے", "جائي"},
	}
	for _, c := range cases {
		if got, want := NormalizeDishName(c.a), NormalizeDishName(c.b); got != want || got == "" {
			t.Errorf("%q -> %q, %q -> %q", c.a, got, c.b, want)
		}
	}
	if NormalizeDishName("Fish curry") == NormalizeDishName("Chicken curry") {
		t.Error("different dishes must not match")
	}
	if NormalizeDishName("ورق عنب") != NormalizeDishName("ورق العنب") || NormalizeDishName("ورق عنب") == NormalizeDishName("رق عنب") {
		t.Errorf("short words keep a leading waw: %q", NormalizeDishName("ورق عنب"))
	}
	if NormalizeDishName("Grass") != "grass" {
		t.Errorf("ss words keep their s: %q", NormalizeDishName("Grass"))
	}
}

func TestNormalizeCuisinesAliases(t *testing.T) {
	got := normalizeCuisines([]string{"Arab", "Arabian", "arabic", "Desi", "North Indian", "Pinoy", "BBQ", "Thai"})
	want := []string{"arabic", "indian", "filipino", "grills", "thai"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("normalizeCuisines = %v, want %v", got, want)
	}
}

func TestValidateSchedule(t *testing.T) {
	cases := []struct {
		days  []int
		slots []string
		code  string
	}{
		{[]int{0, 2, 4}, []string{"lunch"}, ""},
		{nil, nil, ""},
		{[]int{7}, []string{"lunch"}, "invalid_weekday"},
		{[]int{-1}, []string{"lunch"}, "invalid_weekday"},
		{[]int{1, 1}, []string{"lunch"}, "duplicate"},
		{[]int{1}, []string{" "}, "empty_slot"},
		{[]int{1}, []string{"lunch", "lunch"}, "duplicate"},
	}
	for _, c := range cases {
		err := validateSchedule(c.days, c.slots)
		var ve *httpx.ValidationError
		switch {
		case c.code == "" && err != nil:
			t.Errorf("%v %v: unexpected %v", c.days, c.slots, err)
		case c.code != "" && (!errors.As(err, &ve) || ve.Fields[0].Code != c.code):
			t.Errorf("%v %v: got %v, want %s", c.days, c.slots, err, c.code)
		}
	}
}
