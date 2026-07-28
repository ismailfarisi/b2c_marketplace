package feed

import (
	"testing"

	"github.com/google/uuid"
)

func organic(n int, exploreFrom int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{ID: uuid.New(), Score: float64(n - i)}
		if exploreFrom >= 0 && i >= exploreFrom {
			out[i].Explore = true
		}
	}
	return out
}

func ads(n int) []Item {
	out := make([]Item, n)
	for i := range out {
		out[i] = Item{ID: uuid.New(), DeliveryID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	}
	return out
}

func TestSponsoredItemsLandOnTheirSlots(t *testing.T) {
	slots := []int{2, 6, 10}
	page := Inject(organic(20, -1), ads(3), slots, 20)

	if len(page) != 20 {
		t.Fatalf("page is %d long, want 20", len(page))
	}
	for _, s := range slots {
		if !page[s].Sponsored {
			t.Errorf("position %d is not sponsored", s)
		}
	}
	sponsored := 0
	for _, it := range page {
		if it.Sponsored {
			sponsored++
		}
	}
	if sponsored != 3 {
		t.Fatalf("%d sponsored items in the page, want 3", sponsored)
	}
}

// The failure this whole file exists to prevent.
//
// PLAN-VIDEO-FIRST §3 reserves 10–15% of the feed for reels that have not
// earned their place. Exploration slots sort last, because they have no score
// to sort on — so a naive `items[:limit]` truncation after injection deletes
// exactly them. The supply engine quietly becomes a rent, and the effect shows
// up two quarters later as an empty feed rather than as a traceable bug.
func TestPaidSlotsDoNotComeOutOfTheExplorationSlice(t *testing.T) {
	// 20 organic, the last 3 exploratory — roughly the 15% §3 asks for.
	items := organic(20, 17)
	before := 0
	for _, it := range items {
		if it.Explore {
			before++
		}
	}

	page := Inject(items, ads(3), []int{2, 6, 10}, 20)

	after := 0
	for _, it := range page {
		if it.Explore {
			after++
		}
	}
	if after != before {
		t.Fatalf("exploration slots went from %d to %d — advertising was funded by "+
			"deleting the reels that give new vendors a chance", before, after)
	}
}

func TestOnlyAnAllExplorationPageLosesExplorationSlots(t *testing.T) {
	items := organic(20, 0) // every item exploratory
	page := Inject(items, ads(3), []int{2, 6, 10}, 20)

	explore := 0
	for _, it := range page {
		if it.Explore {
			explore++
		}
	}
	// Three had to go; there was nothing else to drop.
	if explore != 17 {
		t.Fatalf("got %d exploratory items, want 17", explore)
	}
	if len(page) != 20 {
		t.Fatalf("page is %d long, want 20", len(page))
	}
}

// A sponsored reel was not chosen to discover whether it performs — it was
// chosen because someone paid. Letting the flag through would put paid
// impressions into the measurement of what exploration costs, which is the
// number that decides whether exploration survives its first budget review.
func TestASponsoredItemIsNeverAlsoAnExplorationSlot(t *testing.T) {
	advert := ads(1)
	advert[0].Explore = true

	page := Inject(organic(20, -1), advert, []int{2}, 20)
	if page[2].Explore {
		t.Fatal("a paid slot was counted as exploration")
	}
	if !page[2].Sponsored {
		t.Fatal("the paid slot lost its label")
	}
}

func TestOrganicOrderIsPreservedAroundTheSlots(t *testing.T) {
	items := organic(20, -1)
	page := Inject(items, ads(2), []int{2, 6}, 20)

	last := -1.0
	for i, it := range page {
		if it.Sponsored {
			continue
		}
		if last >= 0 && it.Score > last {
			t.Fatalf("organic order broke at position %d", i)
		}
		last = it.Score
	}
}

func TestNoSponsoredItemsMeansTheOrganicPage(t *testing.T) {
	items := organic(20, -1)
	page := Inject(items, nil, []int{2, 6, 10}, 20)
	if len(page) != 20 {
		t.Fatalf("got %d items", len(page))
	}
	for _, it := range page {
		if it.Sponsored {
			t.Fatal("nothing was sold, nothing should be labelled")
		}
	}
}

// Fewer winners than slots is the common case in a launch district with three
// advertisers. The page must still be full and the unsold slots must not
// become gaps.
func TestFewerWinnersThanSlotsStillFillsThePage(t *testing.T) {
	page := Inject(organic(20, -1), ads(1), []int{2, 6, 10}, 20)

	if len(page) != 20 {
		t.Fatalf("page is %d long, want 20", len(page))
	}
	sponsored := 0
	for _, it := range page {
		if it.Sponsored {
			sponsored++
		}
	}
	if sponsored != 1 {
		t.Fatalf("%d sponsored items, want 1", sponsored)
	}
	if !page[2].Sponsored {
		t.Fatal("the single winner should take the earliest slot")
	}
}

// A short page means the eligible set is exhausted, which PLAN-VIDEO-FIRST §10
// treats as a feature rather than a failure. Injection must not pad it back to
// full with advertising — a feed that runs out of local content and fills the
// remainder with ads is the exact product the wedge argues against.
func TestAShortOrganicPageStaysShort(t *testing.T) {
	page := Inject(organic(4, -1), ads(3), []int{2, 6, 10}, 20)

	if len(page) > 5 {
		t.Fatalf("4 organic items and 3 ads produced a page of %d — a feed that runs "+
			"out of local content must not fill the rest with advertising", len(page))
	}
}

func TestSlotsBeyondThePageAreIgnored(t *testing.T) {
	page := Inject(organic(6, -1), ads(3), []int{2, 6, 10}, 6)
	if len(page) != 6 {
		t.Fatalf("got %d items, want 6", len(page))
	}
	sponsored := 0
	for _, it := range page {
		if it.Sponsored {
			sponsored++
		}
	}
	if sponsored != 1 {
		t.Fatalf("%d sponsored items in a 6-item page, want 1 — slots 6 and 10 do "+
			"not exist", sponsored)
	}
}

func TestSponsoredSlotsMatchTheAdModulesLayout(t *testing.T) {
	// The feed duplicates boosts.SponsoredSlots rather than importing it, to
	// keep the seam test's guarantee intact. This asserts the two stay in step
	// on the shape that matters: never position 0, never adjacent, one in six.
	for size := 1; size <= 50; size++ {
		slots := sponsoredSlots(size)
		if len(slots)*6 > size {
			t.Fatalf("page of %d has %d slots", size, len(slots))
		}
		for i, s := range slots {
			if s < 2 {
				t.Fatalf("page of %d sells position %d", size, s)
			}
			if i > 0 && s-slots[i-1] < 4 {
				t.Fatalf("page of %d has adjacent slots", size)
			}
		}
	}
	if got := len(sponsoredSlots(20)); got != 3 {
		t.Fatalf("a 20-item page has %d slots, want 3 — this must match "+
			"boosts.SponsoredSlots", got)
	}
}
