package ranking

import (
	"encoding/json"
	"testing"
)

func TestExplorationSlotsAreStableForOneViewerAndPage(t *testing.T) {
	// A viewer who backgrounds the app and comes back must get the same page.
	// Re-randomising per request makes the feed feel like it lost your place.
	w := DefaultWeights()
	seed := Seed("viewer-abc", 0)

	first := Slots(20, seed, w)
	second := Slots(20, seed, w)

	if len(first) != len(second) {
		t.Fatalf("slot count changed between identical calls: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("slots are not stable: %v vs %v", first, second)
		}
	}
}

func TestExplorationSlotsDifferAcrossViewers(t *testing.T) {
	// If the exploration slot were always position 3, users would learn that
	// position 3 is where the filler goes and swipe past it — destroying the
	// measurement the slot exists to produce.
	w := DefaultWeights()

	layouts := map[string]bool{}
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		raw, _ := json.Marshal(Slots(20, Seed(key, 0), w))
		layouts[string(raw)] = true
	}
	if len(layouts) < 2 {
		t.Fatalf("every viewer got the same exploration layout: %v", layouts)
	}
}

func TestExplorationNeverTakesTheFirstSlot(t *testing.T) {
	// The first reel of a session decides whether there is a session. It is the
	// most expensive possible place to show something unproven.
	w := DefaultWeights()
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		for page := 0; page < 5; page++ {
			for _, s := range Slots(20, Seed(key, page), w) {
				if s == 0 {
					t.Fatalf("exploration took position 0 for %q page %d", key, page)
				}
			}
		}
	}
}

func TestExplorationShareIsRoughlyHonoured(t *testing.T) {
	w := DefaultWeights() // 15%
	got := len(Slots(20, Seed("viewer", 0), w))
	if got != 3 {
		t.Fatalf("15%% of a 20-item page reserved %d slots, want 3", got)
	}
}

func TestASmallPageStillExplores(t *testing.T) {
	// Round-to-nearest gives a 5-item page zero slots at 15%. A launch city has
	// a small catalogue by definition, so "explores never" would be the normal
	// case rather than the edge case.
	w := DefaultWeights()
	if got := len(Slots(5, Seed("viewer", 0), w)); got < 1 {
		t.Fatalf("a 5-item page reserved %d slots", got)
	}
}

func TestZeroShareDisablesExploration(t *testing.T) {
	w := DefaultWeights()
	w.ExplorationShare = 0
	if got := Slots(20, Seed("viewer", 0), w); len(got) != 0 {
		t.Fatalf("exploration_share=0 still reserved %v", got)
	}
}

func TestEligibilityUsesLifetimeImpressions(t *testing.T) {
	// Windowed impressions would make a reel eligible again every time the
	// window rolled past its launch — a permanent subsidy rather than a chance.
	w := DefaultWeights()

	newReel := Features{LifetimeImpressions: 10, Impressions: 10}
	oldReelQuietWindow := Features{LifetimeImpressions: 50_000, Impressions: 0}

	if !Eligible(newReel, w) {
		t.Error("a reel with 10 lifetime impressions was not eligible for exploration")
	}
	if Eligible(oldReelQuietWindow, w) {
		t.Error("a reel with 50,000 lifetime impressions became eligible again on a quiet window")
	}
}

func TestInterleaveKeepsThePageFull(t *testing.T) {
	ranked := []string{"r0", "r1", "r2", "r3", "r4", "r5", "r6", "r7"}
	explore := []string{"e0", "e1"}
	out := Interleave(ranked, explore, []int{2, 5}, 8)

	if len(out) != 8 {
		t.Fatalf("page length = %d, want 8", len(out))
	}
	if out[2] != "e0" || out[5] != "e1" {
		t.Fatalf("exploration items did not land in their slots: %v", out)
	}
}

func TestInterleaveFallsBackWhenThereIsNothingToExplore(t *testing.T) {
	// A launch district with no new supply must not produce a 19-item page. An
	// empty slot is a visible bug caused by a measurement policy.
	ranked := []string{"r0", "r1", "r2", "r3"}
	out := Interleave(ranked, nil, []int{1, 3}, 4)

	if len(out) != 4 {
		t.Fatalf("page length = %d, want 4", len(out))
	}
	for i, v := range ranked {
		if out[i] != v {
			t.Fatalf("ranked order was disturbed with no exploration pool: %v", out)
		}
	}
}

func TestInterleaveDropsNoRankedItemItHadRoomFor(t *testing.T) {
	ranked := []string{"r0", "r1", "r2"}
	explore := []string{"e0"}
	out := Interleave(ranked, explore, []int{1}, 4)

	seen := map[string]bool{}
	for _, v := range out {
		seen[v] = true
	}
	for _, v := range append(ranked, explore...) {
		if !seen[v] {
			t.Fatalf("%s was dropped from %v", v, out)
		}
	}
}

// --- weights -----------------------------------------------------------------

func TestDefaultWeightsAreValid(t *testing.T) {
	if err := DefaultWeights().Validate(); err != nil {
		t.Fatalf("the compiled-in fallback does not validate: %v", err)
	}
}

func TestInvalidProfilesAreRefused(t *testing.T) {
	// ranking_profiles is edited by hand by whoever is tuning this week. A
	// typo'd zero in prior_events hands the feed to whichever reel got lucky on
	// its third impression, and it must be refused loudly.
	cases := map[string]func(*Weights){
		"zero prior strength": func(w *Weights) { w.PriorEvents = 0 },
		"zero prior":          func(w *Weights) { w.PriorOrderRate = 0 },
		"prior above one":     func(w *Weights) { w.PriorClickRate = 1.5 },
		"negative half-life":  func(w *Weights) { w.RecencyHalfLifeHours = -1 },
		"majority explore":    func(w *Weights) { w.ExplorationShare = 0.9 },
	}
	for name, mutate := range cases {
		w := DefaultWeights()
		mutate(&w)
		if err := w.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestParseWeightsFillsMissingKeysFromDefaults(t *testing.T) {
	// A profile written before a new signal existed must keep working after the
	// deploy that adds it. Zeroing instead would set the new term's weight to
	// zero everywhere — a ranking change nobody made and nobody reviewed.
	w, err := ParseWeights([]byte(`{"conversion": 2.5}`))
	if err != nil {
		t.Fatal(err)
	}
	if w.Conversion != 2.5 {
		t.Errorf("conversion = %v, want 2.5", w.Conversion)
	}
	if w.PriorEvents != DefaultWeights().PriorEvents {
		t.Errorf("an unmentioned key was zeroed rather than defaulted: %v", w.PriorEvents)
	}
}

func TestParseWeightsFallsBackOnGarbage(t *testing.T) {
	for _, raw := range []string{`not json`, `{"prior_events": 0}`} {
		w, err := ParseWeights([]byte(raw))
		if err == nil {
			t.Errorf("ParseWeights(%q) reported success", raw)
		}
		if w != DefaultWeights() {
			t.Errorf("ParseWeights(%q) did not fall back to defaults", raw)
		}
	}
}
