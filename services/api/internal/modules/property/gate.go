package property

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/seam/subject"
)

// PublishGate holds a reel back until its advert is legally publishable.
//
// This is the vertical where the gate matters most, and where its absence was
// closest to being a real problem rather than a cosmetic one. A property advert
// without a valid Trakheesi or DARI permit must not be published — the offence
// is committed by whoever publishes it, which is the platform. The permit is
// enforced in the service and again as a table CHECK, so the *advert* cannot
// reach `active` without one.
//
// The reel could, though. It travels through the media pipeline on its own
// timetable, and nothing in that pipeline had any idea a permit desk existed.
// A video walkthrough of a specific flat, captioned with its price, published
// to a geo-scoped feed, is an advertisement in every sense a regulator cares
// about — and it was reaching the feed while the permit was still being
// checked.
type PublishGate struct {
	pool *pgxpool.Pool
}

func NewPublishGate(pool *pgxpool.Pool) *PublishGate { return &PublishGate{pool: pool} }

func (g *PublishGate) Ready(ctx context.Context, id uuid.UUID) (subject.Readiness, string, error) {
	var (
		status        string
		permitNumber  *string
		permitExpires *time.Time
	)
	err := g.pool.QueryRow(ctx, `
		SELECT status, permit_number, permit_expires
		  FROM property_listings WHERE id = $1`, id,
	).Scan(&status, &permitNumber, &permitExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		// The advert was deleted while the video was transcoding. Nothing to
		// advertise, and nothing that will ever come back.
		return subject.ReadyNever, "This advert no longer exists.", nil
	}
	if err != nil {
		// An error is not a verdict. Returning ReadyNever on a dropped
		// connection would permanently fail a reel over a blip, and returning
		// ReadyNow would publish an unpermitted advert because the database was
		// briefly unavailable — which is the one outcome this type exists to
		// prevent. The caller holds the reel and asks again.
		return subject.ReadyLater, "", fmt.Errorf("property: publish gate: %w", err)
	}

	switch Status(status) {
	case StatusActive:
		// Re-checked here rather than trusted from approval. The advert may
		// have been approved in January under a permit that lapsed in March,
		// and ExpireLapsedPermits is a sweep rather than an instant.
		if PermitExpired(permitExpires, time.Now()) {
			return subject.ReadyNever,
				"The permit on this advert has expired. Renew it and submit again.", nil
		}
		if permitNumber == nil || *permitNumber == "" {
			// Unreachable while the CHECK constraint holds, and checked anyway.
			// The cost is one comparison; the cost of being wrong is publishing
			// an unpermitted property advert.
			return subject.ReadyNever, "This advert has no permit number.", nil
		}
		return subject.ReadyNow, "", nil

	case StatusDraft, StatusPendingReview:
		return subject.ReadyLater,
			"Your video is ready. It goes live when the permit check passes.", nil

	case StatusRejected:
		return subject.ReadyNever,
			"This advert was not approved, so the video was not published.", nil

	case StatusLetOrSold:
		return subject.ReadyNever, "This property is no longer available.", nil

	case StatusPaused:
		// Paused is the seller's own choice and reversible, so the reel waits
		// rather than failing. Unlike every other non-active status here, this
		// one has a person who can undo it in one tap.
		return subject.ReadyLater, "This advert is paused.", nil

	default:
		// Expired, removed, or a status added after this build. Refusing an
		// unknown status is the safe direction for a regulated vertical.
		return subject.ReadyNever, "This advert is not live.", nil
	}
}
