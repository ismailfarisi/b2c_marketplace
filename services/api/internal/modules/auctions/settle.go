package auctions

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/marketplace/api/internal/money"
	"github.com/marketplace/api/internal/platform/postgres"
)

// PaymentTerm is how long a seller has to pay the commission.
//
// Seven days. The plate transfer itself happens at an RTA counter on the two
// parties' own timetable, and a fee due before they have met is a fee that
// arrives while the seller is still unsure the sale is real.
const PaymentTerm = 7 * 24 * time.Hour

// marketZone is the wall clock every date in a notification is rendered
// against.
//
// # Why this is not the server's local time
//
// It was, and it produced a settlement row reading "07 Aug" beside a
// notification reading "8 Aug" — for the same instant. `to_char` rendered the
// timestamp in the database session's zone and Go's Format rendered it in the
// process's, and the two disagreed because a deadline four hours before
// midnight UTC is the next day in Dubai.
//
// On this machine the process happened to be +04 and the *notification* was
// right by luck. In a container, which is UTC, the seller would be told their
// payment is due a day earlier than it is — a deadline rendered early is a
// seller paying under pressure that does not exist, or worse, concluding they
// are already late.
//
// Fixed to the market rather than read from config: this module has one market,
// and a timezone threaded through a constructor for a single value would be
// configuration theatre. It is the same argument `TimeSignal.Timezone` makes
// for carrying an IANA name rather than an offset — a rule in a wall clock is
// not an instant somebody happened to mean once.
var marketZone = mustLoad("Asia/Dubai")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// A container without tzdata. UTC is wrong by four hours, which is
		// better than a nil location panicking on the first settled lot — and
		// the Go toolchain's embedded tzdata makes this genuinely unreachable
		// in this build.
		return time.UTC
	}
	return loc
}

// Notifier is the slice of the notification service this module needs.
//
// An interface rather than the concrete service, and narrow to one method, for
// the reason the codebase applies elsewhere: `auctions` should be constructible
// in a test without a template table, and a deployment with notifications
// switched off should run auctions rather than fail to start.
type Notifier interface {
	Enqueue(ctx context.Context, tx pgx.Tx, m NotifyMessage) error
}

// NotifyMessage mirrors notify.Message without importing it.
//
// The duplication is deliberate and is three fields wide. Importing `notify`
// here would make the auction module depend on the notification schema, and the
// adapter that bridges them lives in `internal/app` where every other
// cross-module wire already does.
type NotifyMessage struct {
	UserID      string
	TemplateKey string
	Params      map[string]string
	// DedupeKey makes the send idempotent. The close sweep is at-least-once and
	// a bidder told three times that they lost has been given three reasons to
	// distrust the result.
	DedupeKey string
}

// WithNotifier attaches one. Nil-safe throughout: an auction runs and settles
// without it, silently, which is what every deployment did before this existed.
func (s *Service) WithNotifier(n Notifier) *Service {
	s.notify = n
	return s
}

// SettleClosed turns every `closed_won` lot into a settlement row and tells
// both parties.
//
// A separate pass from CloseDue rather than part of it, and the separation is
// the point: closing is a clock operation that must finish inside the anti-snipe
// window, and settling touches a second table and enqueues notifications. A
// close that got slower because settlement got slower would start taking bids
// after the hammer, which is the one thing the tight sweep exists to prevent.
//
// Idempotent by the unique constraint on `auction_id`: a lot already settled is
// skipped, so a worker restart mid-pass repeats nothing.
func (s *Service) SettleClosed(ctx context.Context, commissionBP, vatBP int, limit int) (int, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.seller_id, a.winner_id, a.current_minor, a.currency,
		       a.plate_code, a.plate_number
		  FROM plate_auctions a
		  LEFT JOIN auction_settlements st ON st.auction_id = a.id
		 WHERE a.status = 'closed_won' AND st.id IS NULL
		 ORDER BY a.closes_at
		 LIMIT $1`, limit)
	if err != nil {
		return 0, fmt.Errorf("auctions: find unsettled: %w", err)
	}

	type pending struct {
		id, seller, winner uuid.UUID
		hammer             int64
		currency           string
		plate              string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		var code, number string
		if err := rows.Scan(&p.id, &p.seller, &p.winner, &p.hammer, &p.currency,
			&code, &number); err != nil {
			rows.Close()
			return 0, fmt.Errorf("auctions: scan unsettled: %w", err)
		}
		p.plate = code + " " + number
		todo = append(todo, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	settled := 0
	for _, p := range todo {
		// The commission and the VAT on it, in the same basis-point arithmetic
		// the order path uses. Rounded half-up once each rather than compounded:
		// computing VAT on a rounded commission and rounding again is how two
		// systems end up a fils apart on the same sale.
		commission := money.Percentage(money.New(p.hammer, p.currency), commissionBP)
		vat := money.Percentage(commission, vatBP)
		dueAt := time.Now().Add(PaymentTerm)

		err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				INSERT INTO auction_settlements (
					auction_id, seller_id, winner_id, hammer_minor, currency,
					commission_bp, commission_minor, vat_minor, due_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (auction_id) DO NOTHING`,
				p.id, p.seller, p.winner, p.hammer, p.currency,
				commissionBP, commission.Minor, vat.Minor, dueAt)
			if err != nil {
				return fmt.Errorf("auctions: insert settlement: %w", err)
			}
			if tag.RowsAffected() == 0 {
				// Another worker got there first. Not an error, and crucially
				// not a second pair of notifications.
				return nil
			}

			if _, err := tx.Exec(ctx,
				`UPDATE plate_auctions SET status = 'settled' WHERE id = $1`, p.id,
			); err != nil {
				return fmt.Errorf("auctions: mark settled: %w", err)
			}

			// Both parties, inside the same transaction as the settlement row.
			// An outbox write that is not transactional with the thing it
			// announces is how a seller is told their lot sold and then finds
			// no settlement, or the reverse.
			s.tell(ctx, tx, NotifyMessage{
				UserID:      p.winner.String(),
				TemplateKey: "auction.won",
				DedupeKey:   "auction.won:" + p.id.String(),
				Params: map[string]string{
					"plate":  p.plate,
					"amount": money.New(p.hammer, p.currency).Major(),
				},
			})
			s.tell(ctx, tx, NotifyMessage{
				UserID:      p.seller.String(),
				TemplateKey: "auction.sold",
				DedupeKey:   "auction.sold:" + p.id.String(),
				Params: map[string]string{
					"plate":  p.plate,
					"amount": money.New(p.hammer, p.currency).Major(),
					// Commission plus its VAT, because that is the number that
					// will leave the seller's account. Quoting the commission
					// alone and invoicing more is the sort of surprise that
					// costs a seller.
					"fee": money.New(commission.Minor+vat.Minor, p.currency).Major(),
					"due": dueAt.In(marketZone).Format("2 Jan"),
				},
			})
			return nil
		})
		if err != nil {
			// One bad lot does not stop the pass. The next sweep retries it,
			// and the unique constraint means the retry is free.
			return settled, err
		}
		settled++
	}
	return settled, nil
}

// NotifyUnsold tells a seller their lot closed below its reserve.
//
// Its own pass rather than part of CloseDue for the same reason settlement is,
// and its own *state* would have been wrong: the lot is already
// `closed_unsold`, and adding a "notified" flag to carry one boolean would put
// a column on the auction to serve the notifier. The dedupe key does the job.
func (s *Service) NotifyUnsold(ctx context.Context, since time.Time, limit int) (int, error) {
	if s.notify == nil {
		return 0, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, seller_id, plate_code, plate_number, currency,
		       COALESCE(current_minor, opening_minor)
		  FROM plate_auctions
		 WHERE status = 'closed_unsold' AND closes_at >= $1
		 ORDER BY closes_at DESC
		 LIMIT $2`, since, limit)
	if err != nil {
		return 0, fmt.Errorf("auctions: find unsold: %w", err)
	}
	defer rows.Close()

	sent := 0
	for rows.Next() {
		var (
			id, seller   uuid.UUID
			code, number string
			currency     string
			amount       int64
		)
		if err := rows.Scan(&id, &seller, &code, &number, &currency, &amount); err != nil {
			return sent, fmt.Errorf("auctions: scan unsold: %w", err)
		}
		err := postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			s.tell(ctx, tx, NotifyMessage{
				UserID:      seller.String(),
				TemplateKey: "auction.unsold",
				DedupeKey:   "auction.unsold:" + id.String(),
				Params: map[string]string{
					"plate":  code + " " + number,
					"amount": money.New(amount, currency).Major(),
				},
			})
			return nil
		})
		if err != nil {
			return sent, err
		}
		sent++
	}
	return sent, rows.Err()
}

// tell enqueues a notification without letting it fail the thing it announces.
//
// Deliberately not returning the error. A notification that cannot be enqueued
// must not roll back the settlement or the bid it was announcing — the money
// record and the bid are what matter, and a seller who is not told still has a
// settlement to find on their own screen.
//
// Logged rather than dropped, which the first version got wrong. A silent
// `_ =` means a notifier that is misconfigured, missing a template, or simply
// not wired in produces a product where nobody is ever told anything and
// nothing anywhere says so — which is exactly how this shipped with the API's
// service having no notifier attached at all.
func (s *Service) tell(ctx context.Context, tx pgx.Tx, m NotifyMessage) {
	if s.notify == nil {
		return
	}
	if err := s.notify.Enqueue(ctx, tx, m); err != nil {
		slog.WarnContext(ctx, "auction notification not enqueued",
			"error", err, "template", m.TemplateKey, "dedupe", m.DedupeKey)
	}
}
