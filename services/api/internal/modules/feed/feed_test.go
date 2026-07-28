package feed

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	want := Cursor{
		Page: 3,
		Excluded: []uuid.UUID{
			uuid.MustParse("11111111-2222-3333-4444-555555555555"),
			uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa"),
		},
	}

	got, err := DecodeCursor(want.Encode())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Page != want.Page {
		t.Errorf("page = %d, want %d", got.Page, want.Page)
	}
	if len(got.Excluded) != len(want.Excluded) {
		t.Fatalf("excluded = %v, want %v", got.Excluded, want.Excluded)
	}
	for i := range want.Excluded {
		if got.Excluded[i] != want.Excluded[i] {
			t.Errorf("excluded[%d] = %s, want %s", i, got.Excluded[i], want.Excluded[i])
		}
	}
}

func TestEmptyCursorIsZeroNotAnError(t *testing.T) {
	c, err := DecodeCursor("")
	if err != nil {
		t.Fatalf("empty cursor errored: %v", err)
	}
	if !c.IsZero() {
		t.Fatal("empty cursor did not decode to zero")
	}
}

func TestMalformedCursorIsRejected(t *testing.T) {
	// Silently restarting from the top would hide a client bug and give the
	// user a feed that mysteriously jumps back to the beginning.
	for _, bad := range []string{
		"not-base64!!",
		"YWJj",     // valid base64, too short for even a page number
		"AAAAAQAB", // page present, trailing bytes that are not a whole uuid
	} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Errorf("DecodeCursor(%q) accepted a malformed cursor", bad)
		}
	}
}

func TestCursorRefusesAnOversizedExclusionSet(t *testing.T) {
	// A cursor is client-supplied. Without the cap, anyone can hand the feed a
	// 100,000-element uuid array and have it passed straight into a
	// `NOT (id = ANY($5))` predicate on the hottest query in the product.
	oversized := Cursor{Page: 1, Excluded: make([]uuid.UUID, ExclusionCap+40)}
	for i := range oversized.Excluded {
		oversized.Excluded[i] = uuid.New()
	}

	// Encode trims, so a legitimately-produced cursor always decodes.
	got, err := DecodeCursor(oversized.Encode())
	if err != nil {
		t.Fatalf("a cursor this package produced was rejected: %v", err)
	}
	if len(got.Excluded) != ExclusionCap {
		t.Fatalf("excluded = %d ids, want the cap of %d", len(got.Excluded), ExclusionCap)
	}
}

func TestCursorIsOpaque(t *testing.T) {
	// If it looks parseable, a client will parse it — and then depend on it, and
	// then break when the format changes. It already has once, between Phase 2
	// and Phase 4.
	id := uuid.New()
	enc := Cursor{Page: 1, Excluded: []uuid.UUID{id}}.Encode()

	// The ids are packed as raw 16-byte values, so nothing in the output is a
	// readable uuid or a field separator a client could split on. (`-` and `_`
	// do appear — they are the base64url alphabet, not structure.)
	if strings.Contains(enc, id.String()) {
		t.Fatalf("cursor contains a readable uuid: %q", enc)
	}
	if strings.Contains(enc, "|") || strings.Contains(enc, ":") {
		t.Fatalf("cursor exposes a field separator: %q", enc)
	}
}

func TestNextCursorAdvancesAndAccumulates(t *testing.T) {
	first := Cursor{}
	pageOne := []uuid.UUID{uuid.New(), uuid.New()}
	second := first.Next(pageOne)

	if second.Page != 1 {
		t.Errorf("page = %d, want 1", second.Page)
	}
	if len(second.Excluded) != 2 {
		t.Fatalf("excluded = %d, want 2", len(second.Excluded))
	}

	third := second.Next([]uuid.UUID{uuid.New()})
	if third.Page != 2 || len(third.Excluded) != 3 {
		t.Fatalf("third page = %d with %d exclusions", third.Page, len(third.Excluded))
	}
}

func TestNextCursorKeepsTheMostRecentExclusions(t *testing.T) {
	// The oldest exclusions are the ones the viewer saw pages ago and are least
	// likely to resurface at the top of a re-ranked candidate set. Dropping the
	// newest instead would re-serve the page the viewer is currently looking at.
	c := Cursor{Excluded: make([]uuid.UUID, ExclusionCap)}
	for i := range c.Excluded {
		c.Excluded[i] = uuid.New()
	}
	newest := uuid.New()

	next := c.Next([]uuid.UUID{newest})
	if len(next.Excluded) != ExclusionCap {
		t.Fatalf("excluded = %d, want %d", len(next.Excluded), ExclusionCap)
	}
	if next.Excluded[len(next.Excluded)-1] != newest {
		t.Fatal("the just-served reel was evicted from the exclusion set")
	}
}

// --- diversification ---------------------------------------------------------

type item struct {
	id     string
	author string
}

func authorOf(i item) string { return i.author }

func TestSpreadSeparatesSameAuthor(t *testing.T) {
	// The SQL caps how many reels an author gets; it cannot control where they
	// land. Two adjacent reels from one vendor read to a user as "this feed is
	// one laundry shop".
	in := []item{
		{"a1", "vendorA"}, {"a2", "vendorA"},
		{"b1", "vendorB"}, {"c1", "vendorC"},
	}
	out := Spread(in, authorOf)

	if len(out) != len(in) {
		t.Fatalf("Spread dropped items: %d in, %d out", len(in), len(out))
	}
	for i := 1; i < len(out); i++ {
		if out[i].author == out[i-1].author {
			t.Fatalf("adjacent items from %s at %d: %+v", out[i].author, i, out)
		}
	}
}

func TestSpreadIsAPermutation(t *testing.T) {
	in := []item{
		{"a1", "A"}, {"a2", "A"}, {"a3", "A"},
		{"b1", "B"}, {"b2", "B"}, {"c1", "C"},
	}
	out := Spread(in, authorOf)

	seen := map[string]int{}
	for _, i := range out {
		seen[i.id]++
	}
	if len(seen) != len(in) {
		t.Fatalf("Spread is not a permutation: %v", seen)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appears %d times", id, n)
		}
	}
}

func TestSpreadPreservesScoreOrderWhereItCan(t *testing.T) {
	// Nothing to separate — the input must come back untouched, or the ranking
	// is being reordered for no reason.
	in := []item{{"1", "A"}, {"2", "B"}, {"3", "C"}, {"4", "D"}}
	out := Spread(in, authorOf)
	for i := range in {
		if out[i].id != in[i].id {
			t.Fatalf("reordered an already-diverse page: %+v", out)
		}
	}
}

func TestSpreadGivesUpGracefullyOnASingleAuthor(t *testing.T) {
	// One vendor in range is a data problem. Returning fewer items to preserve
	// the invariant would be worse than an undiversified page.
	in := []item{{"1", "A"}, {"2", "A"}, {"3", "A"}}
	out := Spread(in, authorOf)
	if len(out) != 3 {
		t.Fatalf("dropped items when only one author was available: %+v", out)
	}
}

func TestSpreadHandlesTinyPages(t *testing.T) {
	for _, in := range [][]item{nil, {{"1", "A"}}, {{"1", "A"}, {"2", "A"}}} {
		if got := Spread(in, authorOf); len(got) != len(in) {
			t.Fatalf("Spread(%v) changed length", in)
		}
	}
}

// --- retrieval-independent ranking assembly ----------------------------------

func TestCapPerAuthorKeepsTheBestOnesInOrder(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	items := []Item{
		{ID: uuid.New(), AuthorID: a, Score: 9},
		{ID: uuid.New(), AuthorID: a, Score: 8},
		{ID: uuid.New(), AuthorID: a, Score: 7},
		{ID: uuid.New(), AuthorID: b, Score: 6},
	}

	got := capPerAuthor(items, 2)
	if len(got) != 3 {
		t.Fatalf("kept %d items, want 3", len(got))
	}
	if got[0].Score != 9 || got[1].Score != 8 {
		t.Fatalf("kept the wrong two from author a: %v %v", got[0].Score, got[1].Score)
	}
}

func TestCapPerAuthorDoesNotMutateItsInput(t *testing.T) {
	// It is called on a slice the caller still holds, and an in-place filter
	// would silently truncate the exploration pool while ranking the main one.
	a := uuid.New()
	items := []Item{
		{ID: uuid.New(), AuthorID: a, Score: 3},
		{ID: uuid.New(), AuthorID: a, Score: 2},
	}
	original := items[1]

	capPerAuthor(items, 1)
	if items[1].ID != original.ID {
		t.Fatal("capPerAuthor wrote through its input slice")
	}
}

func TestUnionDeduplicatesTheTwoExclusionSources(t *testing.T) {
	// Redis and the cursor overlap by construction — the cursor is written from
	// the same page that was just marked seen. Passing duplicates into the SQL
	// array is harmless but wasteful, and the overlap is near-total.
	shared := uuid.New()
	redisSide := []uuid.UUID{shared, uuid.New()}
	cursorSide := []uuid.UUID{shared, uuid.New()}

	got := union(redisSide, cursorSide)
	if len(got) != 3 {
		t.Fatalf("union produced %d ids, want 3", len(got))
	}

	seen := map[uuid.UUID]int{}
	for _, id := range got {
		seen[id]++
	}
	if seen[shared] != 1 {
		t.Fatalf("the shared id appears %d times", seen[shared])
	}
}

func TestUnionOfNothingIsUsableAsASQLArray(t *testing.T) {
	// A nil slice binds as NULL rather than as an empty array, and
	// `NOT (id = ANY(NULL))` is NULL — which filters out every candidate and
	// returns an empty feed to every first-time viewer.
	if got := union(nil, nil); got == nil {
		t.Fatal("union returned nil rather than an empty slice")
	}
}

// ── the cold-start bug QA found ──────────────────────────────────────────────

func author(n int) uuid.UUID {
	return uuid.NewSHA1(uuid.Nil, []byte{byte(n)})
}

func exploreItem(id int, a int) Item {
	return Item{ID: uuid.NewSHA1(uuid.Nil, []byte{9, byte(id)}), AuthorID: author(a), Explore: true}
}

// A launch district has one vendor and almost everything in it is
// low-impression, so almost everything lands in the exploration bucket — which
// is capped at one item per author. The result was a **one-item feed** however
// many reels that vendor had published, and it appeared exactly when supply was
// thinnest, which is when the product can least afford it.
func TestAThinDistrictDoesNotServeAOneItemFeed(t *testing.T) {
	all := []Item{exploreItem(1, 1), exploreItem(2, 1), exploreItem(3, 1)}
	// What Interleave would have produced after capPerAuthor(explore, 1).
	page := []Item{all[0]}

	got := backfill(page, 20, nil, all)

	if len(got) != maxPerAuthor {
		t.Fatalf("backfilled to %d items, want %d — one vendor's page is capped at "+
			"maxPerAuthor, not at one", len(got), maxPerAuthor)
	}
}

// The cap that stops one author owning the page still holds. Backfill only ever
// tops up to `maxPerAuthor`; a vendor with nine reels does not get nine slots
// because they happen to be the only vendor in range.
func TestBackfillRespectsThePerAuthorCap(t *testing.T) {
	var all []Item
	for i := 0; i < 9; i++ {
		all = append(all, exploreItem(i, 1))
	}

	got := backfill([]Item{all[0]}, 20, all)

	if len(got) > maxPerAuthor {
		t.Fatalf("one author took %d slots, cap is %d", len(got), maxPerAuthor)
	}
}

func TestBackfillLeavesAFullPageAlone(t *testing.T) {
	page := []Item{exploreItem(1, 1), exploreItem(2, 2)}
	got := backfill(page, 2, []Item{exploreItem(3, 3)})

	if len(got) != 2 {
		t.Fatalf("a full page was extended to %d", len(got))
	}
}

func TestBackfillNeverDuplicates(t *testing.T) {
	a, b := exploreItem(1, 1), exploreItem(2, 2)
	got := backfill([]Item{a}, 20, []Item{a, b})

	seen := map[uuid.UUID]bool{}
	for _, it := range got {
		if seen[it.ID] {
			t.Fatalf("%s appears twice", it.ID)
		}
		seen[it.ID] = true
	}
}

// An item that arrives through backfill was still chosen without earning its
// place on score. Clearing the flag would put unearned impressions into the
// measurement of what exploration costs — the number that decides whether
// exploration survives its first budget review.
func TestBackfilledExplorationKeepsItsLabel(t *testing.T) {
	all := []Item{exploreItem(1, 1), exploreItem(2, 1)}
	got := backfill([]Item{all[0]}, 20, all)

	for _, it := range got {
		if !it.Explore {
			t.Fatal("an exploration item lost its label on the way through backfill")
		}
	}
}

// A feed that can be exhausted is the product promise (PLAN-VIDEO-FIRST §10).
// Backfill must not invent items to reach the limit.
func TestBackfillCannotExtendBeyondWhatExists(t *testing.T) {
	got := backfill([]Item{exploreItem(1, 1)}, 20, nil, nil)
	if len(got) != 1 {
		t.Fatalf("got %d items from an empty candidate set", len(got))
	}
}
