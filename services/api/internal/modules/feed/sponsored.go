package feed

// Paid slot injection (Phase 9).
//
// Two rules, and the second is the one that is easy to get wrong.
//
// **A sponsored reel takes a slot, not a rank.** It is placed at a fixed
// position rather than scored against organic candidates. Scoring them together
// would mean a high bid could out-rank a better reel anywhere in the page, and
// the share of the feed that is paid would become an emergent property of
// whoever is bidding today rather than a number anyone decided.
//
// **Paid slots never come out of the exploration slice.** PLAN-VIDEO-FIRST §3
// reserves 10–15% of the feed for reels that have not earned their place,
// because without it new vendors get no impressions and the supply side churns.
// Inserting three sponsored items into a full page pushes three organic items
// off the end — and the ones at the end are disproportionately the exploration
// slots, which sort last precisely because they have no score to sort on.
//
// So the truncation drops earned items before exploratory ones. The cost is
// borne by the reels that were going to be shown anyway.

// Inject places sponsored items at the given slot positions.
//
// `slots` must be ascending. Organic items fill everything else in order, and
// the result is exactly `limit` long when there is enough material.
func Inject(organic []Item, sponsored []Item, slots []int, limit int) []Item {
	if len(sponsored) == 0 || limit <= 0 {
		return truncate(organic, limit)
	}

	// Only slots that fall inside the page, and no more of them than there are
	// sponsored items to fill.
	usable := make([]int, 0, len(slots))
	for _, s := range slots {
		if s < limit && len(usable) < len(sponsored) {
			usable = append(usable, s)
		}
	}
	if len(usable) == 0 {
		return truncate(organic, limit)
	}

	room := limit - len(usable)
	organic = truncate(organic, room)

	out := make([]Item, 0, limit)
	nextSlot, nextSponsored, nextOrganic := 0, 0, 0

	for pos := 0; pos < limit; pos++ {
		if nextSlot < len(usable) && usable[nextSlot] == pos && nextSponsored < len(sponsored) {
			item := sponsored[nextSponsored]
			item.Sponsored = true
			// A sponsored reel is never also an exploration slot. It was not
			// chosen to discover whether it performs; it was chosen because
			// someone paid. Letting the flag through would put paid impressions
			// into the measurement of what exploration costs, which is the
			// number that decides whether exploration survives its first
			// budget review.
			item.Explore = false
			out = append(out, item)
			nextSlot++
			nextSponsored++
			continue
		}
		if nextOrganic >= len(organic) {
			break
		}
		out = append(out, organic[nextOrganic])
		nextOrganic++
	}

	return out
}

// truncate shortens a ranked list, dropping earned items before exploratory
// ones.
//
// The naive `items[:n]` drops from the tail, and the tail is where the
// exploration slots are — they sort last because they have no score to sort on.
// A page that made room for advertising by deleting exactly the reels that were
// there to give new vendors a chance would convert the supply engine into a
// rent, and the effect would show up two quarters later as an empty feed rather
// than as a bug anyone could trace back here.
func truncate(items []Item, n int) []Item {
	if n < 0 {
		n = 0
	}
	if len(items) <= n {
		return items
	}

	drop := len(items) - n
	keep := make([]bool, len(items))
	for i := range keep {
		keep[i] = true
	}

	// Walk backwards dropping earned items first.
	for i := len(items) - 1; i >= 0 && drop > 0; i-- {
		if !items[i].Explore {
			keep[i] = false
			drop--
		}
	}
	// Only if the page is all exploration does anything exploratory go.
	for i := len(items) - 1; i >= 0 && drop > 0; i-- {
		if keep[i] {
			keep[i] = false
			drop--
		}
	}

	out := make([]Item, 0, n)
	for i, k := range keep {
		if k {
			out = append(out, items[i])
		}
	}
	return out
}
