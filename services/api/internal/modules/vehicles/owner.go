package vehicles

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OwnerCheck answers whether a user is the seller behind a listing.
//
// Used by the trust-document endpoint to refuse a mulkiya attached to somebody
// else's car. A plain existence query rather than loading the listing: this
// runs on an authorisation path and has one question to answer.
type OwnerCheck struct{ pool *pgxpool.Pool }

func NewOwnerCheck(pool *pgxpool.Pool) *OwnerCheck { return &OwnerCheck{pool: pool} }

func (c *OwnerCheck) OwnedBy(ctx context.Context, subjectID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := c.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM vehicle_listings
			 WHERE id = $1 AND seller_id = $2 AND status <> 'removed')`,
		subjectID, userID,
	).Scan(&ok)
	return ok, err
}
