// Package feed serves the product's front door.
//
// Phase 2 ranks chronologically, but the query is written in the
// candidate → filter → score → diversify shape from
// docs/PHASE-2-MEDIA-AND-FEED.md §4.2 so that Phase 4 replaces one expression
// rather than rewriting the endpoint. That is not speculative generality: the
// ranking change is scheduled, and the cost of the shape now is zero.
package feed

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Cursor is keyset pagination state.
//
// Keyset and never OFFSET. OFFSET re-scans and re-sorts every skipped row, so
// page 40 costs forty times page 1 — and worse, it double-serves and skips rows
// whenever anything is inserted between requests, which in a feed of freshly
// published reels is every request.
//
// Score is carried even though Phase 2's score is a pure function of
// PublishedAt. When Phase 4 swaps the expression, the cursor format does not
// change and clients holding a cursor across the deploy keep working.
type Cursor struct {
	Score       float64
	PublishedAt time.Time
	ID          uuid.UUID
}

// Encode produces an opaque string.
//
// Opaque by intent: a client that parses it will depend on it, and the tie-break
// field is going to change in Phase 4. Base64 is not security, it is a fence.
func (c Cursor) Encode() string {
	raw := fmt.Sprintf("%s|%s|%s",
		strconv.FormatFloat(c.Score, 'g', -1, 64),
		strconv.FormatInt(c.PublishedAt.UTC().UnixMilli(), 10),
		c.ID.String(),
	)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
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
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return Cursor{}, fmt.Errorf("feed: malformed cursor")
	}

	score, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("feed: malformed cursor score")
	}
	ms, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Cursor{}, fmt.Errorf("feed: malformed cursor timestamp")
	}
	id, err := uuid.Parse(parts[2])
	if err != nil {
		return Cursor{}, fmt.Errorf("feed: malformed cursor id")
	}

	return Cursor{
		Score:       score,
		PublishedAt: time.UnixMilli(ms).UTC(),
		ID:          id,
	}, nil
}

func (c Cursor) IsZero() bool { return c.ID == uuid.Nil }
