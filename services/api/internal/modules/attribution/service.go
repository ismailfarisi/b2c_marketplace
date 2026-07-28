package attribution

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service resolves and records attribution for an order.
type Service struct {
	pool *pgxpool.Pool
	repo *Repository
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, repo: NewRepository(pool)}
}

// AttributeOrder runs inside the caller's transaction.
//
// Inside, not after, and this is the one design decision here worth arguing
// about. The case against: attribution is analytics, an analytics failure
// should never fail a customer's order, and taking a transaction hostage to a
// measurement is how measurement starts costing revenue.
//
// The case for, which wins: an order that commits without its attribution row
// is permanently unattributable. The evidence is time-windowed, so there is no
// backfill — by the time anything notices, the clicks are outside the window
// and the answer is gone. And the whole point of Phase 3 is that the conversion
// number is trustworthy; a number with a silent hole in it wherever a
// background job failed is not.
//
// The risk is bounded by the compromise below: a resolution *error* is logged
// and downgraded to ModelNone rather than propagated. So a broken attribution
// query costs accuracy on that order, never the order itself. Only a failure to
// write the row at all — which means Postgres is already refusing writes —
// takes the order with it.
func (s *Service) AttributeOrder(ctx context.Context, tx pgx.Tx, o Order, v Viewer) Result {
	res := Result{
		Model:    ModelNone,
		GMVMinor: o.GMVMinor,
		Currency: o.Currency,
		Reason:   "attribution not resolved",
	}

	if v.ViewerKey != "" || v.UserID.Valid {
		evidence, err := s.repo.GatherEvidence(ctx, tx, v, o.PlacedAt)
		if err != nil {
			slog.ErrorContext(ctx, "attribution evidence unavailable; recording as organic",
				"order_id", o.ID, "error", err)
		} else {
			res = Resolve(o, evidence, time.Now())
		}
	} else if o.DeclaredReelID != nil {
		// No viewer identity at all, but the client named a reel. Recorded as
		// declared, which is what it is: unverifiable.
		res = Resolve(o, Evidence{}, time.Now())
	}

	if err := s.repo.Save(ctx, tx, o.ID, res); err != nil {
		// Deliberately not returned. See the doc comment: a write failure here
		// means Postgres is refusing writes, in which case the order's own
		// INSERT is about to fail too and this error would only mask that one.
		slog.ErrorContext(ctx, "failed to record attribution",
			"order_id", o.ID, "model", res.Model, "error", err)
	}
	return res
}

// Funnel is one reel's conversion record. Exposed on the service so callers do
// not reach past it into the repository.
func (s *Service) Funnel(ctx context.Context, reelID uuid.UUID, from, to time.Time) (*Funnel, error) {
	return s.repo.Funnel(ctx, reelID, from, to)
}

func (s *Service) PlatformFunnel(ctx context.Context, from, to time.Time) (*PlatformFunnel, error) {
	return s.repo.PlatformFunnel(ctx, from, to)
}

// RollupDay recomputes a closed day into reel_funnel_daily. Called by the
// worker.
func (s *Service) RollupDay(ctx context.Context, day time.Time) (int64, error) {
	return s.repo.RollupDay(ctx, day)
}

// ForOrder reads back a stored attribution. Used by the vendor dashboard to
// answer "did this order come from a reel", which is the question a vendor
// filming content actually wants answered.
func (s *Service) ForOrder(ctx context.Context, orderID uuid.UUID) (*Stored, error) {
	var st Stored
	var model string
	err := s.pool.QueryRow(ctx, `
		SELECT order_id, reel_id, action_id, model, window_seconds,
		       latency_seconds, off_feed, COALESCE(gmv_minor, 0),
		       COALESCE(currency, ''), attributed_at
		  FROM order_attributions WHERE order_id = $1`, orderID,
	).Scan(&st.OrderID, &st.ReelID, &st.ActionID, &model, &st.WindowSeconds,
		&st.LatencySeconds, &st.OffFeed, &st.GMVMinor, &st.Currency, &st.AttributedAt)
	if err != nil {
		return nil, err
	}
	st.Model = Model(model)
	return &st, nil
}

// Stored is an attribution as persisted.
type Stored struct {
	OrderID        uuid.UUID
	ReelID         *uuid.UUID
	ActionID       *uuid.UUID
	Model          Model
	WindowSeconds  int
	LatencySeconds *int
	OffFeed        bool
	GMVMinor       int64
	Currency       string
	AttributedAt   time.Time
}
