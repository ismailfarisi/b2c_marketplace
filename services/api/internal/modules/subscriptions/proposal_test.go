package subscriptions

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/marketplace/api/internal/modules/taste"
)

func pk(name string, rating float64, price int64, cuisines ...string) ProposalKitchen {
	r := rating
	return ProposalKitchen{BranchID: uuid.New(), PlanID: uuid.New(), KitchenName: name,
		Rating: &r, RatingCount: 10, WeekPriceMinor: price, Cuisines: cuisines, Currency: "AED"}
}

func names(ks []ProposalKitchen) []string {
	out := make([]string, 0, len(ks))
	for _, k := range ks {
		out = append(out, k.KitchenName)
	}
	return out
}

func set(cs ...string) map[string]bool {
	m := map[string]bool{}
	for _, c := range cs {
		m[c] = true
	}
	return m
}

func testCurrent() []ProposalKitchen {
	cur := []ProposalKitchen{
		pk("Amma", 4.2, 20000, "kerala"),
		pk("Bint", 3.9, 21000, "lebanese"),
		pk("Chai", 4.6, 19000, "pakistani"),
		pk("Dosa", 4.0, 18000, "indian"),
	}
	for i := range cur {
		cur[i].InCurrent = true
	}
	cur[0].Favourite = true // hearted
	cur[2].OwnRating = 5    // rated by the customer
	cur[1].OwnRating = 3
	return cur
}

func TestProposeKeepsTwoFavouritesAndFillsFresh(t *testing.T) {
	cur := testCurrent()
	others := []ProposalKitchen{
		pk("Eid Kitchen", 4.8, 22000, "emirati"),
		pk("Fili", 4.5, 17000, "filipino"),
		pk("Ghee", 4.9, 25000, "kerala"),
	}
	tc := tasteContext{adventure: taste.Balanced, familiar: set("kerala", "lebanese", "pakistani", "indian"),
		recent: set("kerala", "lebanese", "pakistani", "indian"), avoided: map[uuid.UUID]bool{}}
	got := proposeKitchens(cur, others, tc)
	if len(got) != RotationKitchens {
		t.Fatalf("want 4 kitchens, got %v", names(got))
	}
	if got[0].KitchenName != "Amma" || got[0].Reason != ReasonLoved {
		t.Errorf("hearted kitchen should lead as loved, got %s/%s", got[0].KitchenName, got[0].Reason)
	}
	if got[1].KitchenName != "Chai" || got[1].Reason != ReasonTopRated {
		t.Errorf("own 5-star kitchen should be kept as top_rated, got %s/%s", got[1].KitchenName, got[1].Reason)
	}
	for _, k := range got[2:] {
		if k.InCurrent {
			t.Errorf("only two current kitchens may be kept; %s was kept too", k.KitchenName)
		}
		if k.Reason == "" {
			t.Errorf("%s has no reason", k.KitchenName)
		}
	}
	// Bint (own rating 3) gives way to fresh kitchens; Dosa was never rated.
	for _, n := range names(got) {
		if n == "Bint" || n == "Dosa" {
			t.Errorf("%s should have been replaced: %v", n, names(got))
		}
	}
}

func TestProposeNeverSuggestsAvoidedKitchens(t *testing.T) {
	cur := testCurrent()
	bad := pk("Bad", 5.0, 15000, "emirati")
	others := []ProposalKitchen{bad, pk("Fili", 4.5, 17000, "filipino")}
	tc := tasteContext{adventure: taste.Balanced, familiar: set(), recent: set(),
		avoided: map[uuid.UUID]bool{bad.BranchID: true, cur[0].BranchID: true}}
	got := proposeKitchens(cur, others, tc)
	for _, k := range got {
		if k.BranchID == bad.BranchID {
			t.Fatal("an avoided kitchen was proposed")
		}
		if k.BranchID == cur[0].BranchID {
			t.Fatal("an avoided current kitchen was kept though others could fill the slot")
		}
	}
	if len(got) != RotationKitchens {
		t.Fatalf("want 4, got %v", names(got))
	}
}

func TestProposeSurprisePrefersUneatenCuisines(t *testing.T) {
	cur := testCurrent()
	others := []ProposalKitchen{
		pk("More Kerala", 4.9, 20000, "kerala"),
		pk("Manila", 4.1, 20000, "filipino"),
		pk("Beirut", 4.0, 20000, "lebanese"),
		pk("Addis", 3.8, 20000, "ethiopian"),
	}
	recent := set("kerala", "lebanese", "pakistani", "indian")
	surprise := proposeKitchens(cur, others, tasteContext{adventure: taste.Surprise, familiar: recent, recent: recent, avoided: map[uuid.UUID]bool{}})
	fresh := names(surprise[2:])
	if fresh[0] != "Manila" || fresh[1] != "Addis" {
		t.Errorf("surprise should pick cuisines not eaten recently, got %v", fresh)
	}
	for _, k := range surprise[2:] {
		if k.Reason != ReasonNewCuisine || k.ReasonCuisine == "" {
			t.Errorf("%s: want new_cuisine with a cuisine, got %s/%q", k.KitchenName, k.Reason, k.ReasonCuisine)
		}
	}
	familiar := proposeKitchens(cur, others, tasteContext{adventure: taste.Familiar, familiar: recent, recent: recent, avoided: map[uuid.UUID]bool{}})
	fresh = names(familiar[2:])
	// Known cuisines only; Lebanese first because Kerala is already on the
	// rotation (Amma), then the best-rated Kerala kitchen.
	if fresh[0] != "Beirut" || fresh[1] != "More Kerala" {
		t.Errorf("familiar should pick known cuisines, spread across cuisines, got %v", fresh)
	}
	if familiar[2].Reason != ReasonFamiliarCuisine {
		t.Errorf("want familiar_cuisine, got %s", familiar[2].Reason)
	}
}

func TestProposeSpreadsCuisines(t *testing.T) {
	cur := testCurrent()
	others := []ProposalKitchen{
		pk("Manila 1", 4.9, 20000, "filipino"),
		pk("Manila 2", 4.8, 20000, "filipino"),
		pk("Addis", 4.0, 20000, "ethiopian"),
	}
	got := proposeKitchens(cur, others, tasteContext{adventure: taste.Surprise, familiar: set(), recent: set(), avoided: map[uuid.UUID]bool{}})
	fresh := names(got[2:])
	if fresh[0] != "Manila 1" || fresh[1] != "Addis" {
		t.Errorf("a second Filipino kitchen should lose to a different cuisine, got %v", fresh)
	}
}

func TestProposeFallsBackToCurrentKitchens(t *testing.T) {
	cur := testCurrent()
	got := proposeKitchens(cur, nil, tasteContext{adventure: taste.Balanced, familiar: set(), recent: set(), avoided: map[uuid.UUID]bool{}})
	if len(got) != RotationKitchens {
		t.Fatalf("want 4, got %v", names(got))
	}
	if got[2].Reason != ReasonKept || got[3].Reason != ReasonKept {
		t.Errorf("current kitchens that stay for lack of choice are kept: %v", got)
	}
}

func TestCapProposalSubstitutesCheaperOrKeepsPrevious(t *testing.T) {
	cur := testCurrent() // 20000+21000+19000+18000 = 78000; cap 85800
	cheap := pk("Fili", 4.5, 17000, "filipino")
	dear := pk("Gold", 4.9, 40000, "emirati")
	dear2 := pk("Platinum", 4.7, 39000, "emirati")
	proposed := []ProposalKitchen{cur[0], cur[2], dear, dear2} // 118000, over the cap
	pool := append([]ProposalKitchen{}, cur...)
	pool = append(pool, cheap, dear, dear2)
	tc := tasteContext{adventure: taste.Balanced, familiar: set(), recent: set(), avoided: map[uuid.UUID]bool{}}

	got := capProposal(proposed, cur, pool, 78000, tc)
	if total := proposalTotal(got); total > 85800 {
		t.Fatalf("total %d over the cap: %v", total, names(got))
	}
	if got[0].BranchID != cur[0].BranchID || got[1].BranchID != cur[2].BranchID {
		t.Errorf("kept favourites must not be touched by the cap: %v", names(got))
	}
	var sawCheap, sawPrevious bool
	for _, k := range got[2:] {
		if k.BranchID == cheap.BranchID {
			sawCheap = true
		}
		if k.InCurrent {
			sawPrevious = true
			if k.Reason != ReasonKept {
				t.Errorf("a previous kitchen put back reads as kept, got %s", k.Reason)
			}
		}
	}
	if !sawCheap || !sawPrevious {
		t.Errorf("want the cheaper suggestion and a previous kitchen, got %v", names(got))
	}

	// Within the cap nothing changes.
	ok := []ProposalKitchen{cur[0], cur[2], cheap, pk("Addis", 4.0, 20000, "ethiopian")}
	if same := capProposal(ok, cur, pool, 78000, tc); names(same)[3] != "Addis" {
		t.Errorf("a proposal within the cap must stay as is: %v", names(same))
	}
}

func rotationFor(refresh bool) Rotation {
	r := Rotation{RefreshOnRenew: refresh}
	for w := 1; w <= RotationKitchens; w++ {
		r.Weeks = append(r.Weeks, RotationWeek{Week: w, PlanID: uuid.New(), BranchID: uuid.New()})
	}
	r.EndDate = "2026-03-28"
	return r
}

func TestRenewalPlansRefreshOffIsUnchanged(t *testing.T) {
	r := rotationFor(false)
	called := false
	got, err := renewalPlans(r, RenewOptions{Refresh: true}, func() ([]uuid.UUID, error) {
		called = true
		return []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("refresh off must not ask for a proposal")
	}
	for i, w := range r.Weeks {
		if got[i] != w.PlanID {
			t.Fatalf("refresh off must renew the same plans in week order")
		}
	}
}

func TestRenewalPlansRefreshOnUsesProposal(t *testing.T) {
	r := rotationFor(true)
	want := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	got, err := renewalPlans(r, RenewOptions{Refresh: true}, func() ([]uuid.UUID, error) { return want, nil })
	if err != nil || got[0] != want[0] || got[3] != want[3] {
		t.Fatalf("refresh on should take the proposal, got %v (%v)", got, err)
	}
	// A manual renewal without an edited list keeps the kitchens even with refresh on.
	got, _ = renewalPlans(r, RenewOptions{}, func() ([]uuid.UUID, error) { t.Fatal("not refreshing"); return nil, nil })
	if got[0] != r.Weeks[0].PlanID {
		t.Error("a plain renew keeps the same plans")
	}
	// The customer's own list wins over everything.
	edited := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	got, _ = renewalPlans(r, RenewOptions{PlanIDs: edited, Refresh: true}, func() ([]uuid.UUID, error) { return want, nil })
	if got[1] != edited[1] {
		t.Error("edited plan_ids must be used as given")
	}
	// A proposal that fails falls back to the same kitchens via the caller's nil, nil.
	got, err = renewalPlans(r, RenewOptions{Refresh: true}, func() ([]uuid.UUID, error) { return nil, nil })
	if err != nil || got[0] != r.Weeks[0].PlanID {
		t.Error("no proposal: same plans")
	}
	if _, err = renewalPlans(r, RenewOptions{Refresh: true}, func() ([]uuid.UUID, error) { return nil, errors.New("boom") }); err == nil {
		t.Error("a proposal error is returned")
	}
}

func TestRenewalWindowBarsLastWeeksKitchenOnlyBackToBack(t *testing.T) {
	r := rotationFor(true)
	dubai, _ := time.LoadLocation("Asia/Dubai")
	start, notFirst, err := renewalWindow(r, time.Date(2026, 3, 26, 10, 0, 0, 0, dubai), dubai)
	if err != nil {
		t.Fatal(err)
	}
	if start.Format("2006-01-02") != "2026-03-29" || notFirst != r.Weeks[3].BranchID {
		t.Errorf("back to back: start the day after and bar week 4's kitchen; got %s %v", start, notFirst)
	}
	start, notFirst, _ = renewalWindow(r, time.Date(2026, 4, 10, 10, 0, 0, 0, dubai), dubai)
	if start.Format("2006-01-02") != "2026-04-11" || notFirst != uuid.Nil {
		t.Errorf("lapsed: start tomorrow with no bar; got %s %v", start, notFirst)
	}
}
