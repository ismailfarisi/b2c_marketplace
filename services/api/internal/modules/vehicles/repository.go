package vehicles

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/httpx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Listing is the full record. Search returns a narrower shape — see Result.
type Listing struct {
	ID            uuid.UUID
	ReferenceNo   string
	SellerID      uuid.UUID
	SellerKind    string
	DealerID      uuid.NullUUID
	MakeID        uuid.UUID
	ModelID       uuid.UUID
	TrimID        uuid.NullUUID
	Year          int
	MileageKM     int
	RegionalSpec  string
	Transmission  string
	FuelType      string
	BodyType      string
	VIN           string
	PriceMinor    int64
	Currency      string
	IsNegotiable  bool
	Title         string
	Description   string
	Emirate       string
	City          string
	Lat, Lng      float64
	RadiusM       int
	Status        Status
	FeaturedUntil *time.Time
	PublishedAt   *time.Time
	ExpiresAt     *time.Time
	SoldAt        *time.Time
	CreatedAt     time.Time
}

type Result struct {
	ID           uuid.UUID
	ReferenceNo  string
	SellerID     uuid.UUID
	SellerKind   string
	DealerID     uuid.NullUUID
	MakeID       uuid.UUID
	ModelID      uuid.UUID
	TrimID       uuid.NullUUID
	Year         int
	MileageKM    int
	Transmission string
	FuelType     string
	BodyType     string
	RegionalSpec string
	Colour       string
	PriceMinor   int64
	Currency     string
	IsNegotiable bool
	Title        string
	Emirate      string
	City         string
	PublishedAt  *time.Time
	DistanceM    *float64
	Promoted     bool
}

// ResolveTaxonomy turns the slugs a client sends into ids.
//
// Slugs rather than uuids in the API because the taxonomy is licensed reference
// data that gets reloaded (§1 of the migration): a reload that changes the
// primary keys must not break every saved search and every deep link.
func (r *Repository) ResolveTaxonomy(ctx context.Context, makeSlug, modelSlug string) (uuid.UUID, uuid.UUID, error) {
	var makeID, modelID uuid.UUID
	err := r.pool.QueryRow(ctx, `
		SELECT mk.id, md.id
		  FROM vehicle_makes mk
		  JOIN vehicle_models md ON md.make_id = mk.id AND md.slug = $2
		 WHERE mk.slug = $1 AND mk.is_active AND md.is_active`,
		makeSlug, modelSlug,
	).Scan(&makeID, &modelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "model", Code: "unknown",
			Message: fmt.Sprintf("%s / %s is not in the vehicle taxonomy", makeSlug, modelSlug),
		}}}
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("resolve taxonomy: %w", err)
	}
	return makeID, modelID, nil
}

type CreateInput struct {
	SellerID   uuid.UUID
	SellerKind string
	DealerID   uuid.NullUUID
	MakeID     uuid.UUID
	ModelID    uuid.UUID
	Draft      Draft
}

// Insert writes the listing as a draft and stamps its first price-history row.
//
// The price history entry is written here rather than by a trigger so that the
// listing and its history are one statement apart inside one transaction. A
// trigger would work; it would also mean the only place the rule is written
// down is a place nobody greps.
func (r *Repository) Insert(ctx context.Context, tx pgx.Tx, in CreateInput) (uuid.UUID, string, error) {
	d := in.Draft
	radius := d.RadiusM
	if radius == 0 {
		radius = DefaultRadiusM
	}
	currency := d.Currency
	if currency == "" {
		currency = "AED"
	}
	vin := NormaliseVIN(d.VIN)

	var id uuid.UUID
	var ref string
	err := tx.QueryRow(ctx, `
		INSERT INTO vehicle_listings (
			reference_no, seller_id, seller_kind, dealer_id,
			make_id, model_id, year, mileage_km,
			transmission, fuel_type, body_type, regional_spec, vin,
			price_minor, currency, title, description,
			point, radius_m, status)
		VALUES (
			generate_listing_reference(), $1, $2, $3,
			$4, $5, $6, $7,
			NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), $11, NULLIF($12, ''),
			$13, $14, NULLIF($15, ''), NULLIF($16, ''),
			ST_SetSRID(ST_MakePoint($17, $18), 4326)::geography, $19, 'draft')
		RETURNING id, reference_no`,
		in.SellerID, in.SellerKind, in.DealerID,
		in.MakeID, in.ModelID, d.Year, d.MileageKM,
		d.Transmission, d.FuelType, d.BodyType, d.RegionalSpec, vin,
		d.PriceMinor, currency, d.Title, d.Description,
		d.Lng, d.Lat, radius,
	).Scan(&id, &ref)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("insert listing: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO listing_price_history (listing_id, price_minor, currency, changed_by)
		VALUES ($1, $2, $3, $4)`,
		id, d.PriceMinor, currency, in.SellerID,
	); err != nil {
		return uuid.Nil, "", fmt.Errorf("insert price history: %w", err)
	}

	if err := r.recordStatus(ctx, tx, id, nil, StatusDraft, "seller", &in.SellerID, "created"); err != nil {
		return uuid.Nil, "", err
	}
	return id, ref, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Listing, error) {
	return r.get(ctx, r.pool.QueryRow(ctx, listingSelect+` WHERE l.id = $1`, id))
}

// GetForUpdate takes the row lock. Every status change goes through it, so two
// concurrent transitions serialise rather than both reading 'active' and both
// deciding they may proceed.
func (r *Repository) GetForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Listing, error) {
	return r.get(ctx, tx.QueryRow(ctx, listingSelect+` WHERE l.id = $1 FOR UPDATE OF l`, id))
}

const listingSelect = `
	SELECT l.id, l.reference_no, l.seller_id, l.seller_kind, l.dealer_id,
	       l.make_id, l.model_id, l.trim_id, l.year, l.mileage_km,
	       l.regional_spec, COALESCE(l.transmission, ''), COALESCE(l.fuel_type, ''),
	       COALESCE(l.body_type, ''), COALESCE(l.vin, ''),
	       l.price_minor, l.currency, l.is_negotiable,
	       COALESCE(l.title, ''), COALESCE(l.description, ''),
	       COALESCE(l.emirate, ''), COALESCE(l.city, ''),
	       ST_Y(l.point::geometry), ST_X(l.point::geometry), l.radius_m,
	       l.status, l.featured_until, l.published_at, l.expires_at, l.sold_at, l.created_at
	  FROM vehicle_listings l`

func (r *Repository) get(ctx context.Context, row pgx.Row) (*Listing, error) {
	var l Listing
	var lat, lng *float64
	err := row.Scan(&l.ID, &l.ReferenceNo, &l.SellerID, &l.SellerKind, &l.DealerID,
		&l.MakeID, &l.ModelID, &l.TrimID, &l.Year, &l.MileageKM,
		&l.RegionalSpec, &l.Transmission, &l.FuelType, &l.BodyType, &l.VIN,
		&l.PriceMinor, &l.Currency, &l.IsNegotiable,
		&l.Title, &l.Description, &l.Emirate, &l.City,
		&lat, &lng, &l.RadiusM,
		&l.Status, &l.FeaturedUntil, &l.PublishedAt, &l.ExpiresAt, &l.SoldAt, &l.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load listing: %w", err)
	}
	if lat != nil {
		l.Lat = *lat
	}
	if lng != nil {
		l.Lng = *lng
	}
	return &l, nil
}

// Transition moves a listing and records why.
//
// The `WHERE status = $2` is the optimistic-concurrency guard: if another
// transaction moved the row first, zero rows update and the caller gets a
// conflict rather than silently overwriting a decision.
func (r *Repository) Transition(ctx context.Context, tx pgx.Tx, id uuid.UUID,
	from, to Status, actorType string, actorID *uuid.UUID, reason string) error {

	if !CanTransition(from, to) {
		return fmt.Errorf("%w: a %s listing cannot become %s", httpx.ErrConflict, from, to)
	}

	// Timestamps belong to the transition, not to the caller. Setting
	// published_at from Go means two callers eventually disagree about whether
	// it is the moment of approval or the moment of the request.
	tag, err := tx.Exec(ctx, `
		UPDATE vehicle_listings
		   SET status       = $3,
		       published_at = CASE WHEN $3 = 'active' AND published_at IS NULL
		                           THEN now() ELSE published_at END,
		       expires_at   = CASE WHEN $3 = 'active' AND expires_at IS NULL
		                           THEN now() + make_interval(hours => $4) ELSE expires_at END,
		       sold_at      = CASE WHEN $3 = 'sold' THEN COALESCE(sold_at, now()) ELSE sold_at END,
		       removed_at   = CASE WHEN $3 = 'removed' THEN now() ELSE removed_at END,
		       rejection_reason = CASE WHEN $3 = 'rejected' THEN NULLIF($5, '') ELSE rejection_reason END
		 WHERE id = $1 AND status = $2`,
		id, string(from), string(to), int(ListingTTL/time.Hour), reason)
	if err != nil {
		return fmt.Errorf("transition listing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: the listing changed while this request was in flight", httpx.ErrConflict)
	}
	return r.recordStatus(ctx, tx, id, &from, to, actorType, actorID, reason)
}

func (r *Repository) recordStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID,
	from *Status, to Status, actorType string, actorID *uuid.UUID, reason string) error {

	var fromStr any
	if from != nil {
		fromStr = string(*from)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO listing_status_history (listing_id, from_status, to_status, actor_type, actor_id, reason)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))`,
		id, fromStr, string(to), actorType, actorID, reason,
	); err != nil {
		return fmt.Errorf("record listing status: %w", err)
	}
	return nil
}

// UpdatePrice writes the new price and its history row together.
//
// History is not optional here. A listing whose price moves 40% in three days
// is either a motivated seller or a bait-and-switch, and the price-outlier
// detector reads this table to tell them apart.
func (r *Repository) UpdatePrice(ctx context.Context, tx pgx.Tx, id uuid.UUID, priceMinor int64, by uuid.UUID) error {
	if _, err := tx.Exec(ctx,
		`UPDATE vehicle_listings SET price_minor = $2 WHERE id = $1`, id, priceMinor); err != nil {
		return fmt.Errorf("update price: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO listing_price_history (listing_id, price_minor, currency, changed_by)
		SELECT id, $2, currency, $3 FROM vehicle_listings WHERE id = $1`,
		id, priceMinor, by,
	); err != nil {
		return fmt.Errorf("insert price history: %w", err)
	}
	return nil
}

func (r *Repository) Search(ctx context.Context, f Filter) ([]Result, int64, error) {
	sql, args := f.Build()
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search listings: %w", err)
	}
	defer rows.Close()

	var out []Result
	var total int64
	for rows.Next() {
		var v Result
		if err := rows.Scan(&v.ID, &v.ReferenceNo, &v.SellerID, &v.SellerKind, &v.DealerID,
			&v.MakeID, &v.ModelID, &v.TrimID, &v.Year, &v.MileageKM,
			&v.Transmission, &v.FuelType, &v.BodyType, &v.RegionalSpec, &v.Colour,
			&v.PriceMinor, &v.Currency, &v.IsNegotiable, &v.Title,
			&v.Emirate, &v.City, &v.PublishedAt, &v.DistanceM, &v.Promoted, &total,
		); err != nil {
			return nil, 0, fmt.Errorf("scan listing: %w", err)
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// ── leads, offers, favourites ────────────────────────────────────────────────

type LeadInput struct {
	ListingID      uuid.UUID
	BuyerID        uuid.UUID
	SellerID       uuid.UUID
	Source         string
	SourceReelID   *uuid.UUID
	SourceActionID *uuid.UUID
	ConversationID uuid.NullUUID
}

// UpsertLead is idempotent by (listing, buyer).
//
// A buyer who taps "message seller" twice must not double the seller's lead
// count — dealers pay for lead volume, and a count that inflates on a
// double-tap is a number the platform is charging for and cannot defend.
func (r *Repository) UpsertLead(ctx context.Context, tx pgx.Tx, in LeadInput) (uuid.UUID, bool, error) {
	var id uuid.UUID
	var created bool
	err := tx.QueryRow(ctx, `
		INSERT INTO listing_leads (listing_id, buyer_id, seller_id, source,
		                           source_reel_id, source_action_id, conversation_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (listing_id, buyer_id) DO UPDATE
		   -- Keep the *first* attribution. A buyer who arrives from a reel and
		   -- comes back a week later via search converted because of the reel;
		   -- overwriting would quietly move credit to the last touch, which is
		   -- exactly the bias Phase 3 §4 was written to avoid.
		   SET conversation_id = COALESCE(listing_leads.conversation_id, EXCLUDED.conversation_id)
		RETURNING id, (xmax = 0)`,
		in.ListingID, in.BuyerID, in.SellerID, in.Source,
		in.SourceReelID, in.SourceActionID, in.ConversationID,
	).Scan(&id, &created)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("upsert lead: %w", err)
	}
	return id, created, nil
}

type Offer struct {
	ID             uuid.UUID
	ListingID      uuid.UUID
	BuyerID        uuid.UUID
	AmountMinor    int64
	Currency       string
	State          string
	ExpiresAt      time.Time
	ConversationID uuid.NullUUID
}

func (r *Repository) InsertOffer(ctx context.Context, tx pgx.Tx, o Offer) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO listing_offers (listing_id, conversation_id, buyer_id,
		                            amount_minor, currency, message, expires_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7)
		RETURNING id`,
		o.ListingID, o.ConversationID, o.BuyerID, o.AmountMinor, o.Currency, "", o.ExpiresAt,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert offer: %w", err)
	}
	return id, nil
}

// RespondToOffer is the seller's side. It refuses to act on anything that is
// not still pending, which is what stops an accept racing an expiry sweep.
func (r *Repository) RespondToOffer(ctx context.Context, tx pgx.Tx, offerID, sellerID uuid.UUID, state string) error {
	tag, err := tx.Exec(ctx, `
		UPDATE listing_offers o
		   SET state = $3, responded_at = now()
		  FROM vehicle_listings l
		 WHERE o.id = $1
		   AND o.listing_id = l.id
		   AND l.seller_id = $2
		   AND o.state = 'pending'
		   AND o.expires_at > now()`,
		offerID, sellerID, state)
	if err != nil {
		return fmt.Errorf("respond to offer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: this offer has already been answered, withdrawn or expired",
			httpx.ErrConflict)
	}
	return nil
}

func (r *Repository) Favourite(ctx context.Context, userID, listingID uuid.UUID, on bool) error {
	var err error
	if on {
		_, err = r.pool.Exec(ctx, `
			INSERT INTO listing_favorites (user_id, listing_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, userID, listingID)
	} else {
		_, err = r.pool.Exec(ctx,
			`DELETE FROM listing_favorites WHERE user_id = $1 AND listing_id = $2`, userID, listingID)
	}
	if err != nil {
		return fmt.Errorf("favourite: %w", err)
	}
	return nil
}

// ── sweeps ───────────────────────────────────────────────────────────────────

// ExpireDue retires listings past their window.
//
// Bounded per run rather than "everything due". A month-end burst of ten
// thousand expiries in one statement takes a lock long enough to be noticed on
// the search path, and there is no urgency — a listing that expires twenty
// minutes late has harmed nobody.
func (r *Repository) ExpireDue(ctx context.Context, limit int) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		WITH due AS (
			SELECT id FROM vehicle_listings
			 WHERE status = 'active' AND expires_at IS NOT NULL AND expires_at <= now()
			 ORDER BY expires_at
			 LIMIT $1
			 FOR UPDATE SKIP LOCKED
		), moved AS (
			UPDATE vehicle_listings l SET status = 'expired'
			  FROM due WHERE l.id = due.id
			RETURNING l.id
		)
		INSERT INTO listing_status_history (listing_id, from_status, to_status, actor_type, reason)
		SELECT id, 'active', 'expired', 'system', 'listing window elapsed' FROM moved`,
		limit)
	if err != nil {
		return 0, fmt.Errorf("expire listings: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) ExpireOffers(ctx context.Context, limit int) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE listing_offers SET state = 'expired'
		 WHERE id IN (
			SELECT id FROM listing_offers
			 WHERE state = 'pending' AND expires_at <= now()
			 ORDER BY expires_at LIMIT $1 FOR UPDATE SKIP LOCKED)`,
		limit)
	if err != nil {
		return 0, fmt.Errorf("expire offers: %w", err)
	}
	return tag.RowsAffected(), nil
}
