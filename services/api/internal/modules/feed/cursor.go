// Package feed serves the product's front door.
//
// Phase 2 ranked chronologically and did it in SQL, in the
// candidate → filter → score → diversify shape from
// docs/PHASE-2-MEDIA-AND-FEED.md §4.2, on the stated expectation that Phase 4
// would replace one expression.
//
// It did not turn out that way, and the reason is worth recording. A single
// scoring expression is fine in SQL. Eight terms with Bayesian smoothing, an
// author prior and a per-viewer affinity join is not — not because Postgres
// cannot compute it, but because nobody can unit-test it there, and an
// untestable ranker gets tuned by guessing. So Phase 4 split the endpoint in
// two: **retrieval stays in SQL** (what is eligible — a database question),
// **ranking moved to Go** (what is best — a modelling question, in
// internal/modules/ranking, with tests).
//
// That split has one visible consequence, and it is this file: score-based
// keyset pagination stopped working. Scores are computed per request from
// counters that move, so page 2's "score < cursor score" predicate would be
// comparing against a number that no longer exists.
package feed

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
)

// ExclusionCap bounds how many ids a cursor carries.
//
// 120 reels is six pages at the default size. Beyond that the encoded cursor
// starts to be a meaningful fraction of a URL, and a viewer who has scrolled
// six pages deep into one district's feed has exhausted it anyway.
const ExclusionCap = 120

// Cursor is feed pagination state.
//
// Not a keyset, and not an offset either.
//
// **Not a keyset**, because Phase 4 moved scoring out of SQL: a cursor holding
// `(score, id)` would ask the next page to compare against a score computed
// from counters that have since moved, and a reel's score genuinely changes
// between two requests thirty seconds apart. The predicate would silently skip
// and re-serve rows.
//
// **Not an offset**, because OFFSET re-scans everything it skips and, worse,
// double-serves and drops rows whenever anything is published between requests
// — which in a feed of freshly-published reels is every request.
//
// So the cursor carries the exclusion set explicitly. Redis holds the same
// information and is consulted first; the cursor is what makes pagination
// correct when Redis is absent or has evicted. Without it, a deployment with no
// Redis returns page 1 forever, which is a broken feed caused by an optional
// dependency — exactly the coupling SeenStore's noop fallback exists to avoid.
//
// Page is carried so exploration slot placement stays stable within a session:
// the slot seed is derived from (viewer, page), so a viewer who backgrounds the
// app and returns gets the same layout rather than a reshuffled one.
type Cursor struct {
	Page     int
	Excluded []uuid.UUID
}

func (c Cursor) IsZero() bool { return c.Page == 0 && len(c.Excluded) == 0 }

// Encode produces an opaque string.
//
// Binary rather than text: 120 UUIDs in canonical form is 4,300 characters
// before encoding, and 1,920 bytes after. A cursor that pushes a URL past a
// proxy's header limit fails as a truncated feed, which reads as a ranking bug.
//
// Opaque by intent. A client that can parse a cursor will parse it, and then
// depend on it, and then break when the format changes — which it already has
// once, between Phase 2 and Phase 4.
func (c Cursor) Encode() string {
	excluded := c.Excluded
	if len(excluded) > ExclusionCap {
		// Keep the most recent. The oldest exclusions are the ones the viewer
		// saw pages ago and are least likely to resurface at the top of a
		// re-ranked candidate set.
		excluded = excluded[len(excluded)-ExclusionCap:]
	}

	buf := make([]byte, 0, 4+len(excluded)*16)
	buf = binary.BigEndian.AppendUint32(buf, uint32(c.Page))
	for _, id := range excluded {
		buf = append(buf, id[:]...)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// DecodeCursor parses a cursor, rejecting anything malformed.
//
// A bad cursor is a client bug or a tampering attempt, and the right answer to
// both is an error. Silently restarting from the top would hide the bug and
// give the user a feed that mysteriously jumps back to the beginning.
func DecodeCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("feed: malformed cursor")
	}
	if len(raw) < 4 || (len(raw)-4)%16 != 0 {
		return Cursor{}, fmt.Errorf("feed: malformed cursor")
	}

	c := Cursor{Page: int(binary.BigEndian.Uint32(raw[:4]))}
	// A page number is a slot seed and a sanity bound, not an index into
	// anything — but an absurd one signals a tampered cursor, and accepting it
	// would mean honouring a 4-billion-page feed.
	if c.Page < 0 || c.Page > 10_000 {
		return Cursor{}, fmt.Errorf("feed: malformed cursor page")
	}

	n := (len(raw) - 4) / 16
	if n > ExclusionCap {
		return Cursor{}, fmt.Errorf("feed: cursor carries too many exclusions")
	}

	c.Excluded = make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		var id uuid.UUID
		copy(id[:], raw[4+i*16:4+(i+1)*16])
		c.Excluded = append(c.Excluded, id)
	}
	return c, nil
}

// Next builds the cursor for the following page.
func (c Cursor) Next(served []uuid.UUID) Cursor {
	next := Cursor{
		Page:     c.Page + 1,
		Excluded: append(append([]uuid.UUID{}, c.Excluded...), served...),
	}
	if len(next.Excluded) > ExclusionCap {
		next.Excluded = next.Excluded[len(next.Excluded)-ExclusionCap:]
	}
	return next
}
