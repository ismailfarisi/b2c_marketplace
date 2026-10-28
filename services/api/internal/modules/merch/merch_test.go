package merch

import (
	"testing"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/modules/kitchen"
)

func k(n byte, price int64, cuisines ...string) kitchen.PublicKitchen {
	return kitchen.PublicKitchen{
		BranchID: uuid.UUID{n}, FromPriceMinor: price, Cuisines: cuisines,
		MealSlots: []string{"lunch"},
	}
}

func ids(ks []kitchen.PublicKitchen) []byte {
	out := []byte{}
	for _, k := range ks {
		out = append(out, k.BranchID[0])
	}
	return out
}

func TestArrangePinsFirstExcludesAndCaps(t *testing.T) {
	all := []kitchen.PublicKitchen{k(1, 900, "indian"), k(2, 500, "arabic"), k(3, 700, "indian"), k(4, 300, "indian")}
	max := 3
	c := Collection{
		Rules: Rules{Cuisines: []string{"indian"}},
		Sort:  "price_asc", MaxItems: &max,
		Items: []Item{
			{BranchID: uuid.UUID{2}, Role: "pin", Position: 0}, // pinned even though arabic
			{BranchID: uuid.UUID{4}, Role: "exclude"},
			{BranchID: uuid.UUID{9}, Role: "pin", Position: 1}, // does not deliver here
		},
	}
	got := ids(Arrange(c, all, nil))
	want := []byte{2, 3, 1}
	if string(got) != string(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestRulesMatch(t *testing.T) {
	price := int64(600)
	rot := int64(30000)
	kk := k(1, 500, "indian")
	kk.RotationWeekPriceMinor = &rot
	cases := map[string]struct {
		r    Rules
		want bool
	}{
		"empty":        {Rules{}, true},
		"cuisine any":  {Rules{Cuisines: []string{"arabic", "indian"}}, true},
		"cuisine miss": {Rules{Cuisines: []string{"arabic"}}, false},
		"slot miss":    {Rules{Slots: []string{"dinner"}}, false},
		"rotation":     {Rules{RotationOnly: true}, true},
		"price ok":     {Rules{MaxPriceMinor: &price}, true},
	}
	for name, tc := range cases {
		if got := tc.r.Matches(kk); got != tc.want {
			t.Errorf("%s: got %v", name, got)
		}
	}
	if (Rules{RotationOnly: true}).Matches(k(2, 1, "x")) {
		t.Error("kitchen without rotation price matched rotation_only")
	}
}

func TestNormaliseRejects(t *testing.T) {
	if _, err := NormaliseCollection(CollectionInput{Slug: "Bad Slug", Title: "x"}); err == nil {
		t.Error("bad slug accepted")
	}
	if _, err := NormaliseCollection(CollectionInput{Slug: "ok", Title: "x", Rules: Rules{Slots: []string{"brunch"}}}); err == nil {
		t.Error("bad slot accepted")
	}
	js := "javascript:alert(1)"
	if _, err := NormaliseBanner(BannerInput{Title: "x", LinkURL: &js}); err == nil {
		t.Error("javascript link accepted")
	}
	if _, err := NormaliseSections([]Section{{Type: SectionRail}}); err == nil {
		t.Error("rail without collection accepted")
	}
	if _, err := NormaliseSections([]Section{{Type: "nope"}}); err == nil {
		t.Error("unknown type accepted")
	}
	if _, err := NormaliseItems([]Item{{BranchID: uuid.UUID{1}, Role: "pin"}, {BranchID: uuid.UUID{1}, Role: "exclude"}}); err == nil {
		t.Error("duplicate item accepted")
	}
}
