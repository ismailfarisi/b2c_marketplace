package classifieds

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/modules/ledger"
	"github.com/marketplace/api/internal/money"
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool  *pgxpool.Pool
	nowFn func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, nowFn: time.Now}
}

type Package struct {
	ID           uuid.UUID
	Code         string
	Name         string
	NameAr       string
	Audience     string
	ListingQuota int
	DurationDays int
	FeaturedDays int
	BumpQuota    int
	PriceMinor   int64
	Currency     string
	TaxRateBP    int
	IsRecurring  bool
}

func (s *Service) Packages(ctx context.Context, audience string) ([]Package, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, name, COALESCE(name_ar, ''), audience,
		       listing_quota, duration_days, featured_days, bump_quota,
		       price_minor, currency, tax_rate_bp, is_recurring
		  FROM listing_packages
		 WHERE is_active AND ($1 = '' OR audience = $1)
		 ORDER BY sort_order, price_minor`, audience)
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}
	defer rows.Close()

	var out []Package
	for rows.Next() {
		var p Package
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.NameAr, &p.Audience,
			&p.ListingQuota, &p.DurationDays, &p.FeaturedDays, &p.BumpQuota,
			&p.PriceMinor, &p.Currency, &p.TaxRateBP, &p.IsRecurring); err != nil {
			return nil, fmt.Errorf("scan package: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type Purchase struct {
	ID          uuid.UUID
	Status      string
	AmountMinor int64
	TaxMinor    int64
	Currency    string
	StartsAt    *time.Time
	EndsAt      *time.Time
}

// Buy creates a purchase awaiting payment.
//
// Free packages activate immediately and post nothing to the ledger. That is
// not a special case bolted on — a zero-value journal is unbalanced by
// definition and the ledger would rightly refuse it. The free tier exists
// because supply is the binding constraint in a cold-start classifieds market
// (see the seed in migration 000016), so this path is the common one at launch,
// not the exception.
func (s *Service) Buy(ctx context.Context, buyerID, packageID uuid.UUID) (*Purchase, error) {
	var p Package
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, audience, listing_quota, duration_days, featured_days,
		       bump_quota, price_minor, currency, tax_rate_bp
		  FROM listing_packages WHERE id = $1 AND is_active`, packageID,
	).Scan(&p.ID, &p.Code, &p.Audience, &p.ListingQuota, &p.DurationDays,
		&p.FeaturedDays, &p.BumpQuota, &p.PriceMinor, &p.Currency, &p.TaxRateBP)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load package: %w", err)
	}

	dealerID, err := s.dealerFor(ctx, buyerID)
	if err != nil {
		return nil, err
	}
	if p.Audience == "dealer" && !dealerID.Valid {
		return nil, fmt.Errorf("%w: dealer packages require a verified dealer profile", httpx.ErrForbidden)
	}

	// The listed price is tax-inclusive, which is what a UAE consumer price has
	// to be. Extracting rather than adding means the seller pays the number on
	// the pricing page — the alternative is a checkout total that does not match
	// the advert, which is both a support ticket and a consumer-protection
	// problem.
	gross := money.New(p.PriceMinor, p.Currency)
	tax := money.ExtractTax(gross, p.TaxRateBP)
	net := gross.Minor - tax.Minor

	out := Purchase{AmountMinor: net, TaxMinor: tax.Minor, Currency: p.Currency}
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		status := "pending_payment"
		if p.PriceMinor == 0 {
			status = "active"
		}

		err := tx.QueryRow(ctx, `
			INSERT INTO package_purchases (
				buyer_id, dealer_id, package_id,
				amount_minor, tax_minor, currency,
				listing_quota, bump_quota, duration_days, featured_days,
				status, starts_at, ends_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			        CASE WHEN $11 = 'active' THEN now() END,
			        CASE WHEN $11 = 'active' THEN now() + make_interval(days => $9) END)
			RETURNING id, status, starts_at, ends_at`,
			buyerID, dealerID, p.ID, net, tax.Minor, p.Currency,
			p.ListingQuota, p.BumpQuota, p.DurationDays, p.FeaturedDays, status,
		).Scan(&out.ID, &out.Status, &out.StartsAt, &out.EndsAt)
		if err != nil {
			return fmt.Errorf("insert purchase: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Activate is called when the payment succeeds.
//
// The ledger posting lives here rather than in the payments module for the same
// reason PostOrderCaptured does not: the module that knows what was sold decides
// which accounts move. Payments knows an amount and a provider reference; it
// does not know that this particular amount is unearned for the next 30 days.
func (s *Service) Activate(ctx context.Context, purchaseID uuid.UUID) error {
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var (
			buyerID              uuid.UUID
			net, tax             int64
			currency             string
			durationDays, status = 0, ""
		)
		err := tx.QueryRow(ctx, `
			SELECT buyer_id, amount_minor, tax_minor, currency, duration_days, status
			  FROM package_purchases WHERE id = $1 FOR UPDATE`, purchaseID,
		).Scan(&buyerID, &net, &tax, &currency, &durationDays, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load purchase: %w", err)
		}
		if status != "pending_payment" {
			// Idempotent rather than an error: a gateway that delivers the same
			// webhook twice is normal, and the second delivery must not fail
			// loudly enough to make the provider retry it forever.
			return nil
		}

		if _, err := tx.Exec(ctx, `
			UPDATE package_purchases
			   SET status = 'active', starts_at = now(),
			       ends_at = now() + make_interval(days => duration_days)
			 WHERE id = $1`, purchaseID); err != nil {
			return fmt.Errorf("activate purchase: %w", err)
		}

		if net <= 0 {
			return nil
		}
		_, err = ledger.PostListingFeeCharged(ctx, tx, ledger.ListingFeeCharge{
			PurchaseID: purchaseID,
			BuyerID:    buyerID,
			Currency:   currency,
			NetMinor:   net,
			TaxMinor:   tax,
		})
		return err
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Quota — satisfies vehicles.Entitlements
// ─────────────────────────────────────────────────────────────────────────────

// CheckListingQuota is the seller-facing question, asked before any work.
func (s *Service) CheckListingQuota(ctx context.Context, sellerID uuid.UUID) error {
	ents, err := s.wallet(ctx, s.pool, sellerID)
	if err != nil {
		return err
	}
	if _, ok := SelectForListing(ents, s.nowFn()); !ok {
		return fmt.Errorf("%w: no listing credit — buy a package to publish", httpx.ErrConflict)
	}
	return nil
}

// ClaimFreeListing gives a seller with nothing the zero-price package, if one
// exists for their audience.
//
// # Why this is not a shop
//
// The catalogue already contains a package called "Free listing", priced at
// zero, quota one, audience private. That is a product decision that has already
// been made: a private seller's first listing costs nothing. What was missing is
// anyone claiming it — the entitlement wallet starts empty, so the very first
// publish a new seller ever attempts fails with "no listing credit, buy a
// package", and the app has no store to send them to.
//
// Sending them to one would be the wrong fix. A checkout for a free thing is a
// step that exists only to be completed, on the screen where a marketplace can
// least afford one, in a flow whose entire premise is publishing in under ninety
// seconds.
//
// The bounds are deliberately narrow. Only a package priced at zero, only when
// the seller has no usable credit at all, and only their own audience — a dealer
// does not get a private seller's freebie, and nobody accumulates free credits
// by tapping Publish twice, because the second call finds the first one in the
// wallet.
//
// Returns false when there is nothing to claim, which is a normal answer and not
// an error: a deployment can simply have no free tier.
func (s *Service) ClaimFreeListing(ctx context.Context, sellerID uuid.UUID) (bool, error) {
	ents, err := s.wallet(ctx, s.pool, sellerID)
	if err != nil {
		return false, err
	}
	if _, ok := SelectForListing(ents, s.nowFn()); ok {
		return false, nil
	}

	dealerID, err := s.dealerFor(ctx, sellerID)
	if err != nil {
		return false, err
	}
	audience := "private"
	if dealerID.Valid {
		audience = "dealer"
	}

	var packageID uuid.UUID
	err = s.pool.QueryRow(ctx, `
		SELECT id FROM listing_packages
		 WHERE is_active AND price_minor = 0 AND audience = $1 AND listing_quota > 0
		 ORDER BY listing_quota, created_at
		 LIMIT 1`, audience).Scan(&packageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find free package: %w", err)
	}

	// Through Buy rather than by inserting an entitlement directly, so a free
	// claim produces the same purchase row, the same audit trail and the same
	// revenue recognition path as a paid one. A second insert site for the same
	// concept is how the two drift.
	if _, err := s.Buy(ctx, sellerID, packageID); err != nil {
		return false, err
	}
	return true, nil
}

// ConsumeListing spends one credit, inside the caller's transaction.
//
// The UPDATE re-checks the quota in its WHERE clause rather than trusting the
// value SelectForListing read. Two listings approved concurrently would
// otherwise both see one credit remaining and both spend it; the CHECK on the
// table would catch the overspend, but as a constraint violation with no
// explanation rather than as this error.
func (s *Service) ConsumeListing(ctx context.Context, tx pgx.Tx, sellerID uuid.UUID) error {
	ents, err := s.wallet(ctx, tx, sellerID)
	if err != nil {
		return err
	}
	chosen, ok := SelectForListing(ents, s.nowFn())
	if !ok {
		return fmt.Errorf("%w: no listing credit available", httpx.ErrConflict)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE package_purchases
		   SET listings_used = listings_used + 1,
		       status = CASE WHEN listings_used + 1 >= listing_quota AND bumps_used >= bump_quota
		                     THEN 'exhausted' ELSE status END
		 WHERE id = $1 AND status = 'active' AND listings_used < listing_quota`,
		chosen.ID)
	if err != nil {
		return fmt.Errorf("consume listing credit: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: listing credit was spent concurrently", httpx.ErrConflict)
	}
	return nil
}

// ConsumeBump spends a bump and stamps the listing.
//
// A bump moves the listing to the top of "newest", which is exactly what the
// seller bought — so it writes `bumped_at` on the listing rather than a flag
// the sort has to know about separately. vehicles.orderBy reads GREATEST of the
// two timestamps for that reason.
func (s *Service) ConsumeBump(ctx context.Context, sellerID, listingID uuid.UUID) error {
	now := s.nowFn()
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var owner uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT seller_id FROM vehicle_listings WHERE id = $1 AND status = 'active' FOR UPDATE`,
			listingID).Scan(&owner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: only an active listing can be bumped", httpx.ErrConflict)
			}
			return fmt.Errorf("load listing: %w", err)
		}
		if owner != sellerID {
			return httpx.ErrForbidden
		}

		ents, err := s.wallet(ctx, tx, sellerID)
		if err != nil {
			return err
		}
		chosen, ok := SelectForBump(ents, now)
		if !ok {
			return fmt.Errorf("%w: no bumps left on any active package", httpx.ErrConflict)
		}

		tag, err := tx.Exec(ctx, `
			UPDATE package_purchases
			   SET bumps_used = bumps_used + 1
			 WHERE id = $1 AND status = 'active' AND bumps_used < bump_quota`, chosen.ID)
		if err != nil {
			return fmt.Errorf("consume bump: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: bump was spent concurrently", httpx.ErrConflict)
		}

		if _, err := tx.Exec(ctx,
			`UPDATE vehicle_listings SET bumped_at = now() WHERE id = $1`, listingID); err != nil {
			return fmt.Errorf("bump listing: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO listing_promotions (listing_id, purchase_id, kind, ends_at, created_by)
			VALUES ($1, $2, 'bump', now(), $3)`, listingID, chosen.ID, sellerID); err != nil {
			return fmt.Errorf("record bump: %w", err)
		}
		return nil
	})
}

// Feature buys the listing paid placement for the package's featured days.
func (s *Service) Feature(ctx context.Context, sellerID, listingID uuid.UUID) error {
	now := s.nowFn()
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var owner uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT seller_id FROM vehicle_listings WHERE id = $1 AND status = 'active' FOR UPDATE`,
			listingID).Scan(&owner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: only an active listing can be featured", httpx.ErrConflict)
			}
			return fmt.Errorf("load listing: %w", err)
		}
		if owner != sellerID {
			return httpx.ErrForbidden
		}

		ents, err := s.wallet(ctx, tx, sellerID)
		if err != nil {
			return err
		}
		var chosen Entitlement
		found := false
		for _, e := range ents {
			if e.FeaturedDays > 0 && e.live(now) {
				if !found || e.EndsAt.Before(chosen.EndsAt) {
					chosen, found = e, true
				}
			}
		}
		if !found {
			return fmt.Errorf("%w: no package with featured days", httpx.ErrConflict)
		}

		until := FeaturedUntil(chosen, now)
		if _, err := tx.Exec(ctx,
			`UPDATE vehicle_listings SET featured_until = GREATEST(COALESCE(featured_until, $2), $2)
			  WHERE id = $1`, listingID, until); err != nil {
			return fmt.Errorf("feature listing: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO listing_promotions (listing_id, purchase_id, kind, ends_at, created_by)
			VALUES ($1, $2, 'feature', $3, $4)`, listingID, chosen.ID, until, sellerID); err != nil {
			return fmt.Errorf("record feature: %w", err)
		}
		return nil
	})
}

// wallet loads a seller's spendable purchases.
//
// Takes a querier rather than a pool so the read can happen inside the caller's
// transaction, which is what makes ConsumeListing's re-check meaningful: reading
// the wallet outside the transaction that spends from it is the classic
// time-of-check-to-time-of-use gap.
func (s *Service) wallet(ctx context.Context, q querier, sellerID uuid.UUID) ([]Entitlement, error) {
	rows, err := q.Query(ctx, `
		SELECT id, listing_quota, listings_used, bump_quota, bumps_used,
		       featured_days, duration_days, amount_minor, recognised_minor,
		       starts_at, ends_at
		  FROM package_purchases
		 WHERE buyer_id = $1 AND status = 'active'
		   AND starts_at IS NOT NULL AND ends_at IS NOT NULL
		 ORDER BY ends_at`, sellerID)
	if err != nil {
		return nil, fmt.Errorf("load wallet: %w", err)
	}
	defer rows.Close()

	var out []Entitlement
	for rows.Next() {
		var e Entitlement
		if err := rows.Scan(&e.ID, &e.ListingQuota, &e.ListingsUsed, &e.BumpQuota,
			&e.BumpsUsed, &e.FeaturedDays, &e.DurationDays, &e.AmountMinor,
			&e.RecognisedMinor, &e.StartsAt, &e.EndsAt); err != nil {
			return nil, fmt.Errorf("scan entitlement: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// Recognition
// ─────────────────────────────────────────────────────────────────────────────

// RecogniseDue posts the earned portion of every live package.
//
// One transaction per purchase. A batch that recognises four hundred packages
// in one transaction and fails on the last one has posted nothing, and the
// retry re-reads the same four hundred — whereas per-purchase transactions make
// the job resumable by construction.
//
// `recognised_minor` is re-read FOR UPDATE inside each transaction rather than
// trusted from the outer query. Two overlapping runs of this job would
// otherwise both compute the same step and post it twice, which is a revenue
// overstatement that reconciles to nothing.
func (s *Service) RecogniseDue(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM package_purchases
		 WHERE status IN ('active', 'exhausted')
		   AND amount_minor > recognised_minor
		   AND starts_at IS NOT NULL
		 ORDER BY starts_at
		 LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("find recognisable purchases: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	posted := 0
	for _, id := range ids {
		n, err := s.recogniseOne(ctx, id)
		if err != nil {
			return posted, err
		}
		if n > 0 {
			posted++
		}
	}
	return posted, nil
}

func (s *Service) recogniseOne(ctx context.Context, purchaseID uuid.UUID) (int64, error) {
	var step int64
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		var e Entitlement
		var currency string
		if err := tx.QueryRow(ctx, `
			SELECT id, duration_days, amount_minor, recognised_minor, starts_at, ends_at, currency
			  FROM package_purchases WHERE id = $1 FOR UPDATE`, purchaseID,
		).Scan(&e.ID, &e.DurationDays, &e.AmountMinor, &e.RecognisedMinor,
			&e.StartsAt, &e.EndsAt, &currency); err != nil {
			return fmt.Errorf("load purchase for recognition: %w", err)
		}

		step = RecogniseNow(e, s.nowFn())
		if step <= 0 {
			return nil
		}

		if _, err := tx.Exec(ctx, `
			UPDATE package_purchases SET recognised_minor = recognised_minor + $2
			 WHERE id = $1`, purchaseID, step); err != nil {
			return fmt.Errorf("record recognition: %w", err)
		}
		_, err := ledger.PostListingFeeRecognised(ctx, tx, ledger.ListingFeeRecognition{
			PurchaseID:  purchaseID,
			Currency:    currency,
			AmountMinor: step,
		})
		return err
	})
	return step, err
}

// ExpireDue closes out packages whose window has ended.
//
// It recognises the remainder first. A package that expires with money left in
// deferred revenue leaves that money there permanently — the entitlement is
// gone, so no future recognition step will ever clear it, and the account stops
// reconciling. Whether the seller used their credits is irrelevant: they bought
// thirty days of availability and they got thirty days.
func (s *Service) ExpireDue(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM package_purchases
		 WHERE status IN ('active', 'exhausted') AND ends_at IS NOT NULL AND ends_at <= now()
		 ORDER BY ends_at LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("find expired purchases: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	for _, id := range ids {
		if _, err := s.recogniseOne(ctx, id); err != nil {
			return 0, err
		}
		if _, err := s.pool.Exec(ctx,
			`UPDATE package_purchases SET status = 'expired' WHERE id = $1`, id); err != nil {
			return 0, fmt.Errorf("expire purchase: %w", err)
		}
	}
	return len(ids), nil
}

func (s *Service) dealerFor(ctx context.Context, userID uuid.UUID) (uuid.NullUUID, error) {
	var id uuid.NullUUID
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM dealer_profiles WHERE owner_user_id = $1 AND status = 'active'`,
		userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.NullUUID{}, nil
	}
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("load dealer profile: %w", err)
	}
	return id, nil
}
