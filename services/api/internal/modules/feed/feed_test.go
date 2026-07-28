package feed

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCursorRoundTrip(t *testing.T) {
	want := Cursor{
		Score:       0.7391304347826086,
		PublishedAt: time.Date(2026, 7, 28, 9, 30, 15, 0, time.UTC),
		ID:          uuid.MustParse("11111111-2222-3333-4444-555555555555"),
	}

	got, err := DecodeCursor(want.Encode())
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("id = %s, want %s", got.ID, want.ID)
	}
	if !got.PublishedAt.Equal(want.PublishedAt) {
		t.Errorf("published_at = %s, want %s", got.PublishedAt, want.PublishedAt)
	}
	// Full float64 precision must survive: the cursor is a strict inequality,
	// and a rounded score either re-serves the boundary row or skips it.
	if got.Score != want.Score {
		t.Errorf("score = %v, want %v", got.Score, want.Score)
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
		"YWJj",                     // valid base64, wrong shape
		"MC41fG5vdC1hLW51bWJlcnx4", // right shape, unparseable parts
	} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Errorf("DecodeCursor(%q) accepted a malformed cursor", bad)
		}
	}
}

func TestCursorIsOpaque(t *testing.T) {
	// If it looks parseable, a client will parse it — and the tie-break field
	// changes when ranking lands in Phase 4.
	c := Cursor{Score: 1, PublishedAt: time.Now(), ID: uuid.New()}
	if strings.Contains(c.Encode(), "|") || strings.Contains(c.Encode(), "-") {
		t.Fatalf("cursor leaks its structure: %q", c.Encode())
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
