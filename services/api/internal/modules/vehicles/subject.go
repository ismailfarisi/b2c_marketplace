package vehicles

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

// SubjectAuthorizer answers "may this user post a reel about this thing".
//
// It is the entire interface the media pipeline gained in Phase 6, and the
// shape is the point: reels learns that some subjects have owners, not that
// some subjects are cars. Phase 2 wrote `author_type IN ('vendor_branch',
// 'user')` into the schema and left the user branch unimplemented; this is what
// implements it without the media module importing a vertical.
//
// A vendor-branch reel authorises through branch membership, which reels
// already knew how to do. A user reel cannot: a person is not a member of
// anything, so authority has to come from the *subject*. Owning the listing is
// what earns the right to post a video about it.
type SubjectAuthorizer struct {
	pool *pgxpool.Pool
}

func NewSubjectAuthorizer(pool *pgxpool.Pool) *SubjectAuthorizer {
	return &SubjectAuthorizer{pool: pool}
}

// AuthorizeSubject reports whether this user may author a reel about this
// subject.
//
// Refuses every subject type it does not own, including nil. That default is
// deliberate: the failure it prevents is general UGC arriving through a door
// that was opened for seller reels. PLAN-VIDEO-FIRST §8 has no phase for
// user-authored content that is not attached to a listing, and content with no
// subject also has no coverage geometry (migration 000016 §9) — so it would be
// published to nobody while still costing a moderation review.
func (a *SubjectAuthorizer) AuthorizeSubject(ctx context.Context, userID uuid.UUID,
	subjectType string, subjectID uuid.UUID) error {

	if subjectType != "vehicle_listing" {
		return fmt.Errorf("%w: a personal reel must be about a listing you own", httpx.ErrForbidden)
	}

	var sellerID uuid.UUID
	var status string
	err := a.pool.QueryRow(ctx,
		`SELECT seller_id, status FROM vehicle_listings WHERE id = $1`, subjectID).
		Scan(&sellerID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("authorize listing subject: %w", err)
	}
	if sellerID != userID {
		return httpx.ErrForbidden
	}
	// Removed and rejected listings cannot gain new content. A reel about a
	// listing that was taken down for fraud would otherwise re-enter the feed
	// carrying the same claims that got the listing removed.
	switch Status(status) {
	case StatusRemoved, StatusRejected, StatusExpired:
		return fmt.Errorf("%w: this listing is %s", httpx.ErrConflict, status)
	}
	return nil
}
