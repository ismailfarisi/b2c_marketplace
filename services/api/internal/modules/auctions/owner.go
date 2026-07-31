package auctions

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OwnerCheck answers whether a user is the seller behind a lot.
//
// Used by the trust-document endpoint, and it matters more here than for a car:
// a plate transfer needs the current registration card, so a lot without one is
// a lot nobody should bid on — and attaching one to a stranger's lot would be
// vouching for a sale you have nothing to do with.
type OwnerCheck struct{ pool *pgxpool.Pool }

func NewOwnerCheck(pool *pgxpool.Pool) *OwnerCheck { return &OwnerCheck{pool: pool} }

func (c *OwnerCheck) OwnedBy(ctx context.Context, subjectID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := c.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM plate_auctions
			 WHERE id = $1 AND seller_id = $2 AND status <> 'cancelled')`,
		subjectID, userID,
	).Scan(&ok)
	return ok, err
}
