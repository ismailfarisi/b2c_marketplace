package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/app"
	"github.com/marketplace/api/internal/modules/ranking"
	"github.com/marketplace/api/internal/modules/reels"
	"github.com/marketplace/api/internal/modules/search"
	"github.com/marketplace/api/internal/modules/timesignal"
)

// mediaSweep advances every reel the pipeline still owes work to.
//
// This is a poller, and it is deliberately a poller in Phase 2. River lands
// with the first job that genuinely needs scheduling, retry policy and
// concurrency limits; until then the honest description of the work is "find
// the reels that are stuck and push them", and a 10-second tick does that in 40
// lines instead of 400.
//
// The important property is not the trigger — it is that every step is
// idempotent, so a poller, a webhook and a retry can all call the same function
// without coordinating.
func mediaSweep(ctx context.Context, pool *pgxpool.Pool, media *app.Media) {
	rows, err := pool.Query(ctx, `
		SELECT id, state::text
		  FROM reels
		 WHERE state IN ('draft', 'uploaded', 'probing', 'transcoding', 'moderating', 'approved')
		   -- Leave freshly-touched reels alone: the request that just moved one
		   -- is very likely still chaining it forward in-process, and racing it
		   -- would mean two workers doing the same transcode.
		   AND updated_at < now() - interval '10 seconds'
		 ORDER BY updated_at
		 LIMIT 100`)
	if err != nil {
		slog.ErrorContext(ctx, "media sweep query failed", "error", err)
		return
	}

	type target struct {
		id    uuid.UUID
		state string
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.state); err != nil {
			rows.Close()
			slog.ErrorContext(ctx, "media sweep scan failed", "error", err)
			return
		}
		targets = append(targets, t)
	}
	rows.Close()

	for _, t := range targets {
		job, err := media.Reels.Advance(ctx, t.id, reels.ActorSystem)
		if err != nil {
			slog.WarnContext(ctx, "advance failed", "reel_id", t.id, "error", err)
			continue
		}
		if job == reels.JobNone {
			continue
		}
		if err := media.Pipeline.Run(ctx, job, t.id); err != nil {
			slog.ErrorContext(ctx, "pipeline job failed",
				"reel_id", t.id, "job", job, "error", err)
		}
	}
}

// expireDrafts sweeps uploads that never arrived.
//
// Without it every abandoned upload holds a row and a presigned URL forever,
// and the vendor dashboard fills with drafts nobody can explain.
func expireDrafts(ctx context.Context, pool *pgxpool.Pool) {
	tag, err := pool.Exec(ctx, `
		UPDATE reels
		   SET state = 'expired'
		 WHERE state = 'draft'
		   AND upload_expires_at IS NOT NULL
		   AND upload_expires_at < now()`)
	if err != nil {
		slog.ErrorContext(ctx, "expire drafts failed", "error", err)
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		slog.InfoContext(ctx, "expired abandoned uploads", "count", n)
	}
}

// flushCounters writes Redis counts back to Postgres.
func flushCounters(ctx context.Context, media *app.Media) {
	if media == nil || media.Counters == nil {
		return
	}
	n, err := media.Counters.Flush(ctx, 500)
	if err != nil {
		slog.ErrorContext(ctx, "counter flush failed", "error", err)
		return
	}
	if n > 0 {
		slog.DebugContext(ctx, "flushed reel counters", "count", n)
	}
}

// relayOutbox drains committed events to their consumers.
//
// Phase 2 has one consumer: the search indexer. It is written as a relay rather
// than a direct call from the publish path because the index is optional and
// the publish path is not — an OpenSearch outage must not be able to fail a
// publish, and the outbox is what makes that true.
func relayOutbox(ctx context.Context, pool *pgxpool.Pool, indexer search.Indexer) {
	rows, err := pool.Query(ctx, `
		SELECT id, aggregate_id, event_type
		  FROM outbox_events
		 WHERE published_at IS NULL
		   AND aggregate_type = 'reel'
		   AND (next_attempt_at IS NULL OR next_attempt_at < now())
		 ORDER BY id
		 LIMIT 200`)
	if err != nil {
		slog.ErrorContext(ctx, "outbox query failed", "error", err)
		return
	}

	type event struct {
		id        int64
		reelID    uuid.UUID
		eventType string
	}
	var events []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.id, &e.reelID, &e.eventType); err != nil {
			rows.Close()
			return
		}
		events = append(events, e)
	}
	rows.Close()

	for _, e := range events {
		var err error
		switch e.eventType {
		case "reel.published":
			err = indexReel(ctx, pool, indexer, e.reelID)
		case "reel.removed":
			err = indexer.Delete(ctx, e.reelID)
		default:
			// Not ours. Mark handled so it does not block the queue behind it;
			// other consumers read the same table by their own aggregate type.
			err = nil
		}

		if err != nil {
			// Exponential-ish backoff, capped. A failing consumer must not spin
			// at tick frequency against a dependency that is already unwell.
			if _, uerr := pool.Exec(ctx, `
				UPDATE outbox_events
				   SET attempts = attempts + 1,
				       last_error = $2,
				       next_attempt_at = now() + (least(attempts + 1, 10) * interval '30 seconds')
				 WHERE id = $1`, e.id, err.Error()); uerr != nil {
				slog.ErrorContext(ctx, "outbox backoff update failed", "error", uerr)
			}
			continue
		}

		if _, err := pool.Exec(ctx,
			`UPDATE outbox_events SET published_at = now() WHERE id = $1`, e.id); err != nil {
			slog.ErrorContext(ctx, "outbox mark published failed", "error", err)
		}
	}
}

// indexReel builds the search document from the reel's current state rather
// than from the event payload.
//
// Reading current state means a retry that runs an hour later indexes what is
// true now, not what was true when the event was written. For an index that
// exists to be correct rather than to be a log, that is the behaviour you want.
func indexReel(ctx context.Context, pool *pgxpool.Pool, indexer search.Indexer, reelID uuid.UUID) error {
	var doc search.Document
	// Nullable strings, not []byte: ST_AsGeoJSON returns text, and there is no
	// text→bytea cast in Postgres. A reel with no origin point is also normal.
	var coverage, origin *string
	var tags []string

	err := pool.QueryRow(ctx, `
		SELECT r.id, r.author_type, r.author_id, COALESCE(vb.name, ''),
		       r.category_id, r.subject_type, r.subject_id,
		       COALESCE(r.caption, ''), COALESCE(r.caption_locale, ''),
		       COALESCE(r.duration_ms, 0),
		       ST_AsGeoJSON(r.coverage::geometry),
		       ST_AsGeoJSON(r.origin_point::geometry),
		       COALESCE(ARRAY(SELECT tag FROM reel_tags t WHERE t.reel_id = r.id ORDER BY tag), '{}'),
		       COALESCE(c.views, 0), COALESCE(c.likes, 0),
		       COALESCE(r.published_at, now()),
		       COALESCE(f.impressions, 0), COALESCE(f.clicks, 0),
		       COALESCE(f.orders, 0), COALESCE(f.lifetime_impressions, 0),
		       COALESCE(aq.published_count, 0), COALESCE(aq.orders, 0),
		       COALESCE(aq.takedown_count, 0)
		  FROM reels r
		  LEFT JOIN vendor_branches vb
		         ON r.author_type = 'vendor_branch' AND vb.id = r.author_id
		  LEFT JOIN reel_counters c ON c.reel_id = r.id
		  LEFT JOIN reel_features f ON f.reel_id = r.id
		  LEFT JOIN author_quality aq
		         ON aq.author_type = r.author_type AND aq.author_id = r.author_id
		 WHERE r.id = $1`, reelID,
	).Scan(&doc.ID, &doc.AuthorType, &doc.AuthorID, &doc.AuthorName,
		&doc.CategoryID, &doc.SubjectType, &doc.SubjectID,
		&doc.Caption, &doc.CaptionLocale, &doc.DurationMS,
		&coverage, &origin, &tags, &doc.Views, &doc.Likes, &doc.PublishedAt,
		&doc.Impressions, &doc.Clicks, &doc.Orders, &doc.LifetimeImpressions,
		&doc.AuthorPublished, &doc.AuthorOrders, &doc.AuthorTakedowns)
	if err != nil {
		return err
	}

	doc.Tags = tags
	if coverage != nil {
		doc.Coverage = json.RawMessage(*coverage)
	}
	if origin != nil {
		doc.OriginPoint = json.RawMessage(*origin)
	}
	return indexer.Index(ctx, doc)
}

// rollupFunnel recomputes yesterday's per-reel funnel, and today's so far.
//
// Recompute-and-replace, not incremental accumulation. Events arrive late — a
// mobile client that was offline delivers yesterday's batch this morning — and
// an incremental counter cannot absorb that without double-counting. Yesterday
// is recomputed every hour for a day so those stragglers land.
// reconcileTimeSignals brings `reel_time_signals` back in line with the
// verticals that own its rows: backfills subjects that have never been
// projected, rolls recurring cutoffs past their instant, and re-projects
// anything older than a day.
//
// Nil-safe. A deployment with no subject providers registered has nothing to
// reconcile, and that is a coherent state rather than a misconfiguration.
func reconcileTimeSignals(ctx context.Context, signals *timesignal.Projector) {
	if signals == nil {
		return
	}
	n, err := signals.Reconcile(ctx, 500)
	if err != nil {
		slog.ErrorContext(ctx, "time signal reconcile failed", "error", err)
		return
	}
	if n > 0 {
		slog.DebugContext(ctx, "time signals reconciled", "subjects", n)
	}
}

func rollupFunnel(ctx context.Context, media *app.Media) {
	if media == nil || media.Attribution == nil {
		return
	}
	now := time.Now().UTC()
	for _, day := range []time.Time{now, now.AddDate(0, 0, -1)} {
		n, err := media.Attribution.RollupDay(ctx, day)
		if err != nil {
			slog.ErrorContext(ctx, "funnel rollup failed",
				"day", day.Format("2006-01-02"), "error", err)
			continue
		}
		slog.DebugContext(ctx, "funnel rolled up",
			"day", day.Format("2006-01-02"), "reels", n)
	}
}

// ensureImpressionPartitions creates next month's partition ahead of time.
//
// Pre-creating is not merely tidy: rows that land in the DEFAULT partition
// *block* the later creation of any partition whose range would have covered
// them. A month that slips through cannot be given its own partition without
// moving rows out first, so this job running is what keeps that from happening.
func ensureImpressionPartitions(ctx context.Context, pool *pgxpool.Pool) {
	for _, offset := range []int{0, 1, 2} {
		month := time.Now().UTC().AddDate(0, offset, 0)
		var name string
		if err := pool.QueryRow(ctx,
			`SELECT create_feed_impression_partition($1::date)`,
			month.Format("2006-01-02")).Scan(&name); err != nil {
			slog.ErrorContext(ctx, "impression partition creation failed",
				"month", month.Format("2006-01"), "error", err)
			return
		}
	}
}

// refreshRanking recomputes the inputs the feed scores on.
//
// Two jobs, in order, because the second reads what the first writes: author
// quality is rolled up from reel_features rather than re-aggregated from the
// raw event tables. That makes it cheap and — more importantly — makes the
// author prior definitionally consistent with the reel scores it sits beside.
// Two aggregations of the same events by different SQL is how an author with
// good reels ends up with a bad prior and nobody can explain it.
func refreshRanking(ctx context.Context, pool *pgxpool.Pool) {
	refresher := ranking.NewRefresher(pool)

	n, err := refresher.RefreshFeatures(ctx, 1000)
	if err != nil {
		slog.ErrorContext(ctx, "ranking feature refresh failed", "error", err)
		return
	}
	slog.DebugContext(ctx, "ranking features refreshed", "reels", n)

	authors, err := refresher.RefreshAuthorQuality(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "author quality refresh failed", "error", err)
		return
	}
	slog.DebugContext(ctx, "author quality refreshed", "authors", authors)
}

// rebuildInterests recomputes viewer profiles for anyone active recently.
//
// Batched by viewer, because Fold recomputes from the full decayed history
// every time. Running it over every viewer who has ever used the product would
// make the job's cost grow with the install base rather than with the traffic —
// which is fine for a year and then is the reason the database is slow.
func rebuildInterests(ctx context.Context, media *app.Media) {
	if media == nil || media.Interests == nil {
		return
	}

	// Twice the tick, so a viewer active just before the previous run is still
	// picked up by this one. Overlap costs a recomputation; a gap costs a
	// profile that never updates.
	since := time.Now().UTC().Add(-2 * interestTick)

	n, err := media.Interests.RebuildBatch(ctx, since, 500)
	if err != nil {
		slog.ErrorContext(ctx, "interest rebuild failed", "error", err)
		return
	}
	if n > 0 {
		slog.DebugContext(ctx, "interest profiles rebuilt", "viewers", n)
	}

	// Anonymous device profiles accumulate with installs rather than with
	// users: a key is created by anyone who opens the app once. Ninety days of
	// silence is a device that is not coming back.
	reaped, err := media.Interests.Reap(ctx, 90*24*time.Hour)
	if err != nil {
		slog.ErrorContext(ctx, "interest reap failed", "error", err)
		return
	}
	if reaped > 0 {
		slog.InfoContext(ctx, "reaped stale interest profiles", "rows", reaped)
	}
}

// flushAnalytics drains the ClickHouse buffer.
//
// The counters are logged rather than merely returned, and dropped events are
// logged at error level. A sink that quietly loses 10% of views produces a
// conversion rate 11% too high — and Phase 3 spent an entire design on not
// letting that number be wrong.
func flushAnalytics(ctx context.Context, media *app.Media) {
	if media == nil || media.ClickHouse == nil {
		return
	}
	if _, err := media.ClickHouse.Flush(ctx); err != nil {
		slog.ErrorContext(ctx, "analytics flush failed", "error", err)
	}
	media.ClickHouse.LogStats(ctx)
}

// mediaTick is faster than the transactional sweeper's 30 seconds because exit
// criterion #1 is a five-minute upload-to-feed path and this poller sits on it
// three times.
const mediaTick = 10 * time.Second

// funnelTick is hourly. The numbers feed a dashboard someone checks daily and a
// Phase 4 training set that reads closed days, so minute-level freshness would
// be cost with no consumer.
const funnelTick = time.Hour

// rankingTick is every five minutes.
//
// Chosen against one specific failure: a reel published this morning that is
// converting well should start ranking on that fact today, not tomorrow. The
// counter-pressure is that the refresh scans impressions, which is the largest
// table in the product — so it is batched, ordered by staleness, and given its
// own timeout well away from the ten-second pipeline loop.
const rankingTick = 5 * time.Minute

// interestTick is every fifteen minutes. A viewer's tastes do not move faster
// than that, and the job re-folds a viewer's entire decayed history each run.
const interestTick = 15 * time.Minute

func runMediaLoop(ctx context.Context, pool *pgxpool.Pool, media *app.Media, signals *timesignal.Projector) {
	if media == nil {
		slog.Info("media pipeline disabled: no object store configured")
		return
	}

	// Run the slow maintenance once at startup so a fresh deployment is not a
	// month behind on partitions and a day behind on numbers.
	startupCtx, cancelStartup := context.WithTimeout(ctx, 5*time.Minute)
	ensureImpressionPartitions(startupCtx, pool)
	rollupFunnel(startupCtx, media)
	// Time signals at startup too, and this one matters most on a fresh
	// deployment: every reel published before its vertical had a provider has no
	// projection row, so without a backfill pass the Live now and Ending soon
	// tabs are empty on a database full of eligible content.
	reconcileTimeSignals(startupCtx, signals)
	// Ranking too: a fresh deployment whose reel_features table is empty ranks
	// every reel on its priors alone, which is a feed that ignores every signal
	// it has until the first tick fires.
	refreshRanking(startupCtx, pool)
	cancelStartup()

	ticker := time.NewTicker(mediaTick)
	defer ticker.Stop()
	funnel := time.NewTicker(funnelTick)
	defer funnel.Stop()
	rank := time.NewTicker(rankingTick)
	defer rank.Stop()
	interest := time.NewTicker(interestTick)
	defer interest.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(ctx, mediaTick*5)
			expireDrafts(tickCtx, pool)
			mediaSweep(tickCtx, pool, media)
			flushCounters(tickCtx, media)
			flushAnalytics(tickCtx, media)
			relayOutbox(tickCtx, pool, media.Indexer)
			// On the media tick rather than a slower one. A recurring cutoff
			// that has passed is a CTA the client believes is still open, and
			// the window between "16:00 came and went" and "the projection says
			// so" is a window in which the app takes orders it cannot fill.
			reconcileTimeSignals(tickCtx, signals)
			cancel()

			if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
				return
			}

		case <-rank.C:
			// Its own timeout, generous: the feature refresh scans impressions,
			// which is the largest table in the product. It must be able to
			// take minutes without starving the ten-second pipeline loop.
			rankCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			refreshRanking(rankCtx, pool)
			cancel()

		case <-interest.C:
			interestCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			rebuildInterests(interestCtx, media)
			cancel()

		case <-funnel.C:
			// On its own ticker and its own timeout: the rollup scans
			// impressions, which is the largest table in the product, and it
			// must not be able to starve the 10-second pipeline loop.
			funnelCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			ensureImpressionPartitions(funnelCtx, pool)
			rollupFunnel(funnelCtx, media)
			cancel()
		}
	}
}
