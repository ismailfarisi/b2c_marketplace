package ordering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/marketplace/api/internal/httpx"
	"github.com/marketplace/api/internal/seam/fulfillment"
	"github.com/marketplace/api/internal/seam/servicetype"
)

type Repository struct{}

type Branch struct {
	ID       uuid.UUID
	VendorID uuid.UUID
	Currency string
	Country  string
	Timezone string
}

type Offering struct {
	ID              uuid.UUID
	ServiceType     string
	Name            string
	NameAr          string
	BasePriceMinor  int64
	Currency        string
	TaxRateBP       int
	PriceIsEstimate bool
}

type Address struct {
	ID        uuid.UUID
	Line      string
	Landmark  string
	Area      string
	City      string
	Notes     string
	Recipient string
	Phone     string
	Lat, Lng  float64
}

type Order struct {
	ID          uuid.UUID
	OrderNo     string
	Status      string
	BranchID    uuid.UUID
	VendorID    uuid.UUID
	ServiceType string
	Currency    string
	QuoteID     uuid.UUID
	PlacedAt    time.Time

	// Filled at placement, not loaded from the row. Surfaced so a client can
	// confirm the order was credited to the reel the customer came from — which
	// is the one moment the attribution is cheap to sanity-check, before the
	// evidence ages out of the window.
	AttributionModel string
	AttributedReelID *uuid.UUID
}

func (r *Repository) LoadBranch(ctx context.Context, tx pgx.Tx, id uuid.UUID) (Branch, error) {
	var b Branch
	err := tx.QueryRow(ctx, `
		SELECT b.id, b.vendor_id, v.default_currency, b.country, b.timezone
		  FROM vendor_branches b
		  JOIN vendors v ON v.id = b.vendor_id
		 WHERE b.id = $1
		   AND b.is_active
		   AND b.deleted_at IS NULL
		   AND v.status = 'active'
		   AND v.deleted_at IS NULL`, id,
	).Scan(&b.ID, &b.VendorID, &b.Currency, &b.Country, &b.Timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, fmt.Errorf("%w: branch %s is not accepting orders", httpx.ErrNotFound, id)
	}
	if err != nil {
		return b, fmt.Errorf("load branch: %w", err)
	}
	return b, nil
}

func (r *Repository) LoadAddress(ctx context.Context, tx pgx.Tx, userID, addressID uuid.UUID) (Address, error) {
	var a Address
	err := tx.QueryRow(ctx, `
		SELECT id,
		       concat_ws(', ', NULLIF(flat,''), NULLIF(building,''), NULLIF(street,'')) AS line,
		       COALESCE(landmark,''), COALESCE(area,''), COALESCE(city,''), COALESCE(notes,''),
		       COALESCE(recipient_name,''), COALESCE(recipient_phone,''),
		       ST_Y(point::geometry), ST_X(point::geometry)
		  FROM addresses
		 WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		addressID, userID,
	).Scan(&a.ID, &a.Line, &a.Landmark, &a.Area, &a.City, &a.Notes,
		&a.Recipient, &a.Phone, &a.Lat, &a.Lng)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, fmt.Errorf("%w: address %s", httpx.ErrNotFound, addressID)
	}
	if err != nil {
		return a, fmt.Errorf("load address: %w", err)
	}
	return a, nil
}

func (r *Repository) LoadOfferings(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]Offering, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, service_type, name, COALESCE(name_ar,''),
		       base_price_minor, currency, tax_rate_bp, price_is_estimate
		  FROM service_offerings
		 WHERE branch_id = $1
		   AND id = ANY($2)
		   AND is_active
		   AND deleted_at IS NULL`, branchID, ids)
	if err != nil {
		return nil, fmt.Errorf("load offerings: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]Offering, len(ids))
	for rows.Next() {
		var o Offering
		if err := rows.Scan(&o.ID, &o.ServiceType, &o.Name, &o.NameAr,
			&o.BasePriceMinor, &o.Currency, &o.TaxRateBP, &o.PriceIsEstimate); err != nil {
			return nil, fmt.Errorf("scan offering: %w", err)
		}
		out[o.ID] = o
	}
	return out, rows.Err()
}

type insertOrderParams struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	BranchID       uuid.UUID
	VendorID       uuid.UUID
	ServiceType    string
	Currency       string
	Country        string
	Notes          string
	SourceReelID   *uuid.UUID
	SourceActionID *uuid.UUID
}

// InsertOrder retries on order-number collision.
//
// generate_order_no draws 6 characters from a 32-symbol alphabet — about a
// billion combinations, so a collision is rare but not impossible, and the
// unique index turns it into a failed order rather than a duplicate. Three
// attempts makes the probability negligible without pretending it is zero.
func (r *Repository) InsertOrder(ctx context.Context, tx pgx.Tx, p insertOrderParams) error {
	for attempt := 0; attempt < 3; attempt++ {
		_, err := tx.Exec(ctx, `
			INSERT INTO orders (
				id, order_no, user_id, branch_id, vendor_id, service_type,
				status, kind, currency, notes, source_reel_id, source_action_id
			) VALUES (
				$1, generate_order_no($2), $3, $4, $5, $6,
				'pending_payment', 'one_off', $7, NULLIF($8,''), $9, $10
			)`,
			p.ID, p.Country, p.UserID, p.BranchID, p.VendorID, p.ServiceType,
			p.Currency, p.Notes, p.SourceReelID, p.SourceActionID)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err, "orders_order_no_key") {
			return fmt.Errorf("insert order: %w", err)
		}
	}
	return fmt.Errorf("insert order: could not allocate a unique order number in 3 attempts")
}

func (r *Repository) InsertOrderLine(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, idx int, line LineInput, o Offering) error {
	options, err := json.Marshal(line.Options)
	if err != nil {
		return fmt.Errorf("marshal line options: %w", err)
	}
	if line.Options == nil {
		options = []byte(`{}`)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO order_lines (
			order_id, offering_id, name_snapshot, name_ar_snapshot,
			unit_price_minor, currency, tax_rate_bp_snapshot, quantity, options, sort_order
		) VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10)`,
		orderID, o.ID, o.Name, o.NameAr,
		o.BasePriceMinor, o.Currency, o.TaxRateBP, line.Quantity, options, idx)
	if err != nil {
		return fmt.Errorf("insert order line: %w", err)
	}
	return nil
}

func (r *Repository) FindBucket(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, resourceKey string, date time.Time, windowID uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT id FROM capacity_buckets
		 WHERE branch_id = $1 AND resource_key = $2 AND bucket_date = $3 AND window_id = $4`,
		branchID, resourceKey, date, windowID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// No bucket means the branch never opened that window. Indistinguishable
		// from "full" to the client by design — see the availability query.
		return uuid.Nil, httpx.ErrCapacityTaken
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("find capacity bucket: %w", err)
	}
	return id, nil
}

func (r *Repository) Reserve(ctx context.Context, tx pgx.Tx, bucketID, orderID uuid.UUID, units int, ttl time.Duration) (uuid.UUID, error) {
	var id *uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT reserve_capacity($1, $2, $3, $4::interval)`,
		bucketID, orderID, units, fmt.Sprintf("%d seconds", int(ttl.Seconds())),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("reserve capacity: %w", err)
	}
	if id == nil {
		return uuid.Nil, nil
	}
	return *id, nil
}

func (r *Repository) InsertFulfillment(ctx context.Context, tx pgx.Tx, orderID, branchID uuid.UUID, req fulfillment.Request, reservationID uuid.UUID, addr Address) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO fulfillments (
			order_id, branch_id, kind, status, sequence,
			scheduled_date, window_id, reservation_id,
			address_id, address_line, address_landmark, address_area,
			address_city, address_notes, recipient_name, recipient_phone,
			point, payload_ref
		) VALUES (
			$1,$2,$3,'scheduled',$4,
			$5,$6,$7,
			$8,$9,NULLIF($10,''),NULLIF($11,''),
			NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),NULLIF($15,''),
			ST_SetSRID(ST_MakePoint($16,$17),4326)::geography, NULLIF($18,'')
		)`,
		orderID, branchID, string(req.Kind), req.Sequence,
		req.ScheduledDate, req.WindowID, reservationID,
		addr.ID, addr.Line, addr.Landmark, addr.Area,
		addr.City, addr.Notes, addr.Recipient, addr.Phone,
		addr.Lng, addr.Lat, req.PayloadRef)
	if err != nil {
		return fmt.Errorf("insert fulfillment: %w", err)
	}
	return nil
}

func (r *Repository) InsertQuote(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, q servicetype.QuoteResult) (uuid.UUID, error) {
	var quoteID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO price_quotes (
			order_id, version, state, currency,
			subtotal_minor, discount_minor, delivery_minor, surcharge_minor,
			tax_minor, total_minor, tax_inclusive, authorized_minor
		) VALUES ($1, 1, 'estimated', $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`,
		orderID, q.Currency,
		q.SubtotalMinor, q.DiscountMinor, q.DeliveryMinor, q.SurchargeMinor,
		q.TaxMinor, q.TotalMinor, q.TaxInclusive, q.AuthorizeMinor,
	).Scan(&quoteID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert quote: %w", err)
	}

	for i, line := range q.Lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO quote_lines (quote_id, kind, label, amount_minor, tax_rate_bp, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			quoteID, line.Kind, line.Label, line.AmountMinor, nullableInt(line.TaxRateBP), i,
		); err != nil {
			return uuid.Nil, fmt.Errorf("insert quote line: %w", err)
		}
	}
	return quoteID, nil
}

func (r *Repository) InsertOutbox(ctx context.Context, tx pgx.Tx, aggregateType string, aggregateID uuid.UUID, eventType string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload, idempotency_key)
		VALUES ($1,$2,$3,$4,$5)`,
		aggregateType, aggregateID, eventType, body,
		fmt.Sprintf("%s:%s:%s", aggregateType, aggregateID, eventType))
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func (r *Repository) LoadOrder(ctx context.Context, tx pgx.Tx, orderID, userID uuid.UUID) (*Order, error) {
	var o Order
	err := tx.QueryRow(ctx, `
		SELECT o.id, o.order_no, o.status::text, o.branch_id, o.vendor_id,
		       o.service_type, o.currency, o.placed_at,
		       COALESCE(q.id, '00000000-0000-0000-0000-000000000000'::uuid)
		  FROM orders o
		  LEFT JOIN price_quotes q ON q.order_id = o.id AND q.superseded_by IS NULL
		 WHERE o.id = $1 AND o.user_id = $2`,
		orderID, userID,
	).Scan(&o.ID, &o.OrderNo, &o.Status, &o.BranchID, &o.VendorID,
		&o.ServiceType, &o.Currency, &o.PlacedAt, &o.QuoteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load order: %w", err)
	}
	return &o, nil
}

func nullableInt(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// isUniqueViolation reports whether err is a 23505 on the named constraint.
//
// Matching the constraint name matters: retrying on any unique violation would
// loop forever on a genuine duplicate that a new order number cannot fix.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
