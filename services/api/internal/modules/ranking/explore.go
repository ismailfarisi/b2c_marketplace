package ranking

import (
	"hash/fnv"
	"sort"
)

// Exploration.
//
// PLAN-VIDEO-FIRST §3: "Reserve 10–15% of the feed for exploration — without
// it, new vendors never get impressions and your supply side churns."
//
// That sentence describes a failure that is invisible from the demand side.
// Every engagement metric looks fine while it happens, because the reels being
// shown are the ones that already earned their place. What dies is supply: a
// vendor films five reels, gets forty impressions across all of them because
// they have no history to rank on, and stops filming. Six months later there is
// no content and no obvious moment where it went wrong.
//
// Exploration is the fix and it is a *cost*: those slots go to reels that are
// on average worse, and the page's measured engagement drops by roughly the
// share reserved. That is the price of having supply next quarter.

// Eligible reports whether a candidate has had a fair hearing yet.
//
// Lifetime impressions, not windowed. A rolling window would make a reel
// eligible for exploration again every time the window rolled past its launch,
// which turns a one-off subsidy into a permanent one.
func Eligible(f Features, w Weights) bool {
	return f.LifetimeImpressions < w.ExplorationImpressionFloor
}

// Slots picks which positions on a page are reserved for exploration.
//
// Two properties, both of which cost nothing and both of which matter:
//
//   - **Stable per viewer and page.** A viewer who backgrounds the app and
//     returns gets the same page rather than a reshuffled one. Re-randomising
//     on every request makes the feed feel like it lost your place.
//   - **Different across viewers.** If the exploration slot were always
//     position 3, users would learn that position 3 is where the filler goes and
//     swipe past it — which destroys the measurement the slot exists to produce.
//
// Never returns position 0. The first reel of a session is the one that decides
// whether there is a session, and spending it on an unproven reel is the most
// expensive possible place to explore.
func Slots(pageSize int, seed uint64, w Weights) []int {
	if pageSize <= 1 || w.ExplorationShare <= 0 {
		return nil
	}

	n := int(float64(pageSize)*w.ExplorationShare + 0.5)
	if n <= 0 {
		// Round-to-nearest would give a 5-item page zero slots at a 15% share.
		// One is the right answer: a share configured above zero must actually
		// explore, or a small-catalogue launch city — which is every launch
		// city — explores never.
		n = 1
	}
	if n > pageSize-1 {
		n = pageSize - 1
	}

	// Deterministic stride from the seed. A stride rather than n independent
	// draws because independent draws clump, and two exploration slots
	// side-by-side read as a dead patch in the feed.
	positions := make(map[int]bool, n)
	stride := pageSize / n
	if stride < 1 {
		stride = 1
	}
	offset := int(seed % uint64(stride))

	for i := 0; i < n; i++ {
		p := 1 + offset + i*stride
		if p >= pageSize {
			p = pageSize - 1
		}
		for positions[p] && p > 1 {
			p--
		}
		if p >= 1 && !positions[p] {
			positions[p] = true
		}
	}

	out := make([]int, 0, len(positions))
	for p := range positions {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// Seed derives the per-viewer, per-page slot seed.
//
// Hashed from the viewer key and the page index so that the same viewer on the
// same page gets the same layout, and two viewers do not get the same one.
func Seed(viewerKey string, page int) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(viewerKey))
	_, _ = h.Write([]byte{byte(page), byte(page >> 8)})
	return h.Sum64()
}

// Interleave places exploration candidates into the reserved positions.
//
// Returns a page of exactly min(pageSize, len(ranked)+len(explore)) items, and
// never drops a ranked item to make room: if the exploration pool is short, the
// reserved slots fall back to the ranked list. An empty slot would mean a
// 19-item page whenever the launch city had no new supply, which is a visible
// bug caused by a measurement policy.
func Interleave[T any](ranked, explore []T, slots []int, pageSize int) []T {
	if pageSize <= 0 {
		return nil
	}
	reserved := make(map[int]bool, len(slots))
	for _, s := range slots {
		reserved[s] = true
	}

	out := make([]T, 0, pageSize)
	ri, ei := 0, 0
	for pos := 0; pos < pageSize; pos++ {
		if reserved[pos] && ei < len(explore) {
			out = append(out, explore[ei])
			ei++
			continue
		}
		if ri < len(ranked) {
			out = append(out, ranked[ri])
			ri++
			continue
		}
		// Ranked list exhausted. Keep filling from exploration rather than
		// returning a short page — in a launch district with fifty reels this is
		// the normal case, not the edge case.
		if ei < len(explore) {
			out = append(out, explore[ei])
			ei++
			continue
		}
		break
	}
	return out
}
