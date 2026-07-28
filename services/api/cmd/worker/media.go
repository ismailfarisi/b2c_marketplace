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
	"github.com/marketplace/api/internal/modules/reels"
	"github.com/marketplace/api/internal/modules/search"
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
		       COALESCE(r.published_at, now())
		  FROM reels r
		  LEFT JOIN vendor_branches vb
		         ON r.author_type = 'vendor_branch' AND vb.id = r.author_id
		  LEFT JOIN reel_counters c ON c.reel_id = r.id
		 WHERE r.id = $1`, reelID,
	).Scan(&doc.ID, &doc.AuthorType, &doc.AuthorID, &doc.AuthorName,
		&doc.CategoryID, &doc.SubjectType, &doc.SubjectID,
		&doc.Caption, &doc.CaptionLocale, &doc.DurationMS,
		&coverage, &origin, &tags, &doc.Views, &doc.Likes, &doc.PublishedAt)
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

// mediaTick is faster than the transactional sweeper's 30 seconds because exit
// criterion #1 is a five-minute upload-to-feed path and this poller sits on it
// three times.
const mediaTick = 10 * time.Second

func runMediaLoop(ctx context.Context, pool *pgxpool.Pool, media *app.Media) {
	if media == nil {
		slog.Info("media pipeline disabled: no object store configured")
		return
	}

	ticker := time.NewTicker(mediaTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tickCtx, cancel := context.WithTimeout(ctx, mediaTick*5)
			expireDrafts(tickCtx, pool)
			mediaSweep(tickCtx, pool, media)
			flushCounters(tickCtx, media)
			relayOutbox(tickCtx, pool, media.Indexer)
			cancel()

			if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
				return
			}
		}
	}
}
