package attribution

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Viewer identifies whose history to search.
//
// Both keys, not one. An anonymous viewer who clicks a CTA, signs up, and then
// orders is the single most valuable conversion path in the product — it is the
// feed doing the entire job, acquisition included — and matching only on
// user_id would attribute it to nothing at all.
type Viewer struct {
	UserID    uuid.NullUUID
	ViewerKey string
}

// GatherEvidence reads the click and impression history for one order.
//
// Bounded by the window and by a row limit. A viewer with a year of history
// must not turn order placement into a table scan, and anything outside the
// window cannot affect the answer anyway.
func (r *Repository) GatherEvidence(ctx context.Context, tx pgx.Tx, v Viewer, placedAt time.Time) (Evidence, error) {
	var e Evidence

	clicks, err := r.clicks(ctx, tx, v, placedAt)
	if err != nil {
		return Evidence{}, err
	}
	e.Clicks = clicks

	// Impressions are only consulted when no click matched, but they are read
	// up front: a second round-trip inside the order transaction is worse than
	// reading a few rows that may go unused, and the order path is where
	// latency is felt.
	impressions, err := r.impressions(ctx, tx, v, placedAt)
	if err != nil {
		return Evidence{}, err
	}
	e.Impressions = impressions

	return e, nil
}

func (r *Repository) clicks(ctx context.Context, tx pgx.Tx, v Viewer, placedAt time.Time) ([]Click, error) {
	var userID any
	if v.UserID.Valid {
		userID = v.UserID.UUID
	}

	rows, err := tx.Query(ctx, `
		SELECT c.id, c.action_id, c.reel_id, a.target_type,
		       COALESCE(a.target_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       c.surface, c.clicked_at, COALESCE(c.watched_ms, 0)
		  FROM action_clicks c
		  JOIN reel_actions a ON a.id = c.action_id
		 WHERE (c.viewer_key = $1 OR ($2::uuid IS NOT NULL AND c.user_id = $2))
		   AND c.clicked_at <= $3
		   AND c.clicked_at > $3 - make_interval(secs => $4)
		 ORDER BY c.clicked_at DESC
		 LIMIT 50`,
		v.ViewerKey, userID, placedAt, ClickWindow.Seconds())
	if err != nil {
		return nil, fmt.Errorf("attribution: read clicks: %w", err)
	}
	defer rows.Close()

	var out []Click
	for rows.Next() {
		var c Click
		if err := rows.Scan(&c.ID, &c.ActionID, &c.ReelID, &c.TargetType,
			&c.TargetID, &c.Surface, &c.ClickedAt, &c.WatchedMS); err != nil {
			return nil, fmt.Errorf("attribution: scan click: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// impressions reads what was shown to this viewer, joined to what each reel's
// primary CTA pointed at.
//
// Position 0 only. A reel's secondary CTAs are decoration; treating a `share`
// button in position 2 as evidence that the reel drove a booking would credit
// almost every impression.
func (r *Repository) impressions(ctx context.Context, tx pgx.Tx, v Viewer, placedAt time.Time) ([]Impression, error) {
	var userID any
	if v.UserID.Valid {
		userID = v.UserID.UUID
	}

	rows, err := tx.Query(ctx, `
		SELECT i.reel_id, a.target_type,
		       COALESCE(a.target_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       i.surface, i.served_at
		  FROM feed_impressions i
		  JOIN reel_actions a ON a.reel_id = i.reel_id AND a.position = 0
		 WHERE (i.viewer_key = $1 OR ($2::uuid IS NOT NULL AND i.user_id = $2))
		   AND i.served_at <= $3
		   AND i.served_at > $3 - make_interval(secs => $4)
		 ORDER BY i.served_at DESC
		 LIMIT 100`,
		v.ViewerKey, userID, placedAt, ViewThroughWindow.Seconds())
	if err != nil {
		return nil, fmt.Errorf("attribution: read impressions: %w", err)
	}
	defer rows.Close()

	var out []Impression
	for rows.Next() {
		var i Impression
		if err := rows.Scan(&i.ReelID, &i.TargetType, &i.TargetID,
			&i.Surface, &i.ServedAt); err != nil {
			return nil, fmt.Errorf("attribution: scan impression: %w", err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// Save writes the resolved attribution.
//
// ON CONFLICT DO NOTHING, never DO UPDATE. The primary key on order_id is what
// keeps one order from contributing two conversions, and an upsert would let a
// replay silently rewrite history — which for a number that justifies the
// entire video investment is exactly the wrong affordance.
//
// Rows are written even for ModelNone. An order with no attribution row is
// indistinguishable from an order the attribution job never ran on, and the
// difference between "we know this was organic" and "we do not know" is the
// difference between a denominator and a guess.
func (r *Repository) Save(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, res Result) error {
	var latency any
	if res.Latency > 0 {
		latency = int(res.Latency.Seconds())
	}

	window := ClickWindow
	if res.Model == ModelViewThrough {
		window = ViewThroughWindow
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO order_attributions
		    (order_id, reel_id, action_id, click_id, model, window_seconds,
		     latency_seconds, off_feed, gmv_minor, currency)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (order_id) DO NOTHING`,
		orderID, res.ReelID, res.ActionID, res.ClickID, string(res.Model),
		int(window.Seconds()), latency, res.OffFeed,
		nullableInt64(res.GMVMinor), nullableCurrency(res.Currency))
	if err != nil {
		return fmt.Errorf("attribution: save: %w", err)
	}
	return nil
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullableCurrency(c string) any {
	if len(c) != 3 {
		return nil
	}
	return c
}
