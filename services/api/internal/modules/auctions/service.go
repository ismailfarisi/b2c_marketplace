package auctions

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

type Service struct{ pool *pgxpool.Pool }

func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

const auctionColumns = `
	id, reference_no, seller_id, emirate, plate_code, plate_number, digit_count,
	currency, opening_minor, reserve_minor, increment_minor,
	opens_at, closes_at, status, current_minor, current_bidder, bid_count, winner_id`

func scanAuction(row pgx.Row) (Auction, error) {
	var a Auction
	var status string
	err := row.Scan(&a.ID, &a.ReferenceNo, &a.SellerID, &a.Emirate, &a.PlateCode,
		&a.PlateNumber, &a.DigitCount, &a.Currency, &a.OpeningMinor, &a.ReserveMinor,
		&a.IncrementMinor, &a.OpensAt, &a.ClosesAt, &status,
		&a.CurrentMinor, &a.CurrentBidder, &a.BidCount, &a.WinnerID)
	a.Status = Status(status)
	return a, err
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Auction, error) {
	a, err := scanAuction(s.pool.QueryRow(ctx,
		`SELECT`+auctionColumns+` FROM plate_auctions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Auction{}, httpx.ErrNotFound
	}
	if err != nil {
		return Auction{}, fmt.Errorf("auctions: get: %w", err)
	}
	return a, nil
}

type CreateInput struct {
	SellerID       uuid.UUID
	Emirate        string
	PlateCode      string
	PlateNumber    string
	OpeningMinor   int64
	ReserveMinor   *int64
	IncrementMinor int64
	OpensAt        time.Time
	ClosesAt       time.Time
	Lat, Lng       float64
}

func (s *Service) Create(ctx context.Context, in CreateInput) (Auction, error) {
	digits, err := DigitsOf(in.PlateNumber)
	if err != nil {
		return Auction{}, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "plate_number", Code: "invalid", Message: err.Error(),
		}}}
	}
	if err := ValidateSchedule(in.OpensAt, in.ClosesAt); err != nil {
		return Auction{}, &httpx.ValidationError{Fields: []httpx.FieldError{{
			Field: "closes_at", Code: "invalid", Message: err.Error(),
		}}}
	}
	if in.IncrementMinor <= 0 {
		in.IncrementMinor = 100_000
	}

	var id uuid.UUID
	err = s.pool.QueryRow(ctx, `
		INSERT INTO plate_auctions (
			seller_id, emirate, plate_code, plate_number, digit_count,
			opening_minor, reserve_minor, increment_minor, opens_at, closes_at,
			point, status)
		VALUES ($1, $2, upper($3), $4, $5, $6, $7, $8, $9, $10,
		        CASE WHEN $11 = 0 AND $12 = 0 THEN NULL
		             ELSE ST_SetSRID(ST_MakePoint($12, $11), 4326)::geography END,
		        'draft')
		RETURNING id`,
		in.SellerID, in.Emirate, in.PlateCode, in.PlateNumber, digits,
		in.OpeningMinor, in.ReserveMinor, in.IncrementMinor, in.OpensAt, in.ClosesAt,
		in.Lat, in.Lng,
	).Scan(&id)
	if err != nil {
		return Auction{}, fmt.Errorf("auctions: create: %w", err)
	}
	return s.Get(ctx, id)
}

// Schedule moves a draft to `scheduled`, which is what makes it real.
//
// A separate step from Create for the same reason the sell flow separates draft
// from publish: the reel has to exist and be approved before anyone can be told
// a lot is coming.
func (s *Service) Schedule(ctx context.Context, sellerID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE plate_auctions SET status = 'scheduled'
		 WHERE id = $1 AND seller_id = $2 AND status = 'draft'`, id, sellerID)
	if err != nil {
		return fmt.Errorf("auctions: schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return httpx.ErrNotFound
	}
	return nil
}

// Bid is the whole point of the module.
//
// # Why every line of this is inside one transaction with a row lock
//
// Two bids arriving in the same millisecond is not a rare case on a lot that is
// about to close — it is the *expected* case, because that is exactly when
// everybody is watching. Without `FOR UPDATE`, both read the same
// `current_minor`, both decide they are high enough, and both write. The second
// write wins and the first bidder is silently outbid by a number lower than
// their own.
//
// The check is a pure function ([CheckBid]) so the rule can be tested without a
// database; the lock is here so the rule is applied to a value nobody else can
// change underneath it.
func (s *Service) Bid(ctx context.Context, auctionID, bidderID uuid.UUID, amountMinor int64) (Auction, error) {
	var out Auction

	err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		a, err := scanAuction(tx.QueryRow(ctx,
			`SELECT`+auctionColumns+` FROM plate_auctions WHERE id = $1 FOR UPDATE`, auctionID))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("auctions: lock lot: %w", err)
		}

		// One clock read for the whole decision. Reading `now` again below
		// would let the lot be open for the check and closed for the write.
		now := time.Now()

		// A lot whose closing time has passed but which the sweep has not
		// reached yet is closed, and is treated as closed here rather than
		// taking a bid the settlement is about to ignore.
		if err := CheckBid(a, bidderID.String(), amountMinor, now); err != nil {
			return err
		}

		closesAt, extended := ExtendedClose(a.ClosesAt, now)

		if _, err := tx.Exec(ctx, `
			INSERT INTO plate_bids (auction_id, bidder_id, amount_minor, extended_close)
			VALUES ($1, $2, $3, $4)`,
			auctionID, bidderID, amountMinor, extended,
		); err != nil {
			return fmt.Errorf("auctions: record bid: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE plate_auctions
			   SET current_minor = $2, current_bidder = $3,
			       bid_count = bid_count + 1, closes_at = $4
			 WHERE id = $1`,
			auctionID, amountMinor, bidderID, closesAt,
		); err != nil {
			return fmt.Errorf("auctions: apply bid: %w", err)
		}

		a.CurrentMinor = &amountMinor
		b := bidderID.String()
		a.CurrentBidder = &b
		a.BidCount++
		a.ClosesAt = closesAt
		out = a
		return nil
	})

	return out, err
}

// OpenDue moves scheduled lots to live. Run by the worker sweep.
func (s *Service) OpenDue(ctx context.Context, now time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE plate_auctions SET status = 'live'
		 WHERE status = 'scheduled' AND opens_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("auctions: open due: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// CloseDue settles every lot whose time is up.
//
// Won or unsold, decided by the reserve. A lot that closes below its reserve is
// **unsold** — not sold cheap. That is the entire function of a reserve, and
// getting it wrong transfers a plate for less than the seller agreed to.
//
// One statement rather than a read-then-write loop, so a lot cannot be closed
// twice by two workers: the WHERE clause is the lock.
func (s *Service) CloseDue(ctx context.Context, now time.Time) (won, unsold int, err error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE plate_auctions
		   SET status = 'closed_won', winner_id = current_bidder
		 WHERE status = 'live' AND closes_at <= $1
		   AND current_bidder IS NOT NULL
		   AND (reserve_minor IS NULL OR current_minor >= reserve_minor)`, now)
	if err != nil {
		return 0, 0, fmt.Errorf("auctions: close won: %w", err)
	}
	won = int(tag.RowsAffected())

	tag, err = s.pool.Exec(ctx, `
		UPDATE plate_auctions SET status = 'closed_unsold'
		 WHERE status = 'live' AND closes_at <= $1`, now)
	if err != nil {
		return won, 0, fmt.Errorf("auctions: close unsold: %w", err)
	}
	return won, int(tag.RowsAffected()), nil
}

// Bid is one row of the history.
type Bid struct {
	BidderID string
	Amount   int64
	Extended bool
	PlacedAt time.Time
	IsMine   bool
}

// History is the bid list, newest first.
//
// Bidder ids are returned only as `IsMine`. A public list of who bid what is
// how an auction platform hands a competitor a map of its buyers — and on a
// plate, the bidder's identity is the one thing nobody needs to know.
func (s *Service) History(ctx context.Context, auctionID uuid.UUID, viewer uuid.NullUUID, limit int) ([]Bid, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	rows, err := s.pool.Query(ctx, `
		SELECT bidder_id, amount_minor, extended_close, placed_at
		  FROM plate_bids WHERE auction_id = $1
		 ORDER BY amount_minor DESC LIMIT $2`, auctionID, limit)
	if err != nil {
		return nil, fmt.Errorf("auctions: history: %w", err)
	}
	defer rows.Close()

	var out []Bid
	for rows.Next() {
		var b Bid
		var bidder uuid.UUID
		if err := rows.Scan(&bidder, &b.Amount, &b.Extended, &b.PlacedAt); err != nil {
			return nil, fmt.Errorf("auctions: scan bid: %w", err)
		}
		b.IsMine = viewer.Valid && viewer.UUID == bidder
		out = append(out, b)
	}
	return out, rows.Err()
}
