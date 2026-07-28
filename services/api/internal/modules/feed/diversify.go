package feed

// Diversification.
//
// The SQL in §4.2 caps how many reels an author gets in a page. It cannot
// control where those reels land, because window functions rank rows and do not
// reorder the result. So a page can legitimately come back with an author's two
// reels adjacent, which reads to a user as "this feed is one laundry shop" — a
// perception problem that no amount of correct filtering fixes.
//
// Doing the spacing in Go rather than in SQL is deliberate. Expressing
// "no two adjacent from the same author" in SQL means a recursive CTE or a
// window trick that nobody on the team will be able to modify in six months.
// Here it is a loop.

// Spread reorders items so no two consecutive entries share an author, while
// preserving score order as far as that constraint allows.
//
// The algorithm is greedy: walk the score-ordered list, and whenever the next
// item repeats the previous author, look ahead for the best-scoring item that
// does not, and take that instead. Deferred items are not dropped — they are
// re-offered on the next pass, so the output is always a permutation of the
// input and never a filter.
//
// When every remaining item shares one author, it gives up and emits them in
// order. A feed of one author's reels is a data problem (one vendor in range),
// not something reordering can solve, and returning fewer items to preserve the
// invariant would be worse.
func Spread[T any](items []T, authorOf func(T) string) []T {
	if len(items) < 3 {
		return items
	}

	out := make([]T, 0, len(items))
	remaining := make([]T, len(items))
	copy(remaining, items)

	lastAuthor := ""
	for len(remaining) > 0 {
		pick := 0
		if authorOf(remaining[0]) == lastAuthor {
			pick = -1
			for i := 1; i < len(remaining); i++ {
				if authorOf(remaining[i]) != lastAuthor {
					pick = i
					break
				}
			}
			if pick == -1 {
				// Everything left is the same author. Emit in order.
				pick = 0
			}
		}

		out = append(out, remaining[pick])
		lastAuthor = authorOf(remaining[pick])
		remaining = append(remaining[:pick], remaining[pick+1:]...)
	}
	return out
}
