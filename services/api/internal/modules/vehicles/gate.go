package vehicles

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/subject"
)

// PublishGate holds a reel back until its listing is actually live.
//
// The sell flow submits the listing and the video at the same moment, and the
// two are reviewed by different queues on different timetables — a moderator
// judges the *video*, the classifieds desk judges the *listing*. Neither should
// wait on the other, which is right; but the reel was publishing regardless of
// what the listing desk decided.
//
// What that looked like in the app: a car in the feed with its CTA greyed out
// and "This listing is not live yet" underneath. The action seam behaving
// exactly as designed, over a reel that should not have been on screen. The
// seller's reel had spent its first and best impressions on viewers who could
// not act, and a rejected listing kept a video of the car in the feed
// indefinitely.
type PublishGate struct{ pool *pgxpool.Pool }

func NewPublishGate(pool *pgxpool.Pool) *PublishGate { return &PublishGate{pool: pool} }

func (g *PublishGate) Ready(ctx context.Context, id uuid.UUID) (subject.Readiness, string, error) {
	var status string
	err := g.pool.QueryRow(ctx,
		`SELECT status FROM vehicle_listings WHERE id = $1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return subject.ReadyNever, "This listing no longer exists.", nil
	}
	if err != nil {
		// Held, not failed. A dropped connection is not a decision about a
		// listing, and permanently failing a reel over one would be losing a
		// seller's work to a blip.
		return subject.ReadyLater, "", fmt.Errorf("vehicles: publish gate: %w", err)
	}

	switch Status(status) {
	case StatusActive:
		return subject.ReadyNow, "", nil

	case StatusDraft, StatusPendingReview:
		return subject.ReadyLater,
			"Your video is ready. It goes live when the listing is approved.", nil

	case StatusRejected:
		return subject.ReadyNever,
			"This listing was not approved, so the video was not published.", nil

	case StatusPaused:
		// The seller's own choice, and one tap to undo. Waiting is right where
		// failing would not be.
		return subject.ReadyLater, "This listing is paused.", nil

	case StatusSold:
		// A car sold between filming and publishing. Not a failure of anything,
		// but there is nothing left to advertise.
		return subject.ReadyNever, "This car was marked sold.", nil

	default:
		// Expired, removed, or a status this build predates.
		return subject.ReadyNever, "This listing is not live.", nil
	}
}
