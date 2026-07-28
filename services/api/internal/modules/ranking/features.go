package ranking

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FeatureWindow is the period ranking signals are counted over.
//
// Fourteen days. Short enough that a reel that stopped working stops ranking,
// long enough that a reel published on a quiet Tuesday accumulates enough
// impressions for its smoothed rate to say anything. It is stored on every row
// because it will be retuned, and a 7-day count compared against a 30-day count
// is not a comparison.
const FeatureWindow = 14 * 24 * time.Hour

// FeatureHorizon bounds which reels get refreshed at all.
//
// A reel published four months ago is outside the feed's 30-day retrieval
// window and cannot be served, so recomputing its features is work with no
// consumer. Without this bound the refresh job's cost grows with the catalogue
// forever, which is the shape of job that is fine for a year and then is the
// reason the database is slow.
const FeatureHorizon = 90 * 24 * time.Hour

// Refresher recomputes the ranking inputs.
type Refresher struct {
	pool *pgxpool.Pool
}

func NewRefresher(pool *pgxpool.Pool) *Refresher { return &Refresher{pool: pool} }

// RefreshFeatures recomputes reel_features for a batch of reels.
//
// Recompute-and-replace, not incremental accumulation. Events arrive late — a
// mobile client that was offline delivers yesterday's batch this morning — and
// an incremental counter cannot absorb that without double-counting. The same
// argument the Phase 3 funnel rollup makes, for the same reason.
//
// Ordered by computed_at NULLS FIRST so the job round-robins: a reel that has
// never been computed goes first, and after that the least recently refreshed.
// Without an ordering, a LIMIT'd refresh recomputes the same rows forever and
// the tail is never touched.
func (r *Refresher) RefreshFeatures(ctx context.Context, batch int) (int64, error) {
	if batch <= 0 {
		batch = 500
	}

	tag, err := r.pool.Exec(ctx, `
		WITH target AS (
		    SELECT r.id, r.published_at
		      FROM reels r
		      LEFT JOIN reel_features f ON f.reel_id = r.id
		     WHERE r.state = 'published'
		       AND r.published_at > now() - make_interval(secs => $2)
		     ORDER BY f.computed_at ASC NULLS FIRST
		     LIMIT $3
		),
		computed AS (
		    SELECT t.id,
		           COALESCE(imp.n, 0)      AS impressions,
		           COALESCE(life.n, 0)     AS lifetime_impressions,
		           COALESCE(ev.views, 0)   AS views,
		           COALESCE(ev.completions, 0) AS completions,
		           COALESCE(ev.shares, 0)  AS shares,
		           COALESCE(clk.n, 0)      AS clicks,
		           COALESCE(lk.n, 0)       AS likes,
		           COALESCE(cm.n, 0)       AS comments,
		           COALESCE(sv.n, 0)       AS saves,
		           COALESCE(ord.n, 0)      AS orders,
		           COALESCE(ord.gmv, 0)    AS gmv_minor
		      FROM target t
		      LEFT JOIN LATERAL (
		          SELECT count(*) AS n FROM feed_impressions fi
		           WHERE fi.reel_id = t.id
		             AND fi.served_at > now() - make_interval(secs => $1)
		      ) imp ON true
		      LEFT JOIN LATERAL (
		          -- Lifetime, bounded below by the reel's own publication so the
		          -- partition pruner has a range to work with. Exploration asks
		          -- "has this ever been given a chance", and a windowed count
		          -- would make a reel eligible again every time the window
		          -- rolled past its launch.
		          SELECT count(*) AS n FROM feed_impressions fi
		           WHERE fi.reel_id = t.id AND fi.served_at >= t.published_at
		      ) life ON true
		      LEFT JOIN LATERAL (
		          SELECT
		              -- The Phase 2 view definition — ≥2s watched — applied to a
		              -- column rather than to a jsonb property, so the number
		              -- that decides what a view is does not depend on a client
		              -- remembering a key name.
		              count(*) FILTER (
		                  WHERE ce.event_type = 'view_progress'
		                    AND COALESCE(ce.watched_ms, 0) >= 2000) AS views,
		              count(*) FILTER (
		                  WHERE ce.event_type = 'view_progress' AND ce.completed) AS completions,
		              count(*) FILTER (WHERE ce.event_type = 'share') AS shares
		            FROM client_events ce
		           WHERE ce.reel_id = t.id
		             AND ce.occurred_at > now() - make_interval(secs => $1)
		      ) ev ON true
		      LEFT JOIN LATERAL (
		          SELECT count(*) AS n FROM action_clicks ac
		           WHERE ac.reel_id = t.id
		             AND ac.clicked_at > now() - make_interval(secs => $1)
		      ) clk ON true
		      LEFT JOIN LATERAL (
		          SELECT count(*) AS n FROM reel_likes l
		           WHERE l.reel_id = t.id
		             AND l.created_at > now() - make_interval(secs => $1)
		      ) lk ON true
		      LEFT JOIN LATERAL (
		          SELECT count(*) AS n FROM reel_comments c
		           WHERE c.reel_id = t.id
		             AND c.state = 'published' AND c.deleted_at IS NULL
		             AND c.created_at > now() - make_interval(secs => $1)
		      ) cm ON true
		      LEFT JOIN LATERAL (
		          SELECT count(*) AS n FROM reel_saves s
		           WHERE s.reel_id = t.id
		             AND s.created_at > now() - make_interval(secs => $1)
		      ) sv ON true
		      LEFT JOIN LATERAL (
		          -- Verified conversions only. Declared and view-through
		          -- attributions are legitimate numbers for a vendor dashboard
		          -- and the wrong training label for a ranker: a claim the
		          -- server could not corroborate teaches the model to optimise
		          -- a fiction, and the error compounds on every retrain.
		          SELECT count(*) AS n, COALESCE(sum(oa.gmv_minor), 0) AS gmv
		            FROM order_attributions oa
		           WHERE oa.reel_id = t.id
		             AND oa.model = 'last_click'
		             AND oa.attributed_at > now() - make_interval(secs => $1)
		      ) ord ON true
		)
		INSERT INTO reel_features (
		    reel_id, window_days, impressions, views, completions, clicks,
		    likes, comments, shares, saves, orders, gmv_minor,
		    lifetime_impressions, computed_at)
		SELECT c.id, ($1 / 86400)::int, c.impressions, c.views, c.completions,
		       c.clicks, c.likes, c.comments, c.shares, c.saves, c.orders,
		       c.gmv_minor, c.lifetime_impressions, now()
		  FROM computed c
		ON CONFLICT (reel_id) DO UPDATE
		   SET window_days = EXCLUDED.window_days,
		       impressions = EXCLUDED.impressions,
		       views = EXCLUDED.views,
		       completions = EXCLUDED.completions,
		       clicks = EXCLUDED.clicks,
		       likes = EXCLUDED.likes,
		       comments = EXCLUDED.comments,
		       shares = EXCLUDED.shares,
		       saves = EXCLUDED.saves,
		       orders = EXCLUDED.orders,
		       gmv_minor = EXCLUDED.gmv_minor,
		       -- Lifetime impressions never go down. A partition dropped by the
		       -- TTL would otherwise make an old reel look new and hand it the
		       -- exploration subsidy a second time.
		       lifetime_impressions = GREATEST(
		           reel_features.lifetime_impressions, EXCLUDED.lifetime_impressions),
		       computed_at = now()`,
		FeatureWindow.Seconds(), FeatureHorizon.Seconds(), batch)
	if err != nil {
		return 0, fmt.Errorf("ranking: refresh features: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RefreshAuthorQuality recomputes the per-author prior.
//
// Rolled up from reel_features rather than from the raw event tables. That
// makes it cheap — it reads one row per reel — and it makes the author prior
// definitionally consistent with the reel scores it sits alongside. Two
// aggregations of the same events by different SQL is how an author with good
// reels ends up with a bad prior and nobody can explain it.
func (r *Refresher) RefreshAuthorQuality(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		WITH per_author AS (
		    SELECT r.author_type, r.author_id,
		           count(*) FILTER (WHERE r.state = 'published') AS published_count,
		           count(*) FILTER (WHERE r.state = 'rejected')  AS rejected_count,
		           count(*) FILTER (WHERE r.state = 'removed')   AS takedown_count,
		           COALESCE(sum(f.impressions), 0) AS impressions,
		           COALESCE(sum(f.views), 0)       AS views,
		           COALESCE(sum(f.clicks), 0)      AS clicks,
		           COALESCE(sum(f.orders), 0)      AS orders
		      FROM reels r
		      LEFT JOIN reel_features f ON f.reel_id = r.id
		     WHERE r.created_at > now() - make_interval(secs => $1)
		     GROUP BY r.author_type, r.author_id
		)
		INSERT INTO author_quality (
		    author_type, author_id, published_count, impressions, views,
		    clicks, orders, rejected_count, takedown_count, computed_at)
		SELECT author_type, author_id, published_count, impressions, views,
		       clicks, orders, rejected_count, takedown_count, now()
		  FROM per_author
		ON CONFLICT (author_type, author_id) DO UPDATE
		   SET published_count = EXCLUDED.published_count,
		       impressions = EXCLUDED.impressions,
		       views = EXCLUDED.views,
		       clicks = EXCLUDED.clicks,
		       orders = EXCLUDED.orders,
		       rejected_count = EXCLUDED.rejected_count,
		       takedown_count = EXCLUDED.takedown_count,
		       computed_at = now()`,
		// A year. An author's reputation should outlive one reel's feature
		// window by a long way — the whole point of a prior is that it carries
		// information a fortnight of data cannot.
		(365 * 24 * time.Hour).Seconds())
	if err != nil {
		return 0, fmt.Errorf("ranking: refresh author quality: %w", err)
	}
	return tag.RowsAffected(), nil
}
