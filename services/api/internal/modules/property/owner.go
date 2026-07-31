package property

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OwnerCheck answers whether a user is the advertiser behind a property advert.
//
// The case this exists for is sharper than the vehicle one. Attaching a permit
// to a stranger's advert is not a nuisance — it is supplying the one thing that
// lets that advert go live, under a permit number the platform would then be
// publishing without the advertiser's involvement.
type OwnerCheck struct{ pool *pgxpool.Pool }

func NewOwnerCheck(pool *pgxpool.Pool) *OwnerCheck { return &OwnerCheck{pool: pool} }

func (c *OwnerCheck) OwnedBy(ctx context.Context, subjectID, userID uuid.UUID) (bool, error) {
	var ok bool
	err := c.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM property_listings
			 WHERE id = $1 AND advertiser_id = $2 AND status <> 'removed')`,
		subjectID, userID,
	).Scan(&ok)
	return ok, err
}
