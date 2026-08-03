package property

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/platform/postgres"
)

type Service struct {
	pool  *pgxpool.Pool
	nowFn func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, nowFn: time.Now}
}

type Listing struct {
	ID             uuid.UUID
	ReferenceNo    string
	AdvertiserID   uuid.UUID
	AdvertiserKind string
	Status         Status
	Draft          Draft
	PublishedAt    *time.Time
	ExpiresAt      *time.Time
}

type CreateInput struct {
	AdvertiserID uuid.UUID
	Draft        Draft
}

// Create writes a draft advert.
//
// Drafts are free and need no permit — an agent gathering photos while DET
// processes the application is the normal sequence and it takes days. What a
// draft cannot do is be seen by anybody; `status = 'draft'` keeps it out of
// every read path, and the CHECK on the table keeps it out of `active`.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Listing, error) {
	now := s.nowFn()
	if issues := in.Draft.Validate(now); len(issues) > 0 {
		return nil, validationError(issues)
	}

	d := in.Draft
	currency := d.Currency
	if currency == "" {
		currency = "AED"
	}

	var id uuid.UUID
	var ref string
	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO property_listings (
				advertiser_id, advertiser_kind,
				permit_number, permit_issuer, permit_expires, agent_brn,
				purpose, property_type, bedrooms, bathrooms, size_sqft, furnishing,
				price_minor, currency, rent_period,
				title, description, emirate, community, tower, point, status)
			VALUES ($1, 'agent',
			        NULLIF($2, ''), NULLIF($3, ''), $4, NULLIF($5, ''),
			        $6, $7, $8, $9, $10, NULLIF($11, ''),
			        $12, $13, NULLIF($14, ''),
			        NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''),
			        NULLIF($18, ''), NULLIF($19, ''),
			        ST_SetSRID(ST_MakePoint($20, $21), 4326)::geography, 'draft')
			RETURNING id, reference_no`,
			in.AdvertiserID,
			d.PermitNumber, d.PermitIssuer, d.PermitExpiry, d.AgentBRN,
			d.Purpose, d.PropertyType, d.Bedrooms, d.Bathrooms, d.SizeSqft, d.Furnishing,
			d.PriceMinor, currency, d.RentPeriod,
			d.Title, d.Description, d.Emirate, d.Community, d.Tower,
			d.Lng, d.Lat,
		).Scan(&id, &ref)
		if err != nil {
			return fmt.Errorf("insert property listing: %w", err)
		}
		return s.recordStatus(ctx, tx, id, nil, StatusDraft, "advertiser", &in.AdvertiserID, "created")
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Submit sends an advert to the permit queue.
//
// This is the regulated gate and it is the reason this method is not a copy of
// vehicles.Submit. The permit must be present, well-formed and unexpired before
// a human ever sees the advert — not because the human could not check, but
// because a queue full of adverts missing an obvious field wastes the one
// reviewer whose time is the constraint on how fast this vertical can grow.
func (s *Service) Submit(ctx context.Context, advertiserID, listingID uuid.UUID) error {
	l, err := s.Get(ctx, listingID)
	if err != nil {
		return err
	}
	if l.AdvertiserID != advertiserID {
		return httpx.ErrForbidden
	}
	if !CanTransition(l.Status, StatusPendingReview) {
		return fmt.Errorf("%w: an advert in %s cannot be submitted", httpx.ErrConflict, l.Status)
	}
	if issues := l.Draft.ReadyToPublish(s.nowFn()); len(issues) > 0 {
		return validationError(issues)
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return s.transition(ctx, tx, listingID, l.Status, StatusPendingReview,
			"advertiser", &advertiserID, "submitted for permit review")
	})
}

// Approve is the moderator's decision and the only path to `active`.
//
// The permit is re-checked here, after a human has looked at it. That is not
// belt-and-braces for its own sake: submission and approval are minutes or days
// apart, and a permit that lapsed in between would otherwise go live expired.
func (s *Service) Approve(ctx context.Context, moderatorID, listingID uuid.UUID) error {
	l, err := s.Get(ctx, listingID)
	if err != nil {
		return err
	}
	if !CanTransition(l.Status, StatusActive) {
		return fmt.Errorf("%w: an advert in %s cannot be approved", httpx.ErrConflict, l.Status)
	}
	if issues := l.Draft.ReadyToPublish(s.nowFn()); len(issues) > 0 {
		return validationError(issues)
	}

	// The advert expires at whichever comes first: the standard term, or the
	// permit. A live advert under a lapsed permit is a compliance failure rather
	// than stale content, so the permit wins whenever it is sooner.
	expires := s.nowFn().Add(ListingTTL)
	if l.Draft.PermitExpiry != nil && l.Draft.PermitExpiry.Before(expires) {
		expires = *l.Draft.PermitExpiry
	}

	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE property_listings
			   SET status = 'active', published_at = COALESCE(published_at, now()),
			       expires_at = $2, rejection_reason = NULL
			 WHERE id = $1`, listingID, expires); err != nil {
			return fmt.Errorf("activate property listing: %w", err)
		}
		return s.recordStatus(ctx, tx, listingID, &l.Status, StatusActive,
			"moderator", &moderatorID, "permit verified")
	})
}

func (s *Service) Reject(ctx context.Context, moderatorID, listingID uuid.UUID, reason string) error {
	l, err := s.Get(ctx, listingID)
	if err != nil {
		return err
	}
	if !CanTransition(l.Status, StatusRejected) {
		return fmt.Errorf("%w: an advert in %s cannot be rejected", httpx.ErrConflict, l.Status)
	}
	if reason == "" {
		// Required, unlike most reason fields. A rejected property advert is an
		// advertiser who has paid a brokerage for a permit; "no" with no reason
		// is a support ticket and, if the rejection was wrong, a complaint to
		// the regulator about the platform.
		return &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "reason", Code: "required",
			Message: "tell the advertiser what to fix",
		}}}
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE property_listings SET status = 'rejected', rejection_reason = $2
			 WHERE id = $1`, listingID, reason); err != nil {
			return fmt.Errorf("reject property listing: %w", err)
		}
		return s.recordStatus(ctx, tx, listingID, &l.Status, StatusRejected,
			"moderator", &moderatorID, reason)
	})
}

// MarkLetOrSold is the advertiser closing their own advert.
//
// One-way, unlike a car being marked sold. A property that comes back on the
// market needs a new permit — the old one was issued against a deal that now
// exists — so it is a new advert rather than a reactivation. See the note on
// `transitions`.
func (s *Service) MarkLetOrSold(ctx context.Context, advertiserID, listingID uuid.UUID) error {
	l, err := s.Get(ctx, listingID)
	if err != nil {
		return err
	}
	if l.AdvertiserID != advertiserID {
		return httpx.ErrForbidden
	}
	if !CanTransition(l.Status, StatusLetOrSold) {
		return fmt.Errorf("%w: an advert in %s cannot be closed", httpx.ErrConflict, l.Status)
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE property_listings SET status = 'let_or_sold' WHERE id = $1`,
			listingID); err != nil {
			return fmt.Errorf("close property listing: %w", err)
		}
		return s.recordStatus(ctx, tx, listingID, &l.Status, StatusLetOrSold,
			"advertiser", &advertiserID, "closed by advertiser")
	})
}

// ExpireLapsedPermits is the sweep that keeps the platform compliant.
//
// It is the half of the obligation that is easy to forget: the permit check at
// publish is a moment, and the requirement is continuous. Without this, an
// advert approved in January under a permit that expired in March stays live
// and the platform is advertising without one — which is the platform's
// offence, not the advertiser's.
//
// Runs alongside the ordinary term expiry rather than instead of it, and is
// reported separately so the two do not look like one number.
func (s *Service) ExpireLapsedPermits(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE property_listings
		   SET status = 'expired'
		 WHERE status = 'active'
		   AND permit_expires IS NOT NULL
		   AND permit_expires <= now()`)
	if err != nil {
		return 0, fmt.Errorf("expire lapsed permits: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ExpireOverdue retires adverts that have simply run their term.
func (s *Service) ExpireOverdue(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE property_listings
		   SET status = 'expired'
		 WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("expire property listings: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ReviewQueue is the permit desk's worklist.
type PendingListing struct {
	ID           uuid.UUID
	ReferenceNo  string
	PermitNumber string
	PermitIssuer string
	PermitExpiry *time.Time
	AgentBRN     string
	Community    string
	PriceMinor   int64
	Currency     string
	SubmittedAt  time.Time
	// DuplicatePermit flags a number already live on another advert.
	//
	// Surfaced rather than blocked: a permit legitimately covers a relist after
	// expiry, so this is evidence for the reviewer and not a rule. What it
	// catches is a brokerage running one permit across several units to inflate
	// its apparent inventory.
	DuplicatePermit bool
}

func (s *Service) ReviewQueue(ctx context.Context, limit int) ([]PendingListing, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.id, p.reference_no,
		       COALESCE(p.permit_number, ''), COALESCE(p.permit_issuer, ''),
		       p.permit_expires, COALESCE(p.agent_brn, ''),
		       COALESCE(p.community, p.emirate, ''), p.price_minor, p.currency,
		       p.updated_at,
		       EXISTS (
		           SELECT 1 FROM property_listings other
		            WHERE other.permit_number = p.permit_number
		              AND other.id <> p.id
		              AND other.status = 'active'
		       )
		  FROM property_listings p
		 WHERE p.status = 'pending_review'
		 ORDER BY p.updated_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("property review queue: %w", err)
	}
	defer rows.Close()

	var out []PendingListing
	for rows.Next() {
		var p PendingListing
		if err := rows.Scan(&p.ID, &p.ReferenceNo, &p.PermitNumber, &p.PermitIssuer,
			&p.PermitExpiry, &p.AgentBRN, &p.Community, &p.PriceMinor, &p.Currency,
			&p.SubmittedAt, &p.DuplicatePermit); err != nil {
			return nil, fmt.Errorf("scan pending property: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Listing, error) {
	var l Listing
	var status string
	var lat, lng *float64
	err := s.pool.QueryRow(ctx, `
		SELECT id, reference_no, advertiser_id, advertiser_kind, status,
		       COALESCE(permit_number, ''), COALESCE(permit_issuer, ''),
		       permit_expires, COALESCE(agent_brn, ''),
		       purpose, property_type, bedrooms, bathrooms, size_sqft,
		       COALESCE(furnishing, ''), price_minor, currency,
		       COALESCE(rent_period, ''),
		       COALESCE(title, ''), COALESCE(description, ''),
		       COALESCE(emirate, ''), COALESCE(community, ''), COALESCE(tower, ''),
		       ST_Y(point::geometry), ST_X(point::geometry),
		       published_at, expires_at
		  FROM property_listings WHERE id = $1`, id,
	).Scan(&l.ID, &l.ReferenceNo, &l.AdvertiserID, &l.AdvertiserKind, &status,
		&l.Draft.PermitNumber, &l.Draft.PermitIssuer, &l.Draft.PermitExpiry, &l.Draft.AgentBRN,
		&l.Draft.Purpose, &l.Draft.PropertyType, &l.Draft.Bedrooms, &l.Draft.Bathrooms,
		&l.Draft.SizeSqft, &l.Draft.Furnishing, &l.Draft.PriceMinor, &l.Draft.Currency,
		&l.Draft.RentPeriod, &l.Draft.Title, &l.Draft.Description,
		&l.Draft.Emirate, &l.Draft.Community, &l.Draft.Tower,
		&lat, &lng, &l.PublishedAt, &l.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get property listing: %w", err)
	}
	l.Status = Status(status)
	if lat != nil {
		l.Draft.Lat = *lat
	}
	if lng != nil {
		l.Draft.Lng = *lng
	}
	return &l, nil
}

func (s *Service) transition(ctx context.Context, tx pgx.Tx, id uuid.UUID,
	from, to Status, actorType string, actorID *uuid.UUID, reason string) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: property listing cannot move %s → %s", httpx.ErrConflict, from, to)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE property_listings SET status = $2 WHERE id = $1 AND status = $3`,
		id, string(to), string(from)); err != nil {
		return fmt.Errorf("transition property listing: %w", err)
	}
	return s.recordStatus(ctx, tx, id, &from, to, actorType, actorID, reason)
}

func (s *Service) recordStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID,
	from *Status, to Status, actorType string, actorID *uuid.UUID, reason string) error {
	var fromStr any
	if from != nil {
		fromStr = string(*from)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO property_status_history (listing_id, from_state, to_state, actor_type, actor_id, reason)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
		id, fromStr, string(to), actorType, actorID, reason); err != nil {
		return fmt.Errorf("record property status: %w", err)
	}
	return nil
}

func validationError(issues []FieldIssue) error {
	fields := make([]httpx.FieldError, 0, len(issues))
	for _, i := range issues {
		fields = append(fields, httpx.FieldError{Field: i.Field, Code: i.Code, Message: i.Message})
	}
	return &httpx.ValidationError{Fields: fields}
}
